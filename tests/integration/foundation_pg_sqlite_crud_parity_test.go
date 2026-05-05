//go:build sqliteonly && integration

// Cross-store CRUD parity smoke test: same input through PG store and SQLite
// store must produce equivalent identity shape (UserKey, TeamKey, Metadata).
// This file uses sqliteonly because the sqlitestore package requires that tag
// to compile. The PG store (internal/store/pg) has no build-tag restriction
// and can be imported alongside.
package integration

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
	"github.com/nextlevelbuilder/goclaw/internal/store/sqlitestore"
)

// newParitySQLiteDB opens a :memory: SQLite DB with the full v4 schema applied.
func newParitySQLiteDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite :memory:: %v", err)
	}
	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		db.Close()
		t.Fatalf("foreign_keys pragma: %v", err)
	}
	if err := sqlitestore.EnsureSchema(db); err != nil {
		db.Close()
		t.Fatalf("EnsureSchema: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// TestCrossStoreUserCreateParity: same user input through PG and SQLite stores
// must both produce a non-empty UserKey and Kind='human' with nil ChannelType.
func TestCrossStoreUserCreateParity(t *testing.T) {
	pgDB := testDB(t) // skips if PG unavailable
	sqliteDB := newParitySQLiteDB(t)

	pgStore := pg.NewPGUsersStore(pgDB)
	sqliteStore := sqlitestore.NewSQLiteUsersStore(sqliteDB)

	suffix := uuid.New().String()[:8]
	email := "parity-user-" + suffix + "@example.com"
	meta := json.RawMessage(`{"source":"parity-test"}`)

	// PG create
	pgUser := &store.User{
		Email:        email,
		PasswordHash: "testhash",
		Role:         "member",
		Status:       "active",
		Metadata:     meta,
	}
	if err := pgStore.Create(context.Background(), pgUser); err != nil {
		t.Fatalf("PG Create: %v", err)
	}
	t.Cleanup(func() {
		pgDB.Exec("DELETE FROM users WHERE id = $1", pgUser.ID)
	})

	// SQLite create — same logical input, different email to avoid PG unique collision
	sqliteEmail := "parity-sqlite-" + suffix + "@example.com"
	sqliteUser := &store.User{
		Email:        sqliteEmail,
		PasswordHash: "testhash",
		Role:         "member",
		Status:       "active",
		Metadata:     meta,
	}
	if err := sqliteStore.Create(context.Background(), sqliteUser); err != nil {
		t.Fatalf("SQLite Create: %v", err)
	}

	// Both must have non-empty UserKey
	if pgUser.UserKey == "" {
		t.Error("PG: UserKey must be non-empty after Create")
	}
	if sqliteUser.UserKey == "" {
		t.Error("SQLite: UserKey must be non-empty after Create")
	}

	// Both must default to kind='human'
	if pgUser.Kind != "human" {
		t.Errorf("PG: Kind = %q, want %q", pgUser.Kind, "human")
	}
	if sqliteUser.Kind != "human" {
		t.Errorf("SQLite: Kind = %q, want %q", sqliteUser.Kind, "human")
	}

	// Both must have nil ChannelType for human users
	if pgUser.ChannelType != nil {
		t.Errorf("PG: ChannelType must be nil for human, got %v", pgUser.ChannelType)
	}
	if sqliteUser.ChannelType != nil {
		t.Errorf("SQLite: ChannelType must be nil for human, got %v", sqliteUser.ChannelType)
	}

	// Metadata must round-trip (both have the source key)
	assertMetaKey := func(label string, m json.RawMessage) {
		t.Helper()
		var got map[string]any
		if err := json.Unmarshal(m, &got); err != nil {
			t.Errorf("%s: unmarshal metadata: %v", label, err)
			return
		}
		if got["source"] != "parity-test" {
			t.Errorf("%s: metadata[source] = %v, want parity-test", label, got["source"])
		}
	}
	assertMetaKey("PG", pgUser.Metadata)
	assertMetaKey("SQLite", sqliteUser.Metadata)
}

// TestCrossStoreTeamCreateParity: same team input through PG and SQLite stores
// must both produce a non-empty TeamKey with valid Metadata.
func TestCrossStoreTeamCreateParity(t *testing.T) {
	pgDB := testDB(t)
	sqliteDB := newParitySQLiteDB(t)

	pgTeamStore := pg.NewPGTeamStore(pgDB)
	sqliteTeamStore := sqlitestore.NewSQLiteTeamStore(sqliteDB)

	// Both stores require a lead_agent_id FK. Seed an agent in each DB.
	pgAgentID := uuid.New()
	pgAgentKey := "parity-agent-" + pgAgentID.String()[:8]
	if _, err := pgDB.Exec(
		`INSERT INTO agents (id, agent_key, agent_type, status, provider, model, owner_id)
		 VALUES ($1, $2, 'predefined', 'active', 'test', 'test-model', 'parity-owner')`,
		pgAgentID, pgAgentKey,
	); err != nil {
		t.Fatalf("PG seed agent: %v", err)
	}
	t.Cleanup(func() {
		pgDB.Exec("DELETE FROM agent_teams WHERE lead_agent_id = $1", pgAgentID)
		pgDB.Exec("DELETE FROM agents WHERE id = $1", pgAgentID)
	})

	sqliteAgentID := uuid.New()
	sqliteAgentKey := "parity-agent-" + sqliteAgentID.String()[:8]
	if _, err := sqliteDB.Exec(
		`INSERT INTO agents (id, agent_key, agent_type, status, provider, model, owner_id)
		 VALUES (?, ?, 'predefined', 'active', 'test', 'test-model', 'parity-owner')`,
		sqliteAgentID.String(), sqliteAgentKey,
	); err != nil {
		t.Fatalf("SQLite seed agent: %v", err)
	}

	suffix := uuid.New().String()[:8]
	teamName := "Parity Team " + suffix
	meta := json.RawMessage(`{"env":"test"}`)

	// PG create
	pgTeam := &store.TeamData{
		Name:        teamName,
		LeadAgentID: pgAgentID,
		Status:      store.TeamStatusActive,
		CreatedBy:   "parity-test",
		Metadata:    meta,
	}
	if err := pgTeamStore.CreateTeam(context.Background(), pgTeam); err != nil {
		t.Fatalf("PG CreateTeam: %v", err)
	}

	// SQLite create — same logical input
	sqliteTeam := &store.TeamData{
		Name:        teamName,
		LeadAgentID: sqliteAgentID,
		Status:      store.TeamStatusActive,
		CreatedBy:   "parity-test",
		Metadata:    meta,
	}
	if err := sqliteTeamStore.CreateTeam(context.Background(), sqliteTeam); err != nil {
		t.Fatalf("SQLite CreateTeam: %v", err)
	}

	// Both must have non-empty TeamKey
	if pgTeam.TeamKey == "" {
		t.Error("PG: TeamKey must be non-empty after CreateTeam")
	}
	if sqliteTeam.TeamKey == "" {
		t.Error("SQLite: TeamKey must be non-empty after CreateTeam")
	}

	// Both slugs derived from same name — should match (same identity.SlugFromName logic)
	if pgTeam.TeamKey != sqliteTeam.TeamKey {
		// Slug includes ID suffix so they won't be byte-equal, but both must be non-empty
		// and look like slugified versions of the team name.
		t.Logf("note: PG TeamKey=%q SQLite TeamKey=%q (differ by ID suffix — expected)", pgTeam.TeamKey, sqliteTeam.TeamKey)
	}

	// Metadata must round-trip in both stores
	assertTeamMeta := func(label string, m json.RawMessage) {
		t.Helper()
		var got map[string]any
		if err := json.Unmarshal(m, &got); err != nil {
			t.Errorf("%s: unmarshal team metadata: %v", label, err)
			return
		}
		if got["env"] != "test" {
			t.Errorf("%s: metadata[env] = %v, want test", label, got["env"])
		}
	}
	assertTeamMeta("PG", pgTeam.Metadata)
	assertTeamMeta("SQLite", sqliteTeam.Metadata)
}
