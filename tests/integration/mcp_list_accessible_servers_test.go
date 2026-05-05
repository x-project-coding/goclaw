//go:build integration

package integration

import (
	"database/sql"
	"sort"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
)

// seedMCPServerScoped inserts an mcp_servers row with optional team_id/project_id scope.
// Returns the server ID. Cleanup is registered on t.
func seedMCPServerScoped(t *testing.T, db *sql.DB, teamID, projectID *uuid.UUID) uuid.UUID {
	t.Helper()
	serverID := uuid.New()
	name := "mcp-las-" + serverID.String()[:8]
	_, err := db.Exec(
		`INSERT INTO mcp_servers (id, name, display_name, transport, enabled, created_by, team_id, project_id)
		 VALUES ($1, $2, $2, 'stdio', true, 'test-user', $3, $4)`,
		serverID, name, teamID, projectID,
	)
	if err != nil {
		t.Fatalf("seedMCPServerScoped: %v", err)
	}
	t.Cleanup(func() {
		db.Exec("DELETE FROM mcp_user_credentials WHERE server_id = $1", serverID)
		db.Exec("DELETE FROM mcp_access_requests WHERE server_id = $1", serverID)
		db.Exec("DELETE FROM mcp_user_grants WHERE server_id = $1", serverID)
		db.Exec("DELETE FROM mcp_agent_grants WHERE server_id = $1", serverID)
		db.Exec("DELETE FROM mcp_servers WHERE id = $1", serverID)
	})
	return serverID
}

// grantAgentOnServer inserts an active mcp_agent_grants row.
func grantAgentOnServer(t *testing.T, db *sql.DB, agentID, serverID uuid.UUID) {
	t.Helper()
	grantID := uuid.New()
	_, err := db.Exec(
		`INSERT INTO mcp_agent_grants (id, server_id, agent_id, enabled, granted_by, created_at)
		 VALUES ($1, $2, $3, true, 'test-admin', NOW())`,
		grantID, serverID, agentID,
	)
	if err != nil {
		t.Fatalf("grantAgentOnServer: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM mcp_agent_grants WHERE id = $1", grantID) })
}

// grantAgentOnServerDisabled inserts a disabled mcp_agent_grants row.
func grantAgentOnServerDisabled(t *testing.T, db *sql.DB, agentID, serverID uuid.UUID) {
	t.Helper()
	grantID := uuid.New()
	_, err := db.Exec(
		`INSERT INTO mcp_agent_grants (id, server_id, agent_id, enabled, granted_by, created_at)
		 VALUES ($1, $2, $3, false, 'test-admin', NOW())`,
		grantID, serverID, agentID,
	)
	if err != nil {
		t.Fatalf("grantAgentOnServerDisabled: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM mcp_agent_grants WHERE id = $1", grantID) })
}

// skipIfScopeColumnsAbsent skips when team_id / project_id columns are absent from mcp_servers.
func skipIfScopeColumnsAbsent(t *testing.T, db *sql.DB) {
	t.Helper()
	var n int
	db.QueryRow(
		`SELECT COUNT(*) FROM information_schema.columns
		  WHERE table_name = 'mcp_servers'
		    AND column_name IN ('team_id', 'project_id')`,
	).Scan(&n)
	if n < 2 {
		t.Skip("mcp_servers.team_id / project_id not present — waiting for schema migration")
	}
}

// listAccessibleNames calls ListAccessibleServers and returns sorted server names.
func listAccessibleNames(t *testing.T, db *sql.DB, agentID uuid.UUID, teamID, projectID *uuid.UUID) []string {
	t.Helper()
	mcpStore := pg.NewPGMCPServerStore(db, "")
	servers, err := mcpStore.ListAccessibleServers(t.Context(), agentID, teamID, projectID)
	if err != nil {
		t.Fatalf("ListAccessibleServers: %v", err)
	}
	names := make([]string, 0, len(servers))
	for _, s := range servers {
		names = append(names, s.Name)
	}
	sort.Strings(names)
	return names
}

// containsName returns true if name appears in the sorted slice.
func containsName(names []string, name string) bool {
	for _, n := range names {
		if n == name {
			return true
		}
	}
	return false
}

// --- Phase 05 integration tests: ListAccessibleServers scope filter ---

// TestListAccessibleServers_GlobalOnly: no teamID/projectID → returns only global servers.
func TestListAccessibleServers_GlobalOnly(t *testing.T) {
	db := testDB(t)
	skipIfScopeColumnsAbsent(t, db)

	_, agent1 := seedTenantAgent(t, db)
	_, ownerAgent := seedTenantAgent(t, db)
	teamID, _ := seedTeam(t, db, uuid.Nil, ownerAgent)
	ownerUser := seedUserForScopeMutex(t, db)
	projectID := seedProjectForScopeMutex(t, db, ownerUser)

	globalServer := seedMCPServerScoped(t, db, nil, nil)
	teamServer := seedMCPServerScoped(t, db, &teamID, nil)
	projectServer := seedMCPServerScoped(t, db, nil, &projectID)

	grantAgentOnServer(t, db, agent1, globalServer)
	grantAgentOnServer(t, db, agent1, teamServer)
	grantAgentOnServer(t, db, agent1, projectServer)

	// No scope context → only global
	names := listAccessibleNames(t, db, agent1, nil, nil)
	if len(names) != 1 {
		t.Fatalf("expected 1 server (global only), got %d: %v", len(names), names)
	}
}

// TestListAccessibleServers_GlobalAndTeam: teamID provided → global + team-scoped.
func TestListAccessibleServers_GlobalAndTeam(t *testing.T) {
	db := testDB(t)
	skipIfScopeColumnsAbsent(t, db)

	_, agent1 := seedTenantAgent(t, db)
	_, ownerAgent := seedTenantAgent(t, db)
	teamID, _ := seedTeam(t, db, uuid.Nil, ownerAgent)
	ownerUser := seedUserForScopeMutex(t, db)
	projectID := seedProjectForScopeMutex(t, db, ownerUser)

	globalServer := seedMCPServerScoped(t, db, nil, nil)
	teamServer := seedMCPServerScoped(t, db, &teamID, nil)
	projectServer := seedMCPServerScoped(t, db, nil, &projectID)

	grantAgentOnServer(t, db, agent1, globalServer)
	grantAgentOnServer(t, db, agent1, teamServer)
	grantAgentOnServer(t, db, agent1, projectServer)

	names := listAccessibleNames(t, db, agent1, &teamID, nil)
	if len(names) != 2 {
		t.Fatalf("expected 2 servers (global + team), got %d: %v", len(names), names)
	}
}

// TestListAccessibleServers_GlobalAndProject: projectID provided → global + project-scoped.
func TestListAccessibleServers_GlobalAndProject(t *testing.T) {
	db := testDB(t)
	skipIfScopeColumnsAbsent(t, db)

	_, agent1 := seedTenantAgent(t, db)
	ownerUser := seedUserForScopeMutex(t, db)
	projectID := seedProjectForScopeMutex(t, db, ownerUser)

	globalServer := seedMCPServerScoped(t, db, nil, nil)
	projectServer := seedMCPServerScoped(t, db, nil, &projectID)

	grantAgentOnServer(t, db, agent1, globalServer)
	grantAgentOnServer(t, db, agent1, projectServer)

	names := listAccessibleNames(t, db, agent1, nil, &projectID)
	if len(names) != 2 {
		t.Fatalf("expected 2 servers (global + project), got %d: %v", len(names), names)
	}
}

// TestListAccessibleServers_AllThreeScopes: both teamID and projectID provided → all 3.
func TestListAccessibleServers_AllThreeScopes(t *testing.T) {
	db := testDB(t)
	skipIfScopeColumnsAbsent(t, db)

	_, agent1 := seedTenantAgent(t, db)
	_, ownerAgent := seedTenantAgent(t, db)
	teamID, _ := seedTeam(t, db, uuid.Nil, ownerAgent)
	ownerUser := seedUserForScopeMutex(t, db)
	projectID := seedProjectForScopeMutex(t, db, ownerUser)

	globalServer := seedMCPServerScoped(t, db, nil, nil)
	teamServer := seedMCPServerScoped(t, db, &teamID, nil)
	projectServer := seedMCPServerScoped(t, db, nil, &projectID)

	grantAgentOnServer(t, db, agent1, globalServer)
	grantAgentOnServer(t, db, agent1, teamServer)
	grantAgentOnServer(t, db, agent1, projectServer)

	names := listAccessibleNames(t, db, agent1, &teamID, &projectID)
	if len(names) != 3 {
		t.Fatalf("expected 3 servers (global + team + project), got %d: %v", len(names), names)
	}
}

// TestListAccessibleServers_DifferentTeamExcluded: teamID from a different team → server NOT returned.
// This is the scope isolation load-bearing test.
func TestListAccessibleServers_DifferentTeamExcluded(t *testing.T) {
	db := testDB(t)
	skipIfScopeColumnsAbsent(t, db)

	_, agent1 := seedTenantAgent(t, db)
	_, ownerA := seedTenantAgent(t, db)
	_, ownerB := seedTenantAgent(t, db)
	teamA, _ := seedTeam(t, db, uuid.Nil, ownerA)
	teamB, _ := seedTeam(t, db, uuid.Nil, ownerB)

	globalServer := seedMCPServerScoped(t, db, nil, nil)
	teamAServer := seedMCPServerScoped(t, db, &teamA, nil)

	grantAgentOnServer(t, db, agent1, globalServer)
	grantAgentOnServer(t, db, agent1, teamAServer)

	// Context specifies teamB — teamA server must NOT appear.
	names := listAccessibleNames(t, db, agent1, &teamB, nil)
	if containsName(names, "team-A") {
		t.Error("expected team-A scoped server to be excluded when context is team-B")
	}
	if len(names) != 1 {
		t.Errorf("expected 1 server (global only), got %d: %v", len(names), names)
	}
}

// TestListAccessibleServers_NoGrant: agent without any grant → empty slice.
func TestListAccessibleServers_NoGrant(t *testing.T) {
	db := testDB(t)
	skipIfScopeColumnsAbsent(t, db)

	_, agent2 := seedTenantAgent(t, db) // no grant
	seedMCPServerScoped(t, db, nil, nil) // global server exists but agent2 has no grant

	names := listAccessibleNames(t, db, agent2, nil, nil)
	if len(names) != 0 {
		t.Errorf("expected empty slice for agent with no grants, got: %v", names)
	}
}

// TestListAccessibleServers_DisabledServerExcluded: enabled=false server excluded.
func TestListAccessibleServers_DisabledServerExcluded(t *testing.T) {
	db := testDB(t)
	skipIfScopeColumnsAbsent(t, db)

	_, agent1 := seedTenantAgent(t, db)

	serverID := uuid.New()
	name := "mcp-disabled-" + serverID.String()[:8]
	_, err := db.Exec(
		`INSERT INTO mcp_servers (id, name, display_name, transport, enabled, created_by, team_id, project_id)
		 VALUES ($1, $2, $2, 'stdio', false, 'test-user', NULL, NULL)`,
		serverID, name,
	)
	if err != nil {
		t.Fatalf("insert disabled server: %v", err)
	}
	t.Cleanup(func() {
		db.Exec("DELETE FROM mcp_agent_grants WHERE server_id = $1", serverID)
		db.Exec("DELETE FROM mcp_servers WHERE id = $1", serverID)
	})

	grantAgentOnServer(t, db, agent1, serverID)

	names := listAccessibleNames(t, db, agent1, nil, nil)
	if containsName(names, name) {
		t.Errorf("expected disabled server to be excluded, but found it in result: %v", names)
	}
}

// TestListAccessibleServers_DisabledGrantExcluded: grant enabled=false → server excluded.
func TestListAccessibleServers_DisabledGrantExcluded(t *testing.T) {
	db := testDB(t)
	skipIfScopeColumnsAbsent(t, db)

	_, agent1 := seedTenantAgent(t, db)
	serverID := seedMCPServerScoped(t, db, nil, nil)

	grantAgentOnServerDisabled(t, db, agent1, serverID)

	names := listAccessibleNames(t, db, agent1, nil, nil)
	if len(names) != 0 {
		t.Errorf("expected empty when grant is disabled, got: %v", names)
	}
}
