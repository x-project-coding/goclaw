//go:build integration

package integration

// E2E FK-action scenarios 8-9: archive retains sessions, hard delete nulls FKs.
// Seed helpers live in projects_e2e_helpers_test.go.

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
)

// TestProjectsE2E_ArchiveProjectRetainsSessions asserts UpdateStatus(archived)
// does not null out sessions.project_id — archiving is a soft status change only.
func TestProjectsE2E_ArchiveProjectRetainsSessions(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	owner := e2eUser(t, db)
	_, agentID := seedTenantAgent(t, db)
	p := e2eCreateProject(t, ctx, db, owner)

	sessionKey := "e2e:arch:" + uuid.New().String()[:8]
	if err := insertSessionWithProject(db, sessionKey, agentID, &p.ID); err != nil {
		t.Fatalf("insertSessionWithProject: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM agent_sessions WHERE session_key = $1", sessionKey) })

	ps := pg.NewPGProjectStore(db)
	if err := ps.UpdateStatus(ctx, p.ID, "archived"); err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}

	got, err := ps.Get(ctx, p.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != "archived" {
		t.Errorf("status = %q, want %q", got.Status, "archived")
	}

	// Session must still reference the (now archived) project — no cascade on archive.
	var stored *uuid.UUID
	db.QueryRowContext(ctx,
		`SELECT project_id FROM agent_sessions WHERE session_key = $1`, sessionKey,
	).Scan(&stored)
	if stored == nil || *stored != p.ID {
		t.Errorf("session.project_id after archive = %v, want %v", stored, p.ID)
	}
}

// TestProjectsE2E_HardDeleteNullsSessionsAndContacts asserts:
//   - agent_sessions.project_id → NULL (ON DELETE SET NULL)
//   - channel_contacts.default_project_id → NULL (ON DELETE SET NULL)
//   - project_grants rows → removed (ON DELETE CASCADE)
func TestProjectsE2E_HardDeleteNullsSessionsAndContacts(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	owner := e2eUser(t, db)
	_, agentID := seedTenantAgent(t, db)

	// Insert project without a t.Cleanup — deleted manually to trigger FK actions.
	projectID := uuid.New()
	slug := "e2e-hd-" + projectID.String()[:8]
	_, err := db.Exec(
		`INSERT INTO projects (id, owner_user_id, slug, status) VALUES ($1, $2, $3, 'active')`,
		projectID, owner, slug,
	)
	if err != nil {
		t.Fatalf("insert project: %v", err)
	}

	sessionKey := "e2e:hd:" + uuid.New().String()[:8]
	if err := insertSessionWithProject(db, sessionKey, agentID, &projectID); err != nil {
		t.Fatalf("insertSessionWithProject: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM agent_sessions WHERE session_key = $1", sessionKey) })

	contactID := e2eContact(t, db)
	cs := pg.NewPGContactStore(db)
	if err := cs.UpdateDefaultProject(ctx, contactID, &projectID); err != nil {
		t.Fatalf("UpdateDefaultProject: %v", err)
	}

	gs := pg.NewPGProjectGrantStore(db)
	ownerStr := owner.String()
	g := &store.ProjectGrant{ProjectID: projectID.String(), UserID: &ownerStr, Role: "editor"}
	if err := gs.Create(ctx, g); err != nil {
		t.Fatalf("Create grant: %v", err)
	}

	if _, err := db.Exec("DELETE FROM projects WHERE id = $1", projectID); err != nil {
		t.Fatalf("hard delete project: %v", err)
	}

	// 9a. Sessions.project_id → NULL.
	var sessProj *uuid.UUID
	db.QueryRowContext(ctx,
		`SELECT project_id FROM agent_sessions WHERE session_key = $1`, sessionKey,
	).Scan(&sessProj)
	if sessProj != nil {
		t.Errorf("session.project_id must be NULL after hard-delete, got %v", *sessProj)
	}

	// 9b. contact.default_project_id → NULL.
	var contProj *uuid.UUID
	db.QueryRowContext(ctx,
		`SELECT default_project_id FROM channel_contacts WHERE id = $1`, contactID,
	).Scan(&contProj)
	if contProj != nil {
		t.Errorf("contact.default_project_id must be NULL after hard-delete, got %v", *contProj)
	}

	// 9c. project_grants cascade-deleted.
	var grantCount int
	db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM project_grants WHERE project_id = $1`, projectID,
	).Scan(&grantCount)
	if grantCount != 0 {
		t.Errorf("project_grants must cascade-delete, got %d rows remaining", grantCount)
	}
}
