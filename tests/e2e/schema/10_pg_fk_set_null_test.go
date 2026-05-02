//go:build e2e

package schema_test

import (
	"testing"

	"github.com/nextlevelbuilder/goclaw/tests/e2e/helpers"
)

// criticalSetNullFKs lists FKs that must use ON DELETE SET NULL so that
// user-owned data survives a user record deletion (soft-delete preferred;
// hard-delete vacuum is deferred to v4.x).
var criticalSetNullFKs = []struct {
	table  string
	column string
}{
	{"agent_sessions", "user_id"},
	{"memory_documents", "user_id"},
	{"vault_documents", "owner_user_id"}, // v4 renames user_id → owner_user_id in vault_documents
	{"kg_entities", "user_id"},
	{"kg_relations", "user_id"},
	{"cron_jobs", "user_id"},
	{"paired_devices", "user_id"},
	{"activity_logs", "user_id"},
}

// TestCriticalFKsSetNull queries information_schema.referential_constraints to
// confirm every critical user FK uses DELETE_RULE = 'SET NULL'.
// Using CASCADE here would allow a single DELETE FROM users to silently wipe
// large volumes of user-owned data.
func TestCriticalFKsSetNull(t *testing.T) {
	t.Parallel()
	helpers.MustLoadEnv()
	db := helpers.MustDB(t)

	for _, tc := range criticalSetNullFKs {
		tc := tc
		t.Run(tc.table+"."+tc.column, func(t *testing.T) {
			t.Parallel()

			var deleteRule string
			err := db.QueryRow(`
				SELECT rc.delete_rule
				FROM information_schema.table_constraints    AS tc
				JOIN information_schema.key_column_usage     AS kcu
					ON  tc.constraint_name = kcu.constraint_name
					AND tc.table_schema    = kcu.table_schema
				JOIN information_schema.referential_constraints AS rc
					ON  rc.constraint_name   = tc.constraint_name
					AND rc.constraint_schema = tc.table_schema
				WHERE tc.constraint_type = 'FOREIGN KEY'
				  AND tc.table_schema    = 'public'
				  AND tc.table_name      = $1
				  AND kcu.column_name    = $2`,
				tc.table, tc.column,
			).Scan(&deleteRule)

			if err != nil {
				t.Fatalf("%s.%s: no FK found (query error: %v)", tc.table, tc.column, err)
			}
			if deleteRule != "SET NULL" {
				t.Errorf("%s.%s: delete_rule = %q, want 'SET NULL'", tc.table, tc.column, deleteRule)
			}
		})
	}
}

// TestUsersDeletedAtColumn asserts users.deleted_at exists, is timestamptz,
// and is nullable — required for soft-delete support.
func TestUsersDeletedAtColumn(t *testing.T) {
	t.Parallel()
	helpers.MustLoadEnv()
	db := helpers.MustDB(t)

	var dataType, isNullable string
	err := db.QueryRow(`
		SELECT data_type, is_nullable
		FROM information_schema.columns
		WHERE table_schema = 'public'
		  AND table_name   = 'users'
		  AND column_name  = 'deleted_at'`).Scan(&dataType, &isNullable)
	if err != nil {
		t.Fatalf("users.deleted_at column not found: %v", err)
	}
	if dataType != "timestamp with time zone" {
		t.Errorf("users.deleted_at: data_type = %q, want 'timestamp with time zone'", dataType)
	}
	if isNullable != "YES" {
		t.Errorf("users.deleted_at: is_nullable = %q, want 'YES'", isNullable)
	}
}
