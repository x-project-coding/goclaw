//go:build sqliteonly && integration

package integration

// SQLite parity — access, FK-action, and permission-resolution scenarios 6-10.
// Helpers and DB setup live in projects_sqlite_parity_helpers_test.go.

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/sqlitestore"
)

// TestSQLiteProjectsParity_ContactDefaultProjectSet asserts UpdateDefaultProject
// and GetContactByID round-trip correctly on SQLite.
func TestSQLiteProjectsParity_ContactDefaultProjectSet(t *testing.T) {
	db := newProjectsParityDB(t)
	ctx := context.Background()

	ownerID := sqliteE2EUser(t, db)
	p := sqliteE2EProject(t, ctx, db, ownerID)
	contactID := sqliteE2EContact(t, db)
	contactUUID, err := uuid.Parse(contactID)
	if err != nil {
		t.Fatalf("parse contactID: %v", err)
	}

	cs := sqlitestore.NewSQLiteContactStore(db)
	if err := cs.UpdateDefaultProject(ctx, contactUUID, &p.ID); err != nil {
		t.Fatalf("UpdateDefaultProject: %v", err)
	}

	contact, err := cs.GetContactByID(ctx, contactUUID)
	if err != nil {
		t.Fatalf("GetContactByID: %v", err)
	}
	if contact.DefaultProjectID == nil || *contact.DefaultProjectID != p.ID {
		t.Errorf("DefaultProjectID = %v, want %v", contact.DefaultProjectID, p.ID)
	}
}

// TestSQLiteProjectsParity_ArchiveRetainsSessions asserts that UpdateStatus does
// not null out agent_sessions.project_id — a status UPDATE is not a DELETE and
// must not trigger ON DELETE SET NULL.
func TestSQLiteProjectsParity_ArchiveRetainsSessions(t *testing.T) {
	db := newProjectsParityDB(t)
	ctx := context.Background()

	ownerID := sqliteE2EUser(t, db)
	agentID := sqliteE2EAgent(t, db, ownerID)
	p := sqliteE2EProject(t, ctx, db, ownerID)

	pStr := p.ID.String()
	sessionKey := sqliteE2ESession(t, db, agentID, &pStr)

	ps := sqlitestore.NewSQLiteProjectStore(db)
	if err := ps.UpdateStatus(ctx, p.ID, "archived"); err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}

	got, err := ps.Get(ctx, p.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != "archived" {
		t.Errorf("status = %q, want archived", got.Status)
	}

	var stored *string
	db.QueryRowContext(ctx,
		`SELECT project_id FROM agent_sessions WHERE session_key = ?`, sessionKey,
	).Scan(&stored)
	if stored == nil || *stored != pStr {
		t.Errorf("session.project_id after archive = %v, want %v", stored, pStr)
	}
}

// TestSQLiteProjectsParity_HardDeleteNullsFK asserts that with
// PRAGMA foreign_keys=ON, hard-deleting a projects row triggers:
//   - ON DELETE SET NULL on agent_sessions.project_id
//   - ON DELETE SET NULL on channel_contacts.default_project_id
//   - ON DELETE CASCADE on project_grants
func TestSQLiteProjectsParity_HardDeleteNullsFK(t *testing.T) {
	db := newProjectsParityDB(t)
	ctx := context.Background()

	ownerID := sqliteE2EUser(t, db)
	agentID := sqliteE2EAgent(t, db, ownerID)

	// Insert project without a deferred cleanup — deleted manually to trigger FK actions.
	projectID := uuid.Must(uuid.NewV7()).String()
	slug := "sqle-hd-" + projectID[len(projectID)-8:]
	_, err := db.Exec(
		`INSERT INTO projects (id, owner_user_id, slug, status) VALUES (?, ?, ?, 'active')`,
		projectID, ownerID, slug,
	)
	if err != nil {
		t.Fatalf("insert project: %v", err)
	}

	sessionKey := sqliteE2ESession(t, db, agentID, &projectID)

	contactID := sqliteE2EContact(t, db)
	contactUUID, _ := uuid.Parse(contactID)
	projUUID, _ := uuid.Parse(projectID)
	cs := sqlitestore.NewSQLiteContactStore(db)
	if err := cs.UpdateDefaultProject(ctx, contactUUID, &projUUID); err != nil {
		t.Fatalf("UpdateDefaultProject: %v", err)
	}

	gs := sqlitestore.NewSQLiteProjectGrantStore(db)
	g := &store.ProjectGrant{ProjectID: projectID, UserID: &ownerID, Role: "editor"}
	if err := gs.Create(ctx, g); err != nil {
		t.Fatalf("Create grant: %v", err)
	}

	if _, err := db.Exec("DELETE FROM projects WHERE id = ?", projectID); err != nil {
		t.Fatalf("hard delete: %v", err)
	}

	// 9a. session.project_id → NULL.
	var sessProj *string
	db.QueryRowContext(ctx,
		`SELECT project_id FROM agent_sessions WHERE session_key = ?`, sessionKey,
	).Scan(&sessProj)
	if sessProj != nil {
		t.Errorf("session.project_id must be NULL after hard-delete, got %v", *sessProj)
	}

	// 9b. contact.default_project_id → NULL.
	var contProj *string
	db.QueryRowContext(ctx,
		`SELECT default_project_id FROM channel_contacts WHERE id = ?`, contactID,
	).Scan(&contProj)
	if contProj != nil {
		t.Errorf("contact.default_project_id must be NULL after hard-delete, got %v", *contProj)
	}

	// 9c. project_grants cascade-deleted.
	var grantCount int
	db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM project_grants WHERE project_id = ?`, projectID,
	).Scan(&grantCount)
	if grantCount != 0 {
		t.Errorf("project_grants must cascade-delete, got %d rows", grantCount)
	}
}

// TestSQLiteProjectsParity_OwnerResolvesEditorRank asserts the SQLite UNION ALL
// query returns rank=3 and isOwner=true for the project creator with no grant row.
func TestSQLiteProjectsParity_OwnerResolvesEditorRank(t *testing.T) {
	db := newProjectsParityDB(t)
	ctx := context.Background()

	ownerID := sqliteE2EUser(t, db)
	p := sqliteE2EProject(t, ctx, db, ownerID)

	gs := sqlitestore.NewSQLiteProjectGrantStore(db)
	role, isOwner, found, err := permissions.ResolveProjectRole(ctx, gs, ownerID, p.ID.String())
	if err != nil {
		t.Fatalf("ResolveProjectRole: %v", err)
	}
	if !found {
		t.Fatal("owner must have access (found=true)")
	}
	if role != permissions.ProjectRoleEditor {
		t.Errorf("owner role = %q, want editor", role)
	}
	if !isOwner {
		t.Error("isOwner must be true for the project creator")
	}
}

// TestSQLiteProjectsParity_GetNonExistentReturnsErrNoRows asserts the SQLite
// project store returns sql.ErrNoRows for a missing UUID.
func TestSQLiteProjectsParity_GetNonExistentReturnsErrNoRows(t *testing.T) {
	db := newProjectsParityDB(t)
	ctx := context.Background()

	ps := sqlitestore.NewSQLiteProjectStore(db)
	_, err := ps.Get(ctx, uuid.Must(uuid.NewV7()))
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("expected sql.ErrNoRows, got %v", err)
	}
}
