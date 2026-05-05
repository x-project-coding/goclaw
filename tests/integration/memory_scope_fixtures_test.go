//go:build integration

package integration

import (
	"context"
	"database/sql"
	"fmt"
	"testing"

	"github.com/google/uuid"
)

// ============================================================
// 5D scope test fixtures — creates minimal rows in the 5 scope
// entity tables: agents, users, agent_teams, channel_contacts, projects.
// ============================================================

// scopeFixtures holds IDs of entities created for 5D scope tests.
type scopeFixtures struct {
	AgentID   uuid.UUID
	UserID    uuid.UUID
	TeamID    uuid.UUID
	ContactID uuid.UUID
	ProjectID uuid.UUID
}

// makeScopeFixtures seeds one row in each scope entity table and registers
// cleanup so all rows are removed at the end of the test.
func makeScopeFixtures(t *testing.T, db *sql.DB) scopeFixtures {
	t.Helper()
	ctx := context.Background()

	fx := scopeFixtures{
		AgentID:   uuid.New(),
		UserID:    uuid.New(),
		TeamID:    uuid.New(),
		ContactID: uuid.New(),
		ProjectID: uuid.New(),
	}

	agentKey := "fixture-" + fx.AgentID.String()[:8]
	if _, err := db.ExecContext(ctx, `
		INSERT INTO agents (id, agent_key, status, provider, model, owner_id)
		VALUES ($1, $2, 'active', 'test', 'test-model', 'fixture-owner')
		ON CONFLICT DO NOTHING`,
		fx.AgentID, agentKey); err != nil {
		t.Skipf("scope fixture: seed agent: %v", err)
	}

	if _, err := db.ExecContext(ctx, `
		INSERT INTO users (id, email, password_hash, role, status, user_key, kind)
		VALUES ($1, $2, 'hash', 'member', 'active', $3, 'human')
		ON CONFLICT DO NOTHING`,
		fx.UserID,
		fmt.Sprintf("fx-%s@test.com", fx.UserID.String()[:8]),
		"fxuser-"+fx.UserID.String()[:8]); err != nil {
		t.Skipf("scope fixture: seed user: %v", err)
	}

	if _, err := db.ExecContext(ctx, `
		INSERT INTO agent_teams (id, team_key, name, lead_agent_id)
		VALUES ($1, $2, 'Fixture Team', $3)
		ON CONFLICT DO NOTHING`,
		fx.TeamID, "fxteam-"+fx.TeamID.String()[:8], fx.AgentID); err != nil {
		t.Skipf("scope fixture: seed team: %v", err)
	}

	if _, err := db.ExecContext(ctx, `
		INSERT INTO channel_contacts (id, channel_type, sender_id, display_name)
		VALUES ($1, 'telegram', $2, 'Fixture Contact')
		ON CONFLICT DO NOTHING`,
		fx.ContactID, "fx-sender-"+fx.ContactID.String()[:8]); err != nil {
		t.Skipf("scope fixture: seed contact: %v", err)
	}

	if _, err := db.ExecContext(ctx, `
		INSERT INTO projects (id, name, slug, owner_user_id, status)
		VALUES ($1, 'Fixture Project', $2, $3, 'active')
		ON CONFLICT DO NOTHING`,
		fx.ProjectID, "fxproj-"+fx.ProjectID.String()[:8], fx.UserID); err != nil {
		t.Skipf("scope fixture: seed project: %v", err)
	}

	t.Cleanup(func() {
		db.ExecContext(ctx, "DELETE FROM memory_chunks    WHERE agent_id = $1", fx.AgentID)
		db.ExecContext(ctx, "DELETE FROM memory_documents WHERE agent_id = $1", fx.AgentID)
		db.ExecContext(ctx, "DELETE FROM episodic_summaries WHERE agent_id = $1", fx.AgentID)
		db.ExecContext(ctx, "DELETE FROM projects          WHERE id = $1", fx.ProjectID)
		db.ExecContext(ctx, "DELETE FROM channel_contacts  WHERE id = $1", fx.ContactID)
		db.ExecContext(ctx, "DELETE FROM agent_teams       WHERE id = $1", fx.TeamID)
		db.ExecContext(ctx, "DELETE FROM users             WHERE id = $1", fx.UserID)
		db.ExecContext(ctx, "DELETE FROM agents            WHERE id = $1", fx.AgentID)
	})

	return fx
}

// insertScopedMemDoc inserts a memory_documents row with optional 5D scope fields.
// Returns the inserted row ID. Fails the test immediately on error.
func insertScopedMemDoc(t *testing.T, db *sql.DB,
	agentID uuid.UUID,
	teamID, userID, contactID, projectID *uuid.UUID,
	path, hash string,
) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := db.ExecContext(context.Background(), `
		INSERT INTO memory_documents
		  (id, agent_id, team_id, user_id, contact_id, project_id, path, content, hash)
		VALUES ($1,$2,$3,$4,$5,$6,$7,'',$8)`,
		id, agentID, teamID, userID, contactID, projectID, path, hash)
	if err != nil {
		t.Fatalf("insertScopedMemDoc %q: %v", path, err)
	}
	return id
}

// uuidPtr returns a pointer to the given UUID value.
func uuidPtr(id uuid.UUID) *uuid.UUID { return &id }
