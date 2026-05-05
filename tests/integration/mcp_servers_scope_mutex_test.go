//go:build integration

package integration

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// seedUserForScopeMutex inserts a minimal users row for mcp scope mutex tests.
// Uses a unique suffix to avoid email/key collisions with other test files.
func seedUserForScopeMutex(t *testing.T, db *sql.DB) uuid.UUID {
	t.Helper()
	id := uuid.New()
	suffix := id.String()[:8]
	_, err := db.Exec(
		`INSERT INTO users (id, email, password_hash, display_name, role, kind, user_key)
		 VALUES ($1, $2, 'x', 'u', 'member', 'human', $3)`,
		id, "mcpsm-"+suffix+"@local", "mcpsm-"+suffix,
	)
	if err != nil {
		t.Fatalf("seedUserForScopeMutex: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM users WHERE id = $1", id) })
	return id
}

// seedProjectForScopeMutex inserts a minimal active project.
// ownerID must reference an existing users row.
func seedProjectForScopeMutex(t *testing.T, db *sql.DB, ownerID uuid.UUID) uuid.UUID {
	t.Helper()
	id := uuid.New()
	slug := "mcpsm-" + id.String()[:8]
	_, err := db.Exec(
		`INSERT INTO projects (id, owner_user_id, slug, status)
		 VALUES ($1, $2, $3, 'active')`,
		id, ownerID, slug,
	)
	if err != nil {
		t.Fatalf("seedProjectForScopeMutex: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM projects WHERE id = $1", id) })
	return id
}

// insertMCPServerWithScope inserts an mcp_servers row including team_id/project_id scope.
// Callers set teamID and projectID to nil for global scope.
func insertMCPServerWithScope(db *sql.DB, teamID, projectID *uuid.UUID) (uuid.UUID, error) {
	serverID := uuid.New()
	name := "mcp-sm-" + serverID.String()[:8]
	_, err := db.Exec(
		`INSERT INTO mcp_servers (id, name, display_name, transport, enabled, created_by, team_id, project_id)
		 VALUES ($1, $2, $2, 'stdio', true, 'test-user', $3, $4)`,
		serverID, name, teamID, projectID,
	)
	return serverID, err
}

// skipIfScopeMutexColumnsMissing skips when team_id / project_id columns are absent.
// This is the expected RED state before Phase 04 DDL lands.
func skipIfScopeMutexColumnsMissing(t *testing.T, db *sql.DB) {
	t.Helper()
	var n int
	db.QueryRow(
		`SELECT COUNT(*) FROM information_schema.columns
		  WHERE table_name = 'mcp_servers'
		    AND column_name IN ('team_id', 'project_id')`,
	).Scan(&n)
	if n < 2 {
		t.Skip("mcp_servers.team_id / project_id columns not present — waiting for schema DDL")
	}
}

// TestMCPServer_Scope_GlobalAllowed verifies that inserting a server with both
// team_id and project_id NULL (global scope) succeeds.
func TestMCPServer_Scope_GlobalAllowed(t *testing.T) {
	db := testDB(t)
	skipIfScopeMutexColumnsMissing(t, db)

	serverID, err := insertMCPServerWithScope(db, nil, nil)
	if err != nil {
		t.Fatalf("expected global scope insert to succeed, got: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM mcp_servers WHERE id = $1", serverID) })
}

// TestMCPServer_Scope_TeamOnlyAllowed verifies that inserting a server with
// team_id set and project_id NULL (team-scoped) succeeds.
func TestMCPServer_Scope_TeamOnlyAllowed(t *testing.T) {
	db := testDB(t)
	skipIfScopeMutexColumnsMissing(t, db)

	_, ownerAgentID := seedTenantAgent(t, db)
	teamID, _ := seedTeam(t, db, uuid.Nil, ownerAgentID)

	serverID, err := insertMCPServerWithScope(db, &teamID, nil)
	if err != nil {
		t.Fatalf("expected team-scoped insert to succeed, got: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM mcp_servers WHERE id = $1", serverID) })
}

// TestMCPServer_Scope_ProjectOnlyAllowed verifies that inserting a server with
// project_id set and team_id NULL (project-scoped) succeeds.
func TestMCPServer_Scope_ProjectOnlyAllowed(t *testing.T) {
	db := testDB(t)
	skipIfScopeMutexColumnsMissing(t, db)

	ownerID := seedUserForScopeMutex(t, db)
	projectID := seedProjectForScopeMutex(t, db, ownerID)

	serverID, err := insertMCPServerWithScope(db, nil, &projectID)
	if err != nil {
		t.Fatalf("expected project-scoped insert to succeed, got: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM mcp_servers WHERE id = $1", serverID) })
}

// TestMCPServer_Scope_BothRejected verifies that setting both team_id and
// project_id on a single server row violates the mutex CHECK constraint
// (SQLSTATE 23514).
func TestMCPServer_Scope_BothRejected(t *testing.T) {
	db := testDB(t)
	skipIfScopeMutexColumnsMissing(t, db)

	_, ownerAgentID := seedTenantAgent(t, db)
	teamID, _ := seedTeam(t, db, uuid.Nil, ownerAgentID)

	ownerUserID := seedUserForScopeMutex(t, db)
	projectID := seedProjectForScopeMutex(t, db, ownerUserID)

	_, err := insertMCPServerWithScope(db, &teamID, &projectID)
	if err == nil {
		t.Fatal("expected CHECK violation when both team_id and project_id are set, got nil error")
	}
	// SQLSTATE 23514 = check_violation
	if !strings.Contains(err.Error(), "23514") {
		t.Errorf("expected SQLSTATE 23514 (check_violation), got: %v", err)
	}
}

// TestMCPServer_Scope_TeamCascadeDelete verifies that deleting the parent team
// also removes mcp_servers rows scoped to that team (ON DELETE CASCADE).
func TestMCPServer_Scope_TeamCascadeDelete(t *testing.T) {
	db := testDB(t)
	skipIfScopeMutexColumnsMissing(t, db)

	_, ownerAgentID := seedTenantAgent(t, db)
	teamID, _ := seedTeam(t, db, uuid.Nil, ownerAgentID)

	serverID, err := insertMCPServerWithScope(db, &teamID, nil)
	if err != nil {
		t.Fatalf("insert team-scoped server: %v", err)
	}

	// Delete team — should cascade to mcp_servers
	if _, err := db.Exec("DELETE FROM agent_teams WHERE id = $1", teamID); err != nil {
		t.Fatalf("delete team: %v", err)
	}

	var count int
	db.QueryRow("SELECT COUNT(*) FROM mcp_servers WHERE id = $1", serverID).Scan(&count)
	if count != 0 {
		t.Errorf("expected mcp_servers row to be CASCADE deleted when team is removed, but row still exists")
	}
}

// TestMCPServer_Scope_ProjectCascadeDelete verifies that deleting the parent
// project removes mcp_servers rows scoped to that project (ON DELETE CASCADE).
func TestMCPServer_Scope_ProjectCascadeDelete(t *testing.T) {
	db := testDB(t)
	skipIfScopeMutexColumnsMissing(t, db)

	ownerID := seedUserForScopeMutex(t, db)
	projectID := seedProjectForScopeMutex(t, db, ownerID)

	serverID, err := insertMCPServerWithScope(db, nil, &projectID)
	if err != nil {
		t.Fatalf("insert project-scoped server: %v", err)
	}

	// Delete project — should cascade to mcp_servers
	if _, err := db.Exec("DELETE FROM projects WHERE id = $1", projectID); err != nil {
		t.Fatalf("delete project: %v", err)
	}

	var count int
	db.QueryRow("SELECT COUNT(*) FROM mcp_servers WHERE id = $1", serverID).Scan(&count)
	if count != 0 {
		t.Errorf("expected mcp_servers row to be CASCADE deleted when project is removed, but row still exists")
	}
}
