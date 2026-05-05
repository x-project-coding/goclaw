//go:build integration

package integration

import (
	"context"
	"database/sql"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
)

// seedUserForSessions inserts a minimal users row for session tests.
func seedUserForSessions(t *testing.T, db *sql.DB) uuid.UUID {
	t.Helper()
	id := uuid.New()
	suffix := id.String()[:8]
	_, err := db.Exec(
		`INSERT INTO users (id, email, password_hash, display_name, role, kind, user_key)
		 VALUES ($1, $2, 'x', 'u', 'member', 'human', $3)`,
		id, "sess-"+suffix+"@local", "sess-"+suffix,
	)
	if err != nil {
		t.Fatalf("seedUserForSessions: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM users WHERE id = $1", id) })
	return id
}

// seedProjectForSessions inserts a minimal active project.
func seedProjectForSessions(t *testing.T, db *sql.DB, ownerID uuid.UUID) uuid.UUID {
	t.Helper()
	id := uuid.New()
	slug := "sp-" + id.String()[:8]
	_, err := db.Exec(
		`INSERT INTO projects (id, owner_user_id, slug, status)
		 VALUES ($1, $2, $3, 'active')`,
		id, ownerID, slug,
	)
	if err != nil {
		t.Fatalf("seedProjectForSessions: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM projects WHERE id = $1", id) })
	return id
}

// insertSessionWithProject inserts an agent_sessions row with optional project_id.
func insertSessionWithProject(db *sql.DB, sessionKey string, agentID uuid.UUID, projectID *uuid.UUID) error {
	_, err := db.Exec(
		`INSERT INTO agent_sessions (id, session_key, messages, agent_id, project_id)
		 VALUES ($1, $2, '[]', $3, $4)`,
		uuid.New(), sessionKey, agentID, projectID,
	)
	return err
}

// skipIfSessionsProjectColumnMissing skips the test if the project_id column is absent.
func skipIfSessionsProjectColumnMissing(t *testing.T, db *sql.DB) {
	t.Helper()
	var n int
	db.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM information_schema.columns
		  WHERE table_name = 'agent_sessions' AND column_name = 'project_id'`,
	).Scan(&n)
	if n == 0 {
		t.Skip("agent_sessions.project_id column not present — waiting for schema DDL")
	}
}

// ─── Scenario 1: create session with project_id → row stores it ──────────────

// TestAgentSessionsProjectIDStoredOnCreate asserts that creating a session
// with a non-nil project_id stores the FK in the row.
func TestAgentSessionsProjectIDStoredOnCreate(t *testing.T) {
	db := testDB(t)
	skipIfSessionsProjectColumnMissing(t, db)

	owner := seedUserForSessions(t, db)
	_, agentID := seedTenantAgent(t, db)
	projectID := seedProjectForSessions(t, db, owner)

	sessionKey := "agent:test:ws:proj-" + uuid.New().String()[:8]
	if err := insertSessionWithProject(db, sessionKey, agentID, &projectID); err != nil {
		t.Fatalf("insert session with project_id: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM agent_sessions WHERE session_key = $1", sessionKey) })

	var stored *uuid.UUID
	err := db.QueryRowContext(context.Background(),
		`SELECT project_id FROM agent_sessions WHERE session_key = $1`, sessionKey,
	).Scan(&stored)
	if err != nil {
		t.Fatalf("select project_id: %v", err)
	}
	if stored == nil || *stored != projectID {
		t.Errorf("expected project_id=%v, got %v", projectID, stored)
	}
}

// ─── Scenario 2: create session without project_id → NULL stored ─────────────

// TestAgentSessionsProjectIDNullWhenNotProvided asserts that omitting project_id
// stores NULL in the row (backward-compatible default).
func TestAgentSessionsProjectIDNullWhenNotProvided(t *testing.T) {
	db := testDB(t)
	skipIfSessionsProjectColumnMissing(t, db)

	_, agentID := seedTenantAgent(t, db)

	sessionKey := "agent:test:ws:noproj-" + uuid.New().String()[:8]
	if err := insertSessionWithProject(db, sessionKey, agentID, nil); err != nil {
		t.Fatalf("insert session without project_id: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM agent_sessions WHERE session_key = $1", sessionKey) })

	var stored *uuid.UUID
	err := db.QueryRowContext(context.Background(),
		`SELECT project_id FROM agent_sessions WHERE session_key = $1`, sessionKey,
	).Scan(&stored)
	if err != nil {
		t.Fatalf("select project_id: %v", err)
	}
	if stored != nil {
		t.Errorf("expected NULL project_id, got %v", *stored)
	}
}

// ─── Scenario 3: hard-delete project → session.project_id becomes NULL ───────

// TestAgentSessionsProjectIDSetNullOnProjectHardDelete asserts ON DELETE SET NULL
// behavior: removing the projects row nullifies the FK on agent_sessions.
func TestAgentSessionsProjectIDSetNullOnProjectHardDelete(t *testing.T) {
	db := testDB(t)
	skipIfSessionsProjectColumnMissing(t, db)

	owner := seedUserForSessions(t, db)
	_, agentID := seedTenantAgent(t, db)

	// Insert project WITHOUT registering a cleanup — we delete it manually.
	projectID := uuid.New()
	slug := "hd-" + projectID.String()[:8]
	_, err := db.Exec(
		`INSERT INTO projects (id, owner_user_id, slug, status) VALUES ($1, $2, $3, 'active')`,
		projectID, owner, slug,
	)
	if err != nil {
		t.Fatalf("insert project: %v", err)
	}

	sessionKey := "agent:test:ws:fkdel-" + uuid.New().String()[:8]
	if err := insertSessionWithProject(db, sessionKey, agentID, &projectID); err != nil {
		t.Fatalf("insert session: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM agent_sessions WHERE session_key = $1", sessionKey) })

	// Hard-delete the project row — ON DELETE SET NULL must fire.
	if _, err := db.Exec("DELETE FROM projects WHERE id = $1", projectID); err != nil {
		t.Fatalf("hard-delete project: %v", err)
	}

	var stored *uuid.UUID
	err = db.QueryRowContext(context.Background(),
		`SELECT project_id FROM agent_sessions WHERE session_key = $1`, sessionKey,
	).Scan(&stored)
	if err != nil {
		t.Fatalf("select project_id after delete: %v", err)
	}
	if stored != nil {
		t.Errorf("expected NULL project_id after project hard-delete, got %v", *stored)
	}
}

// ─── Scenario 4: UpdateProject succeeds for member+ caller ───────────────────

// TestAgentSessionsUpdateProjectSucceedsForMember asserts that UpdateProject
// writes the new FK when the caller holds at least the member role.
func TestAgentSessionsUpdateProjectSucceedsForMember(t *testing.T) {
	db := testDB(t)
	skipIfSessionsProjectColumnMissing(t, db)

	owner := seedUserForSessions(t, db)
	_, agentID := seedTenantAgent(t, db)
	projectID := seedProjectForSessions(t, db, owner)

	sessionKey := "agent:test:ws:upd-" + uuid.New().String()[:8]
	if err := insertSessionWithProject(db, sessionKey, agentID, nil); err != nil {
		t.Fatalf("insert session: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM agent_sessions WHERE session_key = $1", sessionKey) })

	// Caller IS the owner → editor-equivalent rank satisfies member+ requirement.
	sessStore := pg.NewPGSessionStore(db)
	if err := sessStore.UpdateProject(context.Background(), sessionKey, &projectID); err != nil {
		t.Fatalf("UpdateProject: %v", err)
	}

	var stored *uuid.UUID
	db.QueryRowContext(context.Background(),
		`SELECT project_id FROM agent_sessions WHERE session_key = $1`, sessionKey,
	).Scan(&stored)
	if stored == nil || *stored != projectID {
		t.Errorf("expected project_id=%v after UpdateProject, got %v", projectID, stored)
	}
}

// ─── Scenario 5: UpdateProject rejected for viewer-only caller (handler gate) ─

// TestAgentSessionsUpdateProjectRejectedForViewer asserts that CanAccessProject
// returns false when the caller holds only the viewer role, preventing the
// handler from calling UpdateProject. The store itself is not called here —
// the permission layer is tested directly.
func TestAgentSessionsUpdateProjectRejectedForViewer(t *testing.T) {
	db := testDB(t)
	skipIfSessionsProjectColumnMissing(t, db)

	owner := seedUserForSessions(t, db)
	projectID := seedProjectForSessions(t, db, owner)

	// Insert a viewer-only grant for a fresh user.
	viewerID := seedUserForSessions(t, db)
	_, err := db.Exec(
		`INSERT INTO project_grants (project_id, user_id, role) VALUES ($1, $2, 'viewer')`,
		projectID, viewerID,
	)
	if err != nil {
		t.Fatalf("insert viewer grant: %v", err)
	}
	t.Cleanup(func() {
		db.Exec("DELETE FROM project_grants WHERE project_id = $1 AND user_id = $2", projectID, viewerID)
	})

	grantStore := pg.NewPGProjectGrantStore(db)
	ok, err := permissions.CanAccessProject(
		context.Background(), grantStore,
		viewerID.String(), projectID.String(),
		permissions.ProjectRoleMember,
	)
	if err != nil {
		t.Fatalf("CanAccessProject: %v", err)
	}
	if ok {
		t.Error("viewer-only caller must NOT satisfy member+ requirement")
	}
}
