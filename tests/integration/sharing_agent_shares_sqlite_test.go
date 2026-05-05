//go:build sqliteonly && integration

package integration

import (
	"database/sql"
	"testing"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"

	"github.com/nextlevelbuilder/goclaw/internal/store/sqlitestore"
)

// sqliteSharesDB returns a fresh in-memory SQLite DB with v4 schema applied.
func sqliteSharesDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		t.Fatalf("enable fk: %v", err)
	}
	if err := sqlitestore.EnsureSchema(db); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	return db
}

func sqliteSeedUser(t *testing.T, db *sql.DB) string {
	t.Helper()
	id := uuid.New().String()
	suffix := id[:8]
	_, err := db.Exec(
		`INSERT INTO users (id, email, password_hash, display_name, role, kind, user_key)
		 VALUES (?, ?, 'x', 'u', 'member', 'human', ?)`,
		id, "share-"+suffix+"@local", "share-"+suffix,
	)
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}
	return id
}

func sqliteSeedTeam(t *testing.T, db *sql.DB, ownerID string) string {
	t.Helper()
	lead := sqliteSeedAgent(t, db, ownerID)
	id := uuid.New().String()
	suffix := id[:8]
	_, err := db.Exec(
		`INSERT INTO agent_teams (id, team_key, name, owner_user_id, lead_agent_id, created_by)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		id, "team-"+suffix, "Team "+suffix, ownerID, lead, ownerID,
	)
	if err != nil {
		t.Fatalf("seed team: %v", err)
	}
	return id
}

func sqliteSeedAgent(t *testing.T, db *sql.DB, ownerID string) string {
	t.Helper()
	id := uuid.New().String()
	suffix := id[:8]
	_, err := db.Exec(
		`INSERT INTO agents (id, agent_key, status, provider, model, owner_id, owner_user_id)
		 VALUES (?, ?, 'active', 'test', 'm', ?, ?)`,
		id, "shared-"+suffix, "owner-"+suffix, ownerID,
	)
	if err != nil {
		t.Fatalf("seed agent: %v", err)
	}
	return id
}

func sqliteInsertShare(db *sql.DB, agentID string, userID, teamID *string, role, createdBy string) error {
	_, err := db.Exec(
		`INSERT INTO agent_shares (id, agent_id, shared_with_user_id, shared_with_team_id, role, created_by)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		uuid.New().String(), agentID, userID, teamID, role, createdBy)
	return err
}

// TestSQLiteAgentSharesColumns asserts SQLite mirror has rebuilt agent_shares
// columns (shared_with_user_id, shared_with_team_id, role, created_by, etc.)
// and legacy user_id/granted_by are gone.
func TestSQLiteAgentSharesColumns(t *testing.T) {
	db := sqliteSharesDB(t)
	rows, err := db.Query(`PRAGMA table_info(agent_shares)`)
	if err != nil {
		t.Fatalf("pragma: %v", err)
	}
	defer rows.Close()
	cols := map[string]bool{}
	for rows.Next() {
		var (
			cid     int
			name    string
			typeAff string
			notnull int
			dflt    sql.NullString
			pk      int
		)
		if err := rows.Scan(&cid, &name, &typeAff, &notnull, &dflt, &pk); err != nil {
			t.Fatalf("scan: %v", err)
		}
		cols[name] = true
	}
	for _, want := range []string{"id", "agent_id", "shared_with_user_id", "shared_with_team_id", "role", "metadata", "created_by", "created_at", "updated_at"} {
		if !cols[want] {
			t.Errorf("agent_shares.%s missing in sqlite schema", want)
		}
	}
	for _, gone := range []string{"user_id", "granted_by"} {
		if cols[gone] {
			t.Errorf("agent_shares.%s must be removed", gone)
		}
	}
}

func TestSQLiteAgentSharesRoleEnum(t *testing.T) {
	db := sqliteSharesDB(t)
	owner := sqliteSeedUser(t, db)
	target := sqliteSeedUser(t, db)
	agent := sqliteSeedAgent(t, db, owner)

	for _, bad := range []string{"owner", "user", ""} {
		if err := sqliteInsertShare(db, agent, &target, nil, bad, owner); err == nil {
			t.Errorf("role=%q must be rejected", bad)
		}
	}
	for _, good := range []string{"viewer", "member", "editor"} {
		fresh := sqliteSeedUser(t, db)
		if err := sqliteInsertShare(db, agent, &fresh, nil, good, owner); err != nil {
			t.Errorf("role=%q must be accepted: %v", good, err)
		}
	}
}

func TestSQLiteAgentSharesTargetMutex(t *testing.T) {
	db := sqliteSharesDB(t)
	owner := sqliteSeedUser(t, db)
	target := sqliteSeedUser(t, db)
	team := sqliteSeedTeam(t, db, owner)
	agent := sqliteSeedAgent(t, db, owner)

	if err := sqliteInsertShare(db, agent, nil, nil, "viewer", owner); err == nil {
		t.Error("both NULL must be rejected")
	}
	if err := sqliteInsertShare(db, agent, &target, &team, "viewer", owner); err == nil {
		t.Error("both set must be rejected")
	}
	if err := sqliteInsertShare(db, agent, &target, nil, "viewer", owner); err != nil {
		t.Errorf("user-only must be accepted: %v", err)
	}
	if err := sqliteInsertShare(db, agent, nil, &team, "member", owner); err != nil {
		t.Errorf("team-only must be accepted: %v", err)
	}
}

func TestSQLiteAgentSharesUnique(t *testing.T) {
	db := sqliteSharesDB(t)
	owner := sqliteSeedUser(t, db)
	target := sqliteSeedUser(t, db)
	agent := sqliteSeedAgent(t, db, owner)

	if err := sqliteInsertShare(db, agent, &target, nil, "viewer", owner); err != nil {
		t.Fatalf("first: %v", err)
	}
	if err := sqliteInsertShare(db, agent, &target, nil, "editor", owner); err == nil {
		t.Error("duplicate (agent,user) must be rejected")
	}
}
