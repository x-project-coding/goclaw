//go:build integration

package integration

import (
	"context"
	"database/sql"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
)

// e2eShareInsert is a thin wrapper that mirrors the AgentAccessStore
// CreateShare contract through raw SQL so we don't have to wire the full
// store aggregate just for this test.
func e2eShareInsert(t *testing.T, db *sql.DB, in store.AgentShareInput) {
	t.Helper()
	if _, err := db.Exec(
		`INSERT INTO agent_shares (id, agent_id, shared_with_user_id, shared_with_team_id, role, created_by)
		 VALUES (uuid_generate_v7(), $1, $2, $3, $4, $5)`,
		in.AgentID, in.SharedWithUserID, in.SharedWithTeamID, in.Role, in.CreatedBy,
	); err != nil {
		t.Fatalf("e2e share insert: %v", err)
	}
}

// TestSharingEndToEnd_FullFlow walks through the canonical sharing journey:
//
//	create users + team + agent → share to user (viewer) + team (member) →
//	resolve roles for owner / explicit / implicit / stranger →
//	revoke user share → resolve again → delete team → resolve again
//
// Exercises PG schema (Phase 04), resolver (Phase 05), share flag struct
// fields (Phase 02), and target-mutex CHECK end-to-end.
func TestSharingEndToEnd_FullFlow(t *testing.T) {
	db := testDB(t)
	resolver := permissions.NewAgentAccessResolver(db)
	ctx := context.Background()

	// Actors.
	ownerA := seedUserForShares(t, db)
	userB := seedUserForShares(t, db)
	userC := seedUserForShares(t, db)
	stranger := seedUserForShares(t, db)
	team := seedTeamForShares(t, db, ownerA)
	grantUserToTeam(t, db, team, userB, "member")

	// Agent X owned by A, share_workspace=true (collapse user zone),
	// share_memory=false (memory still per-user).
	agentX := seedAgentForShares(t, db, ownerA)
	if _, err := db.Exec(
		`UPDATE agents SET share_workspace = TRUE, share_memory = FALSE WHERE id = $1`,
		agentX); err != nil {
		t.Fatalf("set share flags: %v", err)
	}

	// Verify share flags read back via the AgentStore (Phase 02 contract).
	pgStore := pg.NewPGAgentStore(db)
	ag, err := pgStore.GetByIDUnscoped(ctx, agentX)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if !ag.ShareWorkspace || ag.ShareMemory {
		t.Errorf("share flags read-back: ShareWorkspace=%v ShareMemory=%v want (true,false)",
			ag.ShareWorkspace, ag.ShareMemory)
	}

	// Step 1: explicit user grant — userC as viewer.
	e2eShareInsert(t, db, store.AgentShareInput{
		AgentID: agentX, SharedWithUserID: &userC,
		Role: store.ShareRoleViewer, CreatedBy: ownerA,
	})
	// Step 2: explicit team grant — team T as member (userB is in team).
	e2eShareInsert(t, db, store.AgentShareInput{
		AgentID: agentX, SharedWithTeamID: &team,
		Role: store.ShareRoleMember, CreatedBy: ownerA,
	})

	// Resolve roles for all four actors.
	checks := []struct {
		name string
		user uuid.UUID
		want string
	}{
		{"owner_A", ownerA, permissions.ShareOwner},
		{"team_member_B", userB, permissions.ShareMember},
		{"explicit_C", userC, permissions.ShareViewer},
		{"stranger", stranger, permissions.ShareNone},
	}
	for _, c := range checks {
		c := c
		t.Run("resolve_"+c.name, func(t *testing.T) {
			got, err := resolver.ResolveRole(ctx, c.user, agentX)
			if err != nil {
				t.Fatalf("resolve: %v", err)
			}
			if got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}

	// Step 3: revoke userC's explicit share → resolves to none.
	if _, err := db.Exec(
		`DELETE FROM agent_shares WHERE agent_id = $1 AND shared_with_user_id = $2`,
		agentX, userC); err != nil {
		t.Fatalf("revoke userC: %v", err)
	}
	if got, _ := resolver.ResolveRole(ctx, userC, agentX); got != permissions.ShareNone {
		t.Errorf("post-revoke userC: got %q, want none", got)
	}

	// Step 4: delete team → userB's implicit grant cascades away.
	if _, err := db.Exec(`DELETE FROM agent_teams WHERE id = $1`, team); err != nil {
		t.Fatalf("delete team: %v", err)
	}
	if got, _ := resolver.ResolveRole(ctx, userB, agentX); got != permissions.ShareNone {
		t.Errorf("post-team-delete userB: got %q, want none", got)
	}

	// Step 5: owner role is unaffected by share churn.
	if got, _ := resolver.ResolveRole(ctx, ownerA, agentX); got != permissions.ShareOwner {
		t.Errorf("owner role: got %q, want owner", got)
	}
}

// TestSharingEndToEnd_TargetMutexAtAPI verifies the DB CHECK constraint
// blocks both both-target and neither-target inserts, regardless of role.
func TestSharingEndToEnd_TargetMutexAtAPI(t *testing.T) {
	db := testDB(t)
	owner := seedUserForShares(t, db)
	target := seedUserForShares(t, db)
	team := seedTeamForShares(t, db, owner)
	agent := seedAgentForShares(t, db, owner)

	// Both set.
	if _, err := db.Exec(
		`INSERT INTO agent_shares (id, agent_id, shared_with_user_id, shared_with_team_id, role, created_by)
		 VALUES (uuid_generate_v7(), $1, $2, $3, 'viewer', $4)`,
		agent, target, team, owner); err == nil {
		t.Error("both-target must be rejected by CHECK")
	}
	// Both null.
	if _, err := db.Exec(
		`INSERT INTO agent_shares (id, agent_id, shared_with_user_id, shared_with_team_id, role, created_by)
		 VALUES (uuid_generate_v7(), $1, NULL, NULL, 'viewer', $2)`,
		agent, owner); err == nil {
		t.Error("neither-target must be rejected by CHECK")
	}
}
