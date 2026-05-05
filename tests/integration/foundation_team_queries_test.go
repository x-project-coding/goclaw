//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
)

// TestFoundation_TeamQueries_PG locks the SELECT/Scan column-count
// agreement on the two team-fetch paths most exercised at runtime:
// GetTeamForAgent (called from agent loop, vault handlers) and
// ListUserTeams (called from web list endpoints). A mismatch between
// the SELECT column list and scanTeamRow's destination list crashes
// every caller, so this test asserts both round-trip metadata.
func TestFoundation_TeamQueries_PG(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	teamStore := pg.NewPGTeamStore(db)
	userStore := pg.NewPGUsersStore(db)
	suffix := uuid.New().String()[:8]

	// Seed a user (lead) so the team has a real owner_id and a grant target.
	owner := &store.User{
		Email:        "team-owner-" + suffix + "@local",
		PasswordHash: "bcrypt-test-stub",
		Role:         "member",
		Status:       "active",
	}
	if err := userStore.Create(ctx, owner); err != nil {
		t.Fatalf("create owner: %v", err)
	}
	ownerIDStr := owner.ID.String()
	t.Cleanup(func() { db.Exec("DELETE FROM users WHERE id = $1", owner.ID) })

	// Seed a lead agent — required by FK on agent_teams.lead_agent_id.
	leadAgentID := store.GenNewID()
	_, err := db.ExecContext(ctx,
		`INSERT INTO agents (id, agent_key, display_name, status, owner_id, model, provider)
		 VALUES ($1, $2, 'lead', 'active', $3, 'noop', 'noop')`,
		leadAgentID, "lead-"+suffix, ownerIDStr,
	)
	if err != nil {
		t.Fatalf("seed lead agent: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM agents WHERE id = $1", leadAgentID) })

	// Create the team — populates metadata + team_key via store.
	team := &store.TeamData{
		Name:        "Smoke Team " + suffix,
		LeadAgentID: leadAgentID,
		Status:      store.TeamStatusActive,
		CreatedBy:   ownerIDStr,
		Metadata:    json.RawMessage(`{"smoke":"team"}`),
	}
	if err := teamStore.CreateTeam(ctx, team); err != nil {
		t.Fatalf("create team: %v", err)
	}
	teamID := team.ID
	t.Cleanup(func() { db.Exec("DELETE FROM agent_teams WHERE id = $1", teamID) })

	// Grant the user access so ListUserTeams returns this team.
	if err := teamStore.GrantTeamAccess(ctx, teamID, ownerIDStr, "member", ownerIDStr); err != nil {
		t.Fatalf("grant: %v", err)
	}

	// GetTeamForAgent — exercises path that crashed pre-fix.
	got, err := teamStore.GetTeamForAgent(ctx, leadAgentID)
	if err != nil {
		t.Fatalf("GetTeamForAgent: %v", err)
	}
	if got == nil || got.ID != teamID {
		t.Fatalf("GetTeamForAgent: want team %s, got %+v", teamID, got)
	}
	if got.TeamKey == "" {
		t.Error("GetTeamForAgent: TeamKey empty")
	}
	if string(got.Metadata) == "" || string(got.Metadata) == "null" {
		t.Errorf("GetTeamForAgent: Metadata not round-tripped, got %q", string(got.Metadata))
	}

	// ListUserTeams — second crash path.
	teams, err := teamStore.ListUserTeams(ctx, ownerIDStr)
	if err != nil {
		t.Fatalf("ListUserTeams: %v", err)
	}
	if len(teams) == 0 {
		t.Fatalf("ListUserTeams: empty result, expected granted team")
	}
	var found *store.TeamData
	for i := range teams {
		if teams[i].ID == teamID {
			found = &teams[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("ListUserTeams: granted team not in result")
	}
	if string(found.Metadata) == "" || string(found.Metadata) == "null" {
		t.Errorf("ListUserTeams: Metadata not round-tripped, got %q", string(found.Metadata))
	}
}
