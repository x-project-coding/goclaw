//go:build e2e && (sqlite || sqliteonly)

package schema_test

import (
	"database/sql"
	"sort"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/nextlevelbuilder/goclaw/internal/store/sqlitestore"
	"github.com/nextlevelbuilder/goclaw/tests/e2e/helpers"
)

// pgSqliteTableExceptions lists tables intentionally absent from SQLite but
// present in PG. Excluded from TestParityTableNames set-equality check.
var pgSqliteTableExceptions = map[string]string{
	"vault_versions": "versioning not supported in SQLite lite edition",
}

// TestParityTableNames asserts that the set of SQLite table names equals
// the PG set minus the known exceptions.
func TestParityTableNames(t *testing.T) {
	helpers.MustLoadEnv()
	helpers.MigrateUp(t)

	// --- PG table set ---
	pgDB := helpers.MustDB(t)
	pgRows, err := pgDB.Query(`
		SELECT tablename FROM pg_catalog.pg_tables
		WHERE schemaname = 'public'
		  AND tablename NOT IN ('schema_migrations', 'data_migrations')`)
	if err != nil {
		t.Fatalf("pg list tables: %v", err)
	}
	pgTables := map[string]bool{}
	for pgRows.Next() {
		var name string
		if err := pgRows.Scan(&name); err != nil {
			t.Fatalf("pg scan: %v", err)
		}
		pgTables[name] = true
	}
	pgRows.Close()
	if err := pgRows.Err(); err != nil {
		t.Fatalf("pg rows err: %v", err)
	}

	// --- SQLite table set ---
	sqliteDB, err := sql.Open("sqlite", ":memory:?_foreign_keys=on")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer sqliteDB.Close()
	if err := sqlitestore.EnsureSchema(sqliteDB); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	sqRows, err := sqliteDB.Query(`
		SELECT name FROM sqlite_master
		WHERE type = 'table'
		  AND name NOT LIKE 'sqlite_%'
		  AND name != 'schema_version'`)
	if err != nil {
		t.Fatalf("sqlite list tables: %v", err)
	}
	sqTables := map[string]bool{}
	for sqRows.Next() {
		var name string
		if err := sqRows.Scan(&name); err != nil {
			t.Fatalf("sqlite scan: %v", err)
		}
		sqTables[name] = true
	}
	sqRows.Close()
	if err := sqRows.Err(); err != nil {
		t.Fatalf("sqlite rows err: %v", err)
	}

	// PG tables that should also exist in SQLite (after removing exceptions).
	for pgTbl := range pgTables {
		if _, isException := pgSqliteTableExceptions[pgTbl]; isException {
			continue
		}
		if !sqTables[pgTbl] {
			t.Errorf("PG table %q missing from SQLite schema", pgTbl)
		}
	}

	// SQLite tables that don't exist in PG (unexpected extras).
	for sqTbl := range sqTables {
		if !pgTables[sqTbl] {
			t.Errorf("SQLite table %q not found in PG schema (unexpected extra)", sqTbl)
		}
	}
}

// TestParityColumnNames checks that for each shared table, the column names
// in SQLite match those in PG. Types may differ (TEXT vs UUID, TEXT vs JSONB, etc.)
// but names must be identical.
func TestParityColumnNames(t *testing.T) {
	helpers.MustLoadEnv()
	helpers.MigrateUp(t)

	pgDB := helpers.MustDB(t)

	sqliteDB, err := sql.Open("sqlite", ":memory:?_foreign_keys=on")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { sqliteDB.Close() })
	if err := sqlitestore.EnsureSchema(sqliteDB); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}

	// PG: columns per table from information_schema.
	// Exclude generated columns (tsvector) — SQLite omits them.
	pgColsQuery := `
		SELECT column_name FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = $1
		  AND data_type NOT IN ('tsvector', 'USER-DEFINED')
		ORDER BY column_name`

	// SQLite: columns from PRAGMA table_info.
	for _, tbl := range sqliteExpectedTables {
		tbl := tbl
		t.Run(tbl, func(t *testing.T) {

			// Fetch PG column names for this table.
			pgRows, err := pgDB.Query(pgColsQuery, tbl)
			if err != nil {
				t.Fatalf("pg columns(%s): %v", tbl, err)
			}
			pgCols := map[string]bool{}
			for pgRows.Next() {
				var col string
				if err := pgRows.Scan(&col); err != nil {
					t.Fatalf("scan: %v", err)
				}
				pgCols[col] = true
			}
			pgRows.Close()
			if err := pgRows.Err(); err != nil {
				t.Fatalf("pg rows err: %v", err)
			}

			// Fetch SQLite column names for this table.
			sqRows, err := sqliteDB.Query("PRAGMA table_info(" + tbl + ")")
			if err != nil {
				t.Fatalf("pragma table_info(%s): %v", tbl, err)
			}
			sqCols := map[string]bool{}
			for sqRows.Next() {
				var cid int
				var name, colType string
				var notNull int
				var dflt sql.NullString
				var pk int
				if err := sqRows.Scan(&cid, &name, &colType, &notNull, &dflt, &pk); err != nil {
					t.Fatalf("scan: %v", err)
				}
				sqCols[name] = true
			}
			sqRows.Close()
			if err := sqRows.Err(); err != nil {
				t.Fatalf("sqlite rows err: %v", err)
			}

			// PG columns that should also exist in SQLite.
			var missing []string
			for col := range pgCols {
				if !sqCols[col] {
					missing = append(missing, col)
				}
			}
			if len(missing) > 0 {
				sort.Strings(missing)
				t.Errorf("table %q: PG columns missing from SQLite: %v", tbl, missing)
			}

			// SQLite columns that are absent in PG (unexpected extras).
			var extra []string
			for col := range sqCols {
				if !pgCols[col] {
					extra = append(extra, col)
				}
			}
			if len(extra) > 0 {
				sort.Strings(extra)
				t.Errorf("table %q: SQLite has extra columns not in PG: %v", tbl, extra)
			}
		})
	}
}
