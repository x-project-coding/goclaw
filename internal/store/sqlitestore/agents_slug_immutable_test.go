//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/google/uuid"

	storelib "github.com/nextlevelbuilder/goclaw/internal/store"
)

// TestSQLiteAgentStore_Update_AgentKeyImmutable pins the
// agent_key-cannot-change invariant the FE settings tab now relies on.
// Update() silently strips agent_key from the updates map (see agents.go
// line ~169 + the comment about FS-coupled slugs); this test fails if a
// future refactor accidentally allows the rename through.
//
// NOTE: per plan #1 P06 T11 the long-term fix is an explicit
// `slug_immutable` error code instead of a silent strip. This test pins
// the *current* behaviour so the silent-strip stays well-defined; an
// explicit-error refactor must update this test in the same change.
func TestSQLiteAgentStore_Update_AgentKeyImmutable(t *testing.T) {
	db, _, agentID := newAgentUpdateTestFixture(t)
	store := NewSQLiteAgentStore(db)
	ctx := storelib.WithRole(context.Background(), "admin")

	// The shared fixture leaves display_name NULL; the GetByID scanner
	// can't take NULL into a string column, so seed it explicitly here.
	if _, err := db.Exec(`UPDATE agents SET display_name = 'Test' WHERE id = ?`, agentID.String()); err != nil {
		t.Fatalf("seed display_name: %v", err)
	}

	// Read the original key so we can assert it survives.
	ag, err := store.GetByID(ctx, agentID)
	if err != nil || ag == nil {
		t.Fatalf("GetByID: %v (ag=%v)", err, ag)
	}
	originalKey := ag.AgentKey
	if originalKey == "" {
		t.Fatalf("seed produced empty agent_key")
	}

	// Caller passes a malicious agent_key + a benign field. The benign
	// field must apply, the agent_key must NOT.
	if err := store.Update(ctx, agentID, map[string]any{
		"agent_key":    "renamed-attempt",
		"display_name": "ok",
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}

	got, err := store.GetByID(ctx, agentID)
	if err != nil || got == nil {
		t.Fatalf("re-read: %v (got=%v)", err, got)
	}
	if got.AgentKey != originalKey {
		t.Errorf("agent_key mutated: was %q, now %q", originalKey, got.AgentKey)
	}
	if got.DisplayName != "ok" {
		t.Errorf("display_name not applied: got %q, want \"ok\"", got.DisplayName)
	}
}

// TestSQLiteTeamStore_Update_TeamKeyImmutable mirrors the agent assertion
// against teams. Same FS-coupling rationale — team_key is the workspace
// folder name and must not change after creation.
func TestSQLiteTeamStore_Update_TeamKeyImmutable(t *testing.T) {
	db, err := OpenDB(filepath.Join(t.TempDir(), "team_slug_immutable.db"))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	// Seed: owner user → lead agent → team. Each FK target must exist.
	ownerID := "owner-slug-test"
	if _, err := db.Exec(`INSERT INTO users (id, email, password_hash, user_key, role, status, created_at, updated_at)
		VALUES (?, 'team-slug@example.test', 'x', 'owner', 'admin', 'active', datetime('now'), datetime('now'))`,
		ownerID); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	leadAgentID := uuid.Must(uuid.NewV7()).String()
	if _, err := db.Exec(
		`INSERT INTO agents (id, agent_key, display_name, status, provider, model, owner_id)
		 VALUES (?, 'lead-'||substr(?,1,8), 'Lead', 'active', 'test', 'test-model', ?)`,
		leadAgentID, leadAgentID, ownerID); err != nil {
		t.Fatalf("seed lead agent: %v", err)
	}
	teamID := uuid.Must(uuid.NewV7())
	originalKey := "alpha-team"
	// settings + metadata are NOT NULL JSON columns — seed with empty JSON to
	// keep the SQLite scanner happy (it can't take NULL → *json.RawMessage).
	if _, err := db.Exec(
		`INSERT INTO agent_teams (id, team_key, name, lead_agent_id, status, settings, metadata, created_by, created_at, updated_at)
		 VALUES (?, ?, 'Alpha', ?, 'active', '{}', '{}', ?, datetime('now'), datetime('now'))`,
		teamID.String(), originalKey, leadAgentID, ownerID); err != nil {
		t.Fatalf("seed team: %v", err)
	}

	store := NewSQLiteTeamStore(db)
	ctx := storelib.WithRole(context.Background(), "admin")

	if err := store.UpdateTeam(ctx, teamID, map[string]any{
		"team_key": "renamed-attempt",
		"name":     "Alpha-Renamed",
	}); err != nil {
		t.Fatalf("UpdateTeam: %v", err)
	}

	// Query the column directly so we don't depend on the full TeamData
	// scan path (json.RawMessage settings/metadata vs modernc/sqlite text
	// has its own quirks unrelated to this invariant).
	var gotKey, gotName string
	if err := db.QueryRow(`SELECT team_key, name FROM agent_teams WHERE id = ?`, teamID.String()).
		Scan(&gotKey, &gotName); err != nil {
		t.Fatalf("re-read: %v", err)
	}
	if gotKey != originalKey {
		t.Errorf("team_key mutated: was %q, now %q", originalKey, gotKey)
	}
	if gotName != "Alpha-Renamed" {
		t.Errorf("name not applied: got %q, want \"Alpha-Renamed\"", gotName)
	}
}
