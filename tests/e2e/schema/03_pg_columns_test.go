//go:build e2e

package schema_test

import (
	"testing"

	"github.com/nextlevelbuilder/goclaw/tests/e2e/helpers"
)

// TestNoTenantIDColumns asserts the v4 schema has zero tenant_id columns.
func TestNoTenantIDColumns(t *testing.T) {
	t.Parallel()
	helpers.MustLoadEnv()
	db := helpers.MustDB(t)

	var count int
	err := db.QueryRow(`
		SELECT COUNT(*) FROM information_schema.columns
		WHERE table_schema = 'public' AND column_name = 'tenant_id'`).Scan(&count)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if count != 0 {
		// Surface which tables still have the column.
		rows, qErr := db.Query(`
			SELECT table_name FROM information_schema.columns
			WHERE table_schema = 'public' AND column_name = 'tenant_id'
			ORDER BY table_name`)
		if qErr == nil {
			defer rows.Close()
			for rows.Next() {
				var tbl string
				_ = rows.Scan(&tbl)
				t.Logf("  table with tenant_id: %s", tbl)
			}
		}
		t.Errorf("found %d tenant_id column(s) — v4 schema must have 0", count)
	}
}

// TestUserIDIsUUID asserts every user_id / owner_user_id column is type uuid.
func TestUserIDIsUUID(t *testing.T) {
	t.Parallel()
	helpers.MustLoadEnv()
	db := helpers.MustDB(t)

	rows, err := db.Query(`
		SELECT table_name, column_name, data_type
		FROM information_schema.columns
		WHERE table_schema = 'public'
		  AND column_name IN ('user_id', 'owner_user_id')
		  AND data_type != 'uuid'
		ORDER BY table_name, column_name`)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()

	var violations []string
	for rows.Next() {
		var tbl, col, dtype string
		if err := rows.Scan(&tbl, &col, &dtype); err != nil {
			t.Fatalf("scan: %v", err)
		}
		violations = append(violations, tbl+"."+col+" ("+dtype+")")
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	if len(violations) > 0 {
		for _, v := range violations {
			t.Logf("  non-uuid user id column: %s", v)
		}
		t.Errorf("%d user_id/owner_user_id column(s) are not uuid type", len(violations))
	}
}

// TestSessionsRenamed asserts the v3 `sessions` table no longer exists and
// the renamed `agent_sessions` table exists with the expected session_key column.
func TestSessionsRenamed(t *testing.T) {
	t.Parallel()
	helpers.MustLoadEnv()
	db := helpers.MustDB(t)

	var oldExists bool
	if err := db.QueryRow(`
		SELECT EXISTS (
			SELECT 1 FROM pg_catalog.pg_tables
			WHERE schemaname = 'public' AND tablename = 'sessions')`,
	).Scan(&oldExists); err != nil {
		t.Fatalf("query sessions: %v", err)
	}
	if oldExists {
		t.Error("v3 table 'sessions' must not exist in v4 schema — expected rename to 'agent_sessions'")
	}

	var newExists bool
	if err := db.QueryRow(`
		SELECT EXISTS (
			SELECT 1 FROM pg_catalog.pg_tables
			WHERE schemaname = 'public' AND tablename = 'agent_sessions')`,
	).Scan(&newExists); err != nil {
		t.Fatalf("query agent_sessions: %v", err)
	}
	if !newExists {
		t.Error("table 'agent_sessions' must exist in v4 schema")
	}

	// Verify session_key column exists on agent_sessions.
	var colExists bool
	if err := db.QueryRow(`
		SELECT EXISTS (
			SELECT 1 FROM information_schema.columns
			WHERE table_schema = 'public'
			  AND table_name = 'agent_sessions'
			  AND column_name = 'session_key')`,
	).Scan(&colExists); err != nil {
		t.Fatalf("query session_key: %v", err)
	}
	if !colExists {
		t.Error("agent_sessions must have a 'session_key' column")
	}
}
