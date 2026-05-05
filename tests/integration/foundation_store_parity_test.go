//go:build sqliteonly && integration

// Cross-store metadata parity sweep: same INSERT through both PG and SQLite
// must produce equivalent metadata round-trips for all 13 entity tables.
// Uses direct SQL (same as foundation_metadata_crud_test.go) so no store
// constructor impedance. Entities 1-7 covered here; 8-13 in part B.
package integration

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"

	"github.com/nextlevelbuilder/goclaw/internal/store/sqlitestore"
)

// paritySweepMeta is the payload written for every parity insert.
var paritySweepMeta = map[string]any{"k": "v", "n": float64(42)}

// paritySweepMetaJSON is the JSON representation of paritySweepMeta.
const paritySweepMetaJSON = `{"k":"v","n":42}`

// newSweepSQLiteDB opens a fresh :memory: SQLite DB pinned to a single
// connection so every Exec and Query share the same in-memory database.
// (:memory: creates a separate DB per connection; MaxOpenConns(1) prevents
// cross-connection visibility gaps in the raw-SQL parity sweep.)
func newSweepSQLiteDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite :memory:: %v", err)
	}
	// Pin to one connection so all ops see the same in-memory database.
	db.SetMaxOpenConns(1)
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

// assertPGMeta reads the metadata column from a PG table+pk and asserts it
// equals paritySweepMeta.
func assertPGMeta(t *testing.T, label, table, pkCol string, pkVal any, db *sql.DB) {
	t.Helper()
	var raw string
	q := `SELECT metadata::text FROM ` + table + ` WHERE ` + pkCol + ` = $1`
	if err := db.QueryRowContext(context.Background(), q, pkVal).Scan(&raw); err != nil {
		t.Errorf("PG/%s: scan metadata: %v", label, err)
		return
	}
	checkMetaKeys(t, "PG/"+label, raw)
}

// assertSQLiteMeta reads the metadata column from a SQLite table+pk and
// asserts it equals paritySweepMeta.
func assertSQLiteMeta(t *testing.T, label, table, pkCol string, pkVal any, db *sql.DB) {
	t.Helper()
	var raw string
	q := `SELECT metadata FROM ` + table + ` WHERE ` + pkCol + ` = ?`
	if err := db.QueryRowContext(context.Background(), q, pkVal).Scan(&raw); err != nil {
		t.Errorf("SQLite/%s: scan metadata: %v", label, err)
		return
	}
	checkMetaKeys(t, "SQLite/"+label, raw)
}

// checkMetaKeys unmarshals raw JSON and verifies all keys in paritySweepMeta match.
func checkMetaKeys(t *testing.T, label, raw string) {
	t.Helper()
	var got map[string]any
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Errorf("%s: unmarshal %q: %v", label, raw, err)
		return
	}
	for wk, wv := range paritySweepMeta {
		if got[wk] != wv {
			t.Errorf("%s: metadata[%q] = %v, want %v", label, wk, got[wk], wv)
		}
	}
}

// TestFoundation_StoreMetadataParity_A verifies metadata round-trips on both
// PG and SQLite stores for: agents, agent_teams, agent_shares, agent_links,
// memory_documents, skills, skill_versions.
func TestFoundation_StoreMetadataParity_A(t *testing.T) {
	pgDB := testDB(t)
	sqliteDB := newSweepSQLiteDB(t)
	ctx := context.Background()

	// seedAgent inserts a minimal agent row in both DBs and returns both IDs.
	seedAgent := func(t *testing.T) (pgID, sqlID uuid.UUID) {
		t.Helper()
		pgID = uuid.New()
		pgKey := "pa-" + pgID.String()[:8]
		if _, err := pgDB.ExecContext(ctx,
			`INSERT INTO agents (id, agent_key, status, provider, model, owner_id, metadata)
			 VALUES ($1,$2,'active','test','m','owner',$3::jsonb)`,
			pgID, pgKey, paritySweepMetaJSON); err != nil {
			t.Fatalf("PG seed agent: %v", err)
		}
		t.Cleanup(func() { pgDB.Exec("DELETE FROM agents WHERE id = $1", pgID) })

		sqlID = uuid.New()
		sqlKey := "sa-" + sqlID.String()[:8]
		if _, err := sqliteDB.ExecContext(ctx,
			`INSERT INTO agents (id, agent_key, status, provider, model, owner_id, metadata)
			 VALUES (?,?,'active','test','m','owner',?)`,
			sqlID.String(), sqlKey, paritySweepMetaJSON); err != nil {
			t.Fatalf("SQLite seed agent: %v", err)
		}
		return pgID, sqlID
	}

	t.Run("agents", func(t *testing.T) {
		pgID, sqlID := seedAgent(t)
		assertPGMeta(t, "agents", "agents", "id", pgID, pgDB)
		assertSQLiteMeta(t, "agents", "agents", "id", sqlID.String(), sqliteDB)
	})

	t.Run("agent_teams", func(t *testing.T) {
		pgAgentID, sqlAgentID := seedAgent(t)

		pgID := uuid.New()
		pgKey := "ptm-" + pgID.String()[:8]
		if _, err := pgDB.ExecContext(ctx,
			`INSERT INTO agent_teams (id, name, lead_agent_id, status, settings, created_by, team_key, metadata)
			 VALUES ($1,$2,$3,'active','{}','test',$4,$5::jsonb)`,
			pgID, "tm"+pgKey, pgAgentID, pgKey, paritySweepMetaJSON); err != nil {
			t.Fatalf("PG agent_teams insert: %v", err)
		}
		t.Cleanup(func() { pgDB.Exec("DELETE FROM agent_teams WHERE id = $1", pgID) })

		sqlID := uuid.New()
		sqlKey := "stm-" + sqlID.String()[:8]
		if _, err := sqliteDB.ExecContext(ctx,
			`INSERT INTO agent_teams (id, name, lead_agent_id, status, settings, created_by, team_key, metadata)
			 VALUES (?,?,?,'active','{}','test',?,?)`,
			sqlID.String(), "tm"+sqlKey, sqlAgentID.String(), sqlKey, paritySweepMetaJSON); err != nil {
			t.Fatalf("SQLite agent_teams insert: %v", err)
		}
		assertPGMeta(t, "agent_teams", "agent_teams", "id", pgID, pgDB)
		assertSQLiteMeta(t, "agent_teams", "agent_teams", "id", sqlID.String(), sqliteDB)
	})

	t.Run("agent_shares", func(t *testing.T) {
		pgAgentID, sqlAgentID := seedAgent(t)

		pgUserID := uuid.New()
		pgUserKey := "pu-" + pgUserID.String()[:8]
		pgDB.ExecContext(ctx,
			`INSERT INTO users (id, email, password_hash, role, status, user_key, kind)
			 VALUES ($1,$2,'h','member','active',$3,'human')`,
			pgUserID, pgUserKey+"@x.com", pgUserKey)
		t.Cleanup(func() { pgDB.Exec("DELETE FROM users WHERE id = $1", pgUserID) })

		sqlUserID := uuid.New()
		sqlUserKey := "su-" + sqlUserID.String()[:8]
		sqliteDB.ExecContext(ctx,
			`INSERT INTO users (id, email, password_hash, role, status, user_key, kind)
			 VALUES (?,?,'h','member','active',?,'human')`,
			sqlUserID.String(), sqlUserKey+"@x.com", sqlUserKey)

		pgID := uuid.New()
		if _, err := pgDB.ExecContext(ctx,
			`INSERT INTO agent_shares (id, agent_id, shared_with_user_id, role, created_by, metadata)
			 VALUES ($1,$2,$3,'viewer',$3,$4::jsonb)`,
			pgID, pgAgentID, pgUserID, paritySweepMetaJSON); err != nil {
			t.Fatalf("PG agent_shares: %v", err)
		}
		t.Cleanup(func() { pgDB.Exec("DELETE FROM agent_shares WHERE id = $1", pgID) })

		sqlID := uuid.New()
		if _, err := sqliteDB.ExecContext(ctx,
			`INSERT INTO agent_shares (id, agent_id, shared_with_user_id, role, created_by, metadata)
			 VALUES (?,?,?,'viewer',?,?)`,
			sqlID.String(), sqlAgentID.String(), sqlUserID.String(), sqlUserID.String(), paritySweepMetaJSON); err != nil {
			t.Fatalf("SQLite agent_shares: %v", err)
		}
		assertPGMeta(t, "agent_shares", "agent_shares", "id", pgID, pgDB)
		assertSQLiteMeta(t, "agent_shares", "agent_shares", "id", sqlID.String(), sqliteDB)
	})

	t.Run("agent_links", func(t *testing.T) {
		pgSrc, sqlSrc := seedAgent(t)
		pgDst, sqlDst := seedAgent(t)

		pgID := uuid.New()
		if _, err := pgDB.ExecContext(ctx,
			`INSERT INTO agent_links (id, source_agent_id, target_agent_id, direction, created_by, metadata)
			 VALUES ($1,$2,$3,'outbound','test',$4::jsonb)`,
			pgID, pgSrc, pgDst, paritySweepMetaJSON); err != nil {
			t.Fatalf("PG agent_links: %v", err)
		}
		t.Cleanup(func() { pgDB.Exec("DELETE FROM agent_links WHERE id = $1", pgID) })

		sqlID := uuid.New()
		if _, err := sqliteDB.ExecContext(ctx,
			`INSERT INTO agent_links (id, source_agent_id, target_agent_id, direction, created_by, metadata)
			 VALUES (?,?,?,'outbound','test',?)`,
			sqlID.String(), sqlSrc.String(), sqlDst.String(), paritySweepMetaJSON); err != nil {
			t.Fatalf("SQLite agent_links: %v", err)
		}
		assertPGMeta(t, "agent_links", "agent_links", "id", pgID, pgDB)
		assertSQLiteMeta(t, "agent_links", "agent_links", "id", sqlID.String(), sqliteDB)
	})

	t.Run("memory_documents", func(t *testing.T) {
		pgAgentID, sqlAgentID := seedAgent(t)

		pgID := uuid.New()
		if _, err := pgDB.ExecContext(ctx,
			`INSERT INTO memory_documents (id, agent_id, path, content, hash, metadata)
			 VALUES ($1,$2,'/p','c','h',$3::jsonb)`,
			pgID, pgAgentID, paritySweepMetaJSON); err != nil {
			t.Fatalf("PG memory_documents: %v", err)
		}
		t.Cleanup(func() { pgDB.Exec("DELETE FROM memory_documents WHERE id = $1", pgID) })

		sqlID := uuid.New()
		if _, err := sqliteDB.ExecContext(ctx,
			`INSERT INTO memory_documents (id, agent_id, path, content, hash, metadata)
			 VALUES (?,?,'/p','c','h',?)`,
			sqlID.String(), sqlAgentID.String(), paritySweepMetaJSON); err != nil {
			t.Fatalf("SQLite memory_documents: %v", err)
		}
		assertPGMeta(t, "memory_documents", "memory_documents", "id", pgID, pgDB)
		assertSQLiteMeta(t, "memory_documents", "memory_documents", "id", sqlID.String(), sqliteDB)
	})

	t.Run("skills", func(t *testing.T) {
		pgID := uuid.New()
		pgSlug := "psk-" + pgID.String()[:8]
		if _, err := pgDB.ExecContext(ctx,
			`INSERT INTO skills (id, name, slug, owner_id, visibility, file_path, file_size, metadata)
			 VALUES ($1,$2,$3,'owner','private','/t',0,$4::jsonb)`,
			pgID, "skill-"+pgSlug, pgSlug, paritySweepMetaJSON); err != nil {
			t.Fatalf("PG skills: %v", err)
		}
		t.Cleanup(func() { pgDB.Exec("DELETE FROM skills WHERE id = $1", pgID) })

		sqlID := uuid.New()
		sqlSlug := "ssk-" + sqlID.String()[:8]
		if _, err := sqliteDB.ExecContext(ctx,
			`INSERT INTO skills (id, name, slug, owner_id, visibility, file_path, file_size, metadata)
			 VALUES (?,?,?,'owner','private','/t',0,?)`,
			sqlID.String(), "skill-"+sqlSlug, sqlSlug, paritySweepMetaJSON); err != nil {
			t.Fatalf("SQLite skills: %v", err)
		}
		assertPGMeta(t, "skills", "skills", "id", pgID, pgDB)
		assertSQLiteMeta(t, "skills", "skills", "id", sqlID.String(), sqliteDB)
	})

	t.Run("skill_versions", func(t *testing.T) {
		pgSkillID := uuid.New()
		pgSkillSlug := "pvsk-" + pgSkillID.String()[:8]
		pgDB.ExecContext(ctx,
			`INSERT INTO skills (id, name, slug, owner_id, visibility, file_path, file_size)
			 VALUES ($1,$2,$3,'owner','private','/t',0)`,
			pgSkillID, "s-"+pgSkillSlug, pgSkillSlug)
		t.Cleanup(func() {
			pgDB.Exec("DELETE FROM skill_versions WHERE skill_id = $1", pgSkillID)
			pgDB.Exec("DELETE FROM skills WHERE id = $1", pgSkillID)
		})

		sqlSkillID := uuid.New()
		sqlSkillSlug := "svsk-" + sqlSkillID.String()[:8]
		sqliteDB.ExecContext(ctx,
			`INSERT INTO skills (id, name, slug, owner_id, visibility, file_path, file_size)
			 VALUES (?,?,?,'owner','private','/t',0)`,
			sqlSkillID.String(), "s-"+sqlSkillSlug, sqlSkillSlug)

		pgID := uuid.New()
		if _, err := pgDB.ExecContext(ctx,
			`INSERT INTO skill_versions (id, skill_id, version, file_hash, file_path, file_size, content, metadata)
			 VALUES ($1,$2,1,'h','/t',0,'c',$3::jsonb)`,
			pgID, pgSkillID, paritySweepMetaJSON); err != nil {
			t.Fatalf("PG skill_versions: %v", err)
		}

		sqlID := uuid.New()
		if _, err := sqliteDB.ExecContext(ctx,
			`INSERT INTO skill_versions (id, skill_id, version, file_hash, file_path, file_size, content, metadata)
			 VALUES (?,?,1,'h','/t',0,'c',?)`,
			sqlID.String(), sqlSkillID.String(), paritySweepMetaJSON); err != nil {
			t.Fatalf("SQLite skill_versions: %v", err)
		}
		assertPGMeta(t, "skill_versions", "skill_versions", "id", pgID, pgDB)
		assertSQLiteMeta(t, "skill_versions", "skill_versions", "id", sqlID.String(), sqliteDB)
	})
}
