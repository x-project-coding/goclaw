//go:build sqliteonly && integration

package integration

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/nextlevelbuilder/goclaw/internal/store/sqlitestore"
)

// TestSQLiteAgentTypeColumnDropped mirrors the PG check on the SQLite schema.
//
// RED until Phase 02 lands the schema edit.
func TestSQLiteAgentTypeColumnDropped(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := sqlitestore.EnsureSchema(db); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}

	rows, err := db.QueryContext(t.Context(), `PRAGMA table_info(agents)`)
	if err != nil {
		t.Fatalf("pragma table_info: %v", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			cid     int
			name    string
			ctype   string
			notnull int
			dflt    sql.NullString
			pk      int
		)
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			t.Fatalf("scan column: %v", err)
		}
		if name == "agent_type" {
			t.Fatalf("agents.agent_type column must be absent in SQLite schema")
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows err: %v", err)
	}
}
