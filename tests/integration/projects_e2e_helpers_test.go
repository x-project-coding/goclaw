//go:build integration

package integration

import (
	"context"
	"database/sql"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
)

// e2eUser inserts a minimal users row into the PG test DB and returns its UUID.
func e2eUser(t *testing.T, db *sql.DB) uuid.UUID {
	t.Helper()
	id := uuid.New()
	suf := id.String()[:8]
	_, err := db.Exec(
		`INSERT INTO users (id, email, password_hash, display_name, role, kind, user_key)
		 VALUES ($1, $2, 'x', 'u', 'member', 'human', $3)`,
		id, "e2e-"+suf+"@local", "e2e-"+suf,
	)
	if err != nil {
		t.Fatalf("e2eUser: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM users WHERE id = $1", id) })
	return id
}

// e2eCreateProject inserts a minimal active project via the PG store and
// registers a t.Cleanup to delete it.
func e2eCreateProject(t *testing.T, ctx context.Context, db *sql.DB, ownerID uuid.UUID) *store.Project {
	t.Helper()
	ps := pg.NewPGProjectStore(db)
	p := &store.Project{
		Slug:        "e2e-" + uuid.New().String()[:8],
		OwnerUserID: ownerID,
		Status:      "active",
	}
	if err := ps.Create(ctx, p); err != nil {
		t.Fatalf("e2eCreateProject: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM projects WHERE id = $1", p.ID) })
	return p
}

// e2eTeam inserts a minimal agent + agent_teams row and returns the team UUID.
func e2eTeam(t *testing.T, db *sql.DB, ownerID uuid.UUID) uuid.UUID {
	t.Helper()
	agentID := uuid.New()
	_, err := db.Exec(
		`INSERT INTO agents (id, agent_key, status, provider, model, owner_id, owner_user_id)
		 VALUES ($1, $2, 'active', 'test', 'm', $3, $4)`,
		agentID, "e2e-lead-"+agentID.String()[:8], "owner", ownerID,
	)
	if err != nil {
		t.Fatalf("e2eTeam.agent: %v", err)
	}
	teamID := uuid.New()
	_, err = db.Exec(
		`INSERT INTO agent_teams (id, team_key, name, owner_user_id, lead_agent_id, created_by)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		teamID, "e2e-team-"+teamID.String()[:8], "E2E Team", ownerID, agentID, ownerID.String(),
	)
	if err != nil {
		t.Fatalf("e2eTeam.team: %v", err)
	}
	t.Cleanup(func() {
		db.Exec("DELETE FROM agent_teams WHERE id = $1", teamID)
		db.Exec("DELETE FROM agents WHERE id = $1", agentID)
	})
	return teamID
}

// e2eContact inserts a group-type channel contact and returns its UUID.
func e2eContact(t *testing.T, db *sql.DB) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := db.Exec(
		`INSERT INTO channel_contacts (id, channel_type, sender_id, contact_type)
		 VALUES ($1, 'telegram', $2, 'group')`,
		id, "grp-e2e-"+id.String()[:8],
	)
	if err != nil {
		t.Fatalf("e2eContact: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM channel_contacts WHERE id = $1", id) })
	return id
}
