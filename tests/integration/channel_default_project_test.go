//go:build integration

package integration

import (
	"context"
	"database/sql"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
)

// skipIfContactDefaultProjectMissing skips when the column is absent (schema not applied).
func skipIfContactDefaultProjectMissing(t *testing.T, db *sql.DB) {
	t.Helper()
	var n int
	db.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM information_schema.columns
		  WHERE table_name = 'channel_contacts' AND column_name = 'default_project_id'`,
	).Scan(&n)
	if n == 0 {
		t.Skip("channel_contacts.default_project_id column not present — waiting for schema DDL")
	}
}

// seedUserForContacts inserts a minimal users row for contact integration tests.
func seedUserForContacts(t *testing.T, db *sql.DB) uuid.UUID {
	t.Helper()
	id := uuid.New()
	suf := id.String()[:8]
	_, err := db.Exec(
		`INSERT INTO users (id, email, password_hash, display_name, role, kind, user_key)
		 VALUES ($1, $2, 'x', 'u', 'member', 'human', $3)`,
		id, "cdp-"+suf+"@local", "cdp-"+suf,
	)
	if err != nil {
		t.Fatalf("seedUserForContacts: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM users WHERE id = $1", id) })
	return id
}

// seedProjectForContacts inserts a minimal active project.
func seedProjectForContacts(t *testing.T, db *sql.DB, ownerID uuid.UUID) uuid.UUID {
	t.Helper()
	id := uuid.New()
	slug := "cdp-" + id.String()[:8]
	_, err := db.Exec(
		`INSERT INTO projects (id, owner_user_id, slug, status)
		 VALUES ($1, $2, $3, 'active')`,
		id, ownerID, slug,
	)
	if err != nil {
		t.Fatalf("seedProjectForContacts: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM projects WHERE id = $1", id) })
	return id
}

// seedGroupContact inserts a group-type channel contact row and returns its UUID.
func seedGroupContact(t *testing.T, db *sql.DB) uuid.UUID {
	t.Helper()
	id := uuid.New()
	sender := "grp-" + id.String()[:8]
	_, err := db.Exec(
		`INSERT INTO channel_contacts (id, channel_type, sender_id, contact_type)
		 VALUES ($1, 'telegram', $2, 'group')`,
		id, sender,
	)
	if err != nil {
		t.Fatalf("seedGroupContact: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM channel_contacts WHERE id = $1", id) })
	return id
}

// ─── Scenario 1: admin sets default project → stored ─────────────────────────

// TestChannelContactDefaultProjectSetByAdmin asserts that UpdateDefaultProject
// writes the FK and the row can be read back with GetContactByID.
func TestChannelContactDefaultProjectSetByAdmin(t *testing.T) {
	db := testDB(t)
	skipIfContactDefaultProjectMissing(t, db)

	owner := seedUserForContacts(t, db)
	projectID := seedProjectForContacts(t, db, owner)
	contactID := seedGroupContact(t, db)

	store := pg.NewPGContactStore(db)

	if err := store.UpdateDefaultProject(context.Background(), contactID, &projectID); err != nil {
		t.Fatalf("UpdateDefaultProject: %v", err)
	}

	contact, err := store.GetContactByID(context.Background(), contactID)
	if err != nil {
		t.Fatalf("GetContactByID: %v", err)
	}
	if contact.DefaultProjectID == nil {
		t.Fatal("expected DefaultProjectID to be set, got nil")
	}
	if *contact.DefaultProjectID != projectID {
		t.Errorf("DefaultProjectID: got %v want %v", *contact.DefaultProjectID, projectID)
	}
}

// ─── Scenario 2: UpdateDefaultProject(nil) clears the binding ────────────────

// TestChannelContactDefaultProjectCleared asserts that passing nil projectID
// clears the default_project_id to NULL.
func TestChannelContactDefaultProjectCleared(t *testing.T) {
	db := testDB(t)
	skipIfContactDefaultProjectMissing(t, db)

	owner := seedUserForContacts(t, db)
	projectID := seedProjectForContacts(t, db, owner)
	contactID := seedGroupContact(t, db)

	store := pg.NewPGContactStore(db)

	// First set a binding.
	if err := store.UpdateDefaultProject(context.Background(), contactID, &projectID); err != nil {
		t.Fatalf("UpdateDefaultProject (set): %v", err)
	}

	// Now clear it.
	if err := store.UpdateDefaultProject(context.Background(), contactID, nil); err != nil {
		t.Fatalf("UpdateDefaultProject (clear): %v", err)
	}

	contact, err := store.GetContactByID(context.Background(), contactID)
	if err != nil {
		t.Fatalf("GetContactByID: %v", err)
	}
	if contact.DefaultProjectID != nil {
		t.Errorf("expected DefaultProjectID nil after clear, got %v", *contact.DefaultProjectID)
	}
}

// ─── Scenario 3: hard-delete project → contact.default_project_id → NULL ─────

// TestChannelContactDefaultProjectNullOnProjectDelete asserts ON DELETE SET NULL:
// removing the projects row nullifies default_project_id on channel_contacts.
func TestChannelContactDefaultProjectNullOnProjectDelete(t *testing.T) {
	db := testDB(t)
	skipIfContactDefaultProjectMissing(t, db)

	owner := seedUserForContacts(t, db)
	contactID := seedGroupContact(t, db)

	// Insert a project without registering a cleanup — we delete it manually.
	projectID := uuid.New()
	slug := "cdp-del-" + projectID.String()[:8]
	_, err := db.Exec(
		`INSERT INTO projects (id, owner_user_id, slug, status) VALUES ($1, $2, $3, 'active')`,
		projectID, owner, slug,
	)
	if err != nil {
		t.Fatalf("insert project: %v", err)
	}

	contactStore := pg.NewPGContactStore(db)
	if err := contactStore.UpdateDefaultProject(context.Background(), contactID, &projectID); err != nil {
		t.Fatalf("UpdateDefaultProject: %v", err)
	}

	// Hard-delete the project — ON DELETE SET NULL must fire.
	if _, err := db.Exec("DELETE FROM projects WHERE id = $1", projectID); err != nil {
		t.Fatalf("hard-delete project: %v", err)
	}

	// Read back the contact — default_project_id must now be NULL.
	var stored *uuid.UUID
	err = db.QueryRowContext(context.Background(),
		`SELECT default_project_id FROM channel_contacts WHERE id = $1`, contactID,
	).Scan(&stored)
	if err != nil {
		t.Fatalf("select default_project_id after project delete: %v", err)
	}
	if stored != nil {
		t.Errorf("expected NULL default_project_id after project hard-delete, got %v", *stored)
	}
}
