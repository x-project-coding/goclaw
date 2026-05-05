//go:build sqliteonly && integration

package integration

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/nextlevelbuilder/goclaw/internal/store/sqlitestore"
)

// TestSQLiteAgentShareFlagColumns asserts the SQLite mirror of the v4
// sharing-model split exposes share_workspace and share_memory with
// INTEGER affinity (BOOLEAN encoded), NOT NULL, default 0.
//
// RED until Phase 02/07 schema lands.
func TestSQLiteAgentShareFlagColumns(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := sqlitestore.EnsureSchema(db); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}

	rows, err := db.Query(`PRAGMA table_info(agents)`)
	if err != nil {
		t.Fatalf("pragma table_info: %v", err)
	}
	defer rows.Close()

	type col struct {
		typeAff string
		notnull int
		dflt    sql.NullString
	}
	cols := map[string]col{}
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
		cols[name] = col{typeAff: typeAff, notnull: notnull, dflt: dflt}
	}

	for _, want := range []string{"share_workspace", "share_memory"} {
		c, ok := cols[want]
		if !ok {
			t.Errorf("agents.%s missing in sqlite schema", want)
			continue
		}
		if c.notnull != 1 {
			t.Errorf("agents.%s: want NOT NULL, got notnull=%d", want, c.notnull)
		}
		if !c.dflt.Valid {
			t.Errorf("agents.%s: want default 0, got NULL default", want)
		} else if c.dflt.String != "0" && c.dflt.String != "FALSE" && c.dflt.String != "false" {
			t.Errorf("agents.%s: want default 0/FALSE, got %q", want, c.dflt.String)
		}
	}

	if _, ok := cols["workspace_sharing"]; ok {
		t.Errorf("agents.workspace_sharing must be removed from sqlite schema")
	}
}
