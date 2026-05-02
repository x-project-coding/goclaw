//go:build e2e && (sqlite || sqliteonly)

package schema_test

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/nextlevelbuilder/goclaw/internal/store/sqlitestore"
)

func sqliteOpenFreshForCols(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:?_foreign_keys=on")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := sqlitestore.EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	return db
}

// TestSqliteNoTenantID asserts no column named tenant_id exists across any table.
func TestSqliteNoTenantID(t *testing.T) {
	db := sqliteOpenFreshForCols(t)
	t.Cleanup(func() { db.Close() })

	// Get all domain table names.
	rows, err := db.Query(`
		SELECT name FROM sqlite_master
		WHERE type = 'table'
		  AND name NOT LIKE 'sqlite_%'
		  AND name != 'schema_version'`)
	if err != nil {
		t.Fatalf("list tables: %v", err)
	}
	var tables []string
	for rows.Next() {
		var tbl string
		if err := rows.Scan(&tbl); err != nil {
			t.Fatalf("scan: %v", err)
		}
		tables = append(tables, tbl)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatalf("rows err: %v", err)
	}

	for _, tbl := range tables {
		tbl := tbl
		t.Run(tbl, func(t *testing.T) {
			cols, err := db.Query("PRAGMA table_info(" + tbl + ")")
			if err != nil {
				t.Fatalf("pragma table_info(%s): %v", tbl, err)
			}
			defer cols.Close()
			for cols.Next() {
				// PRAGMA table_info columns: cid, name, type, notnull, dflt_value, pk
				var cid int
				var name, colType string
				var notNull int
				var dflt sql.NullString
				var pk int
				if err := cols.Scan(&cid, &name, &colType, &notNull, &dflt, &pk); err != nil {
					t.Fatalf("scan column: %v", err)
				}
				if name == "tenant_id" {
					t.Errorf("table %q has tenant_id column — must not exist in v4", tbl)
				}
			}
			if err := cols.Err(); err != nil {
				t.Fatalf("cols err: %v", err)
			}
		})
	}
}

// TestSqliteAgentSessionsRenamed verifies that the old "sessions" table does not
// exist and that "agent_sessions" exists with the expected session_key column.
func TestSqliteAgentSessionsRenamed(t *testing.T) {
	db := sqliteOpenFreshForCols(t)
	t.Cleanup(func() { db.Close() })

	// "sessions" must not exist.
	var count int
	if err := db.QueryRow(`
		SELECT COUNT(*) FROM sqlite_master
		WHERE type = 'table' AND name = 'sessions'`).Scan(&count); err != nil {
		t.Fatalf("query sessions: %v", err)
	}
	if count != 0 {
		t.Error("table 'sessions' still exists — must be renamed to 'agent_sessions' in v4")
	}

	// "agent_sessions" must exist.
	if err := db.QueryRow(`
		SELECT COUNT(*) FROM sqlite_master
		WHERE type = 'table' AND name = 'agent_sessions'`).Scan(&count); err != nil {
		t.Fatalf("query agent_sessions: %v", err)
	}
	if count == 0 {
		t.Fatal("table 'agent_sessions' not found")
	}

	// Must have a session_key column.
	found := false
	cols, err := db.Query("PRAGMA table_info(agent_sessions)")
	if err != nil {
		t.Fatalf("pragma table_info(agent_sessions): %v", err)
	}
	defer cols.Close()
	for cols.Next() {
		var cid int
		var name, colType string
		var notNull int
		var dflt sql.NullString
		var pk int
		if err := cols.Scan(&cid, &name, &colType, &notNull, &dflt, &pk); err != nil {
			t.Fatalf("scan: %v", err)
		}
		if name == "session_key" {
			found = true
		}
	}
	if !found {
		t.Error("agent_sessions.session_key column not found")
	}
}
