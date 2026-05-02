//go:build e2e && (sqlite || sqliteonly)

package schema_test

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/nextlevelbuilder/goclaw/internal/store/sqlitestore"
)

// TestSqliteSchemaApply verifies that EnsureSchema succeeds on a fresh
// in-memory SQLite database. Also exercises the schema_version bookkeeping.
func TestSqliteSchemaApply(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:?_foreign_keys=on")
	if err != nil {
		t.Fatalf("open in-memory sqlite: %v", err)
	}
	defer db.Close()

	if err := sqlitestore.EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema failed: %v", err)
	}

	// Verify schema_version row was written.
	var v int
	if err := db.QueryRow("SELECT version FROM schema_version LIMIT 1").Scan(&v); err != nil {
		t.Fatalf("read schema_version: %v", err)
	}
	if v != 1 {
		t.Errorf("expected schema_version=1, got %d", v)
	}
}

// TestSqliteVersionConst asserts that the exported constant matches v4 expectations.
func TestSqliteVersionConst(t *testing.T) {
	if sqlitestore.SchemaVersion != 1 {
		t.Errorf("expected SchemaVersion=1, got %d", sqlitestore.SchemaVersion)
	}
}
