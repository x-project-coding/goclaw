//go:build integration

package integration

import (
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
)

// TestMCPScopeIsolation_CrossTeam is the P0 invariant for team-scope isolation.
// Agent-1 (team A) must NOT see team-B scoped servers, and vice versa.
// Both agents have active grants on all 3 servers to confirm grant alone is
// insufficient — scope context is also required.
func TestMCPScopeIsolation_CrossTeam(t *testing.T) {
	db := testDB(t)
	skipIfScopeColumnsAbsent(t, db)

	// Two independent team owners + their teams
	_, ownerA := seedTenantAgent(t, db)
	_, ownerB := seedTenantAgent(t, db)
	teamA, _ := seedTeam(t, db, uuid.Nil, ownerA)
	teamB, _ := seedTeam(t, db, uuid.Nil, ownerB)

	// One agent per team (plus owner agents, but we test with fresh agents)
	_, agent1 := seedTenantAgent(t, db) // operates in team A context
	_, agent2 := seedTenantAgent(t, db) // operates in team B context

	// Three servers: 1 global, 1 team-A, 1 team-B
	globalServer := seedMCPServerScoped(t, db, nil, nil)
	teamAServer := seedMCPServerScoped(t, db, &teamA, nil)
	teamBServer := seedMCPServerScoped(t, db, &teamB, nil)

	// Both agents have grants on all three servers
	for _, srv := range []uuid.UUID{globalServer, teamAServer, teamBServer} {
		grantAgentOnServer(t, db, agent1, srv)
		grantAgentOnServer(t, db, agent2, srv)
	}

	mcpStore := pg.NewPGMCPServerStore(db, "")

	// Agent 1 context = team A
	a1Servers, err := mcpStore.ListAccessibleServers(t.Context(), agent1, &teamA, nil)
	if err != nil {
		t.Fatalf("ListAccessibleServers agent1: %v", err)
	}
	a1Names := serverNames(a1Servers)
	if len(a1Names) != 2 {
		t.Errorf("agent1 (team A context): expected 2 servers (global + teamA), got %d: %v", len(a1Names), a1Names)
	}
	for _, srv := range a1Servers {
		if srv.TeamID != nil && *srv.TeamID == teamB {
			t.Errorf("agent1 (team A context): must NOT see team-B server, got: %v", *srv.TeamID)
		}
	}

	// Agent 2 context = team B
	a2Servers, err := mcpStore.ListAccessibleServers(t.Context(), agent2, &teamB, nil)
	if err != nil {
		t.Fatalf("ListAccessibleServers agent2: %v", err)
	}
	a2Names := serverNames(a2Servers)
	if len(a2Names) != 2 {
		t.Errorf("agent2 (team B context): expected 2 servers (global + teamB), got %d: %v", len(a2Names), a2Names)
	}
	for _, srv := range a2Servers {
		if srv.TeamID != nil && *srv.TeamID == teamA {
			t.Errorf("agent2 (team B context): must NOT see team-A server, got: %v", *srv.TeamID)
		}
	}
}

// TestMCPScopeIsolation_CrossProject is the P0 invariant for project-scope isolation.
// Agent scoped to project X must NOT see project Y servers, even with a grant.
func TestMCPScopeIsolation_CrossProject(t *testing.T) {
	db := testDB(t)
	skipIfScopeColumnsAbsent(t, db)

	ownerUser := seedUserForScopeMutex(t, db)
	projectX := seedProjectForScopeMutex(t, db, ownerUser)
	projectY := seedProjectForScopeMutex(t, db, ownerUser)

	_, agentX := seedTenantAgent(t, db) // operates in project X context
	_, agentY := seedTenantAgent(t, db) // operates in project Y context

	globalServer := seedMCPServerScoped(t, db, nil, nil)
	projectXServer := seedMCPServerScoped(t, db, nil, &projectX)
	projectYServer := seedMCPServerScoped(t, db, nil, &projectY)

	// Both agents granted on all three
	for _, srv := range []uuid.UUID{globalServer, projectXServer, projectYServer} {
		grantAgentOnServer(t, db, agentX, srv)
		grantAgentOnServer(t, db, agentY, srv)
	}

	mcpStore := pg.NewPGMCPServerStore(db, "")

	// Agent X context = project X
	xServers, err := mcpStore.ListAccessibleServers(t.Context(), agentX, nil, &projectX)
	if err != nil {
		t.Fatalf("ListAccessibleServers agentX: %v", err)
	}
	if len(xServers) != 2 {
		t.Errorf("agentX (project X): expected 2 servers (global + projectX), got %d", len(xServers))
	}
	for _, srv := range xServers {
		if srv.ProjectID != nil && *srv.ProjectID == projectY {
			t.Errorf("agentX must NOT see project-Y server")
		}
	}

	// Agent Y context = project Y
	yServers, err := mcpStore.ListAccessibleServers(t.Context(), agentY, nil, &projectY)
	if err != nil {
		t.Fatalf("ListAccessibleServers agentY: %v", err)
	}
	if len(yServers) != 2 {
		t.Errorf("agentY (project Y): expected 2 servers (global + projectY), got %d", len(yServers))
	}
	for _, srv := range yServers {
		if srv.ProjectID != nil && *srv.ProjectID == projectX {
			t.Errorf("agentY must NOT see project-X server")
		}
	}
}

// serverNames extracts sorted names from a server slice.
func serverNames(servers []store.MCPServerData) []string {
	names := make([]string, 0, len(servers))
	for _, s := range servers {
		names = append(names, s.Name)
	}
	return names
}
