//go:build sqliteonly && integration

package integration

import (
	"context"
	"database/sql"
	"testing"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/sqlitestore"
)

// newProjectsParityDB opens a fresh in-memory SQLite DB, enables FK enforcement
// explicitly (PRAGMA is connection-scoped; the pool may open new connections),
// and applies the full v4 schema via EnsureSchema.
func newProjectsParityDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	// PRAGMA foreign_keys is connection-scoped in SQLite. Set it explicitly here
	// so it applies to every connection, mirroring the pattern used in
	// sqliteSharesDB and newBootstrapSQLiteDB helpers.
	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		t.Fatalf("PRAGMA foreign_keys ON: %v", err)
	}

	if err := sqlitestore.EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}

	// Assert FK enforcement is confirmed active after schema application.
	var fkEnabled int
	if err := db.QueryRow("PRAGMA foreign_keys").Scan(&fkEnabled); err != nil {
		t.Fatalf("PRAGMA foreign_keys query: %v", err)
	}
	if fkEnabled != 1 {
		t.Fatalf("PRAGMA foreign_keys = %d, want 1 (FK enforcement must be ON)", fkEnabled)
	}
	return db
}

// sqliteE2EUser inserts a minimal users row. The full UUID is used as user_key
// and email to guarantee uniqueness even when UUIDs share a millisecond timestamp prefix.
func sqliteE2EUser(t *testing.T, db *sql.DB) string {
	t.Helper()
	id := uuid.Must(uuid.NewV7()).String()
	_, err := db.Exec(
		`INSERT INTO users (id, email, password_hash, display_name, role, kind, user_key)
		 VALUES (?, ?, 'x', 'u', 'member', 'human', ?)`,
		id, id+"@local", id,
	)
	if err != nil {
		t.Fatalf("sqliteE2EUser: %v", err)
	}
	return id
}

// sqliteE2EProject creates a minimal active project via the SQLite project store.
// The slug uses the tail of the UUID (post-timestamp bytes) for uniqueness.
func sqliteE2EProject(t *testing.T, ctx context.Context, db *sql.DB, ownerID string) *store.Project {
	t.Helper()
	ps := sqlitestore.NewSQLiteProjectStore(db)
	ownerUUID, err := uuid.Parse(ownerID)
	if err != nil {
		t.Fatalf("sqliteE2EProject: parse ownerID: %v", err)
	}
	raw := uuid.Must(uuid.NewV7()).String()
	p := &store.Project{
		Slug:        "sqle-" + raw[len(raw)-12:],
		OwnerUserID: ownerUUID,
		Status:      "active",
	}
	if err := ps.Create(ctx, p); err != nil {
		t.Fatalf("sqliteE2EProject: Create: %v", err)
	}
	return p
}

// sqliteE2ETeam inserts a minimal agent + agent_teams row and returns the team ID string.
func sqliteE2ETeam(t *testing.T, db *sql.DB, ownerID string) string {
	t.Helper()
	agentID := uuid.Must(uuid.NewV7()).String()
	_, err := db.Exec(
		`INSERT INTO agents (id, agent_key, status, provider, model, owner_id, owner_user_id)
		 VALUES (?, ?, 'active', 'test', 'm', ?, ?)`,
		agentID, "sqle-ag-"+agentID[:8], ownerID, ownerID,
	)
	if err != nil {
		t.Fatalf("sqliteE2ETeam.agent: %v", err)
	}
	teamID := uuid.Must(uuid.NewV7()).String()
	_, err = db.Exec(
		`INSERT INTO agent_teams (id, team_key, name, owner_user_id, lead_agent_id, created_by)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		teamID, "sqle-team-"+teamID[:8], "SQLite E2E Team", ownerID, agentID, ownerID,
	)
	if err != nil {
		t.Fatalf("sqliteE2ETeam.team: %v", err)
	}
	return teamID
}

// sqliteE2EContact inserts a group-type channel contact and returns its ID string.
func sqliteE2EContact(t *testing.T, db *sql.DB) string {
	t.Helper()
	id := uuid.Must(uuid.NewV7()).String()
	_, err := db.Exec(
		`INSERT INTO channel_contacts (id, channel_type, sender_id, contact_type)
		 VALUES (?, 'telegram', ?, 'group')`,
		id, "grp-sqle-"+id[:8],
	)
	if err != nil {
		t.Fatalf("sqliteE2EContact: %v", err)
	}
	return id
}

// sqliteE2ESession inserts an agent_sessions row with an optional project_id
// and returns the session_key.
func sqliteE2ESession(t *testing.T, db *sql.DB, agentID string, projectID *string) string {
	t.Helper()
	sessionKey := "sqle:sess:" + uuid.Must(uuid.NewV7()).String()[:8]
	_, err := db.Exec(
		`INSERT INTO agent_sessions (id, session_key, messages, agent_id, project_id)
		 VALUES (?, ?, '[]', ?, ?)`,
		uuid.Must(uuid.NewV7()).String(), sessionKey, agentID, projectID,
	)
	if err != nil {
		t.Fatalf("sqliteE2ESession: %v", err)
	}
	return sessionKey
}

// sqliteE2EAgent inserts a minimal agents row and returns its ID string.
func sqliteE2EAgent(t *testing.T, db *sql.DB, ownerID string) string {
	t.Helper()
	agentID := uuid.Must(uuid.NewV7()).String()
	_, err := db.Exec(
		`INSERT INTO agents (id, agent_key, status, provider, model, owner_id)
		 VALUES (?, ?, 'active', 'test', 'm', ?)`,
		agentID, "sqle-agt-"+agentID[:8], ownerID,
	)
	if err != nil {
		t.Fatalf("sqliteE2EAgent: %v", err)
	}
	return agentID
}
