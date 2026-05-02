//go:build e2e

package schema_test

import (
	"testing"

	"github.com/nextlevelbuilder/goclaw/tests/e2e/helpers"
)

// fkQuery returns rows from information_schema for a specific FK relationship.
// Returns (table, column, foreign_table, foreign_column, delete_rule).
const fkQuery = `
	SELECT
		kcu.table_name,
		kcu.column_name,
		ccu.table_name  AS foreign_table,
		ccu.column_name AS foreign_column,
		rc.delete_rule
	FROM information_schema.table_constraints AS tc
	JOIN information_schema.key_column_usage AS kcu
		ON tc.constraint_name = kcu.constraint_name
		AND tc.table_schema    = kcu.table_schema
	JOIN information_schema.constraint_column_usage AS ccu
		ON ccu.constraint_name = tc.constraint_name
		AND ccu.table_schema   = tc.table_schema
	JOIN information_schema.referential_constraints AS rc
		ON rc.constraint_name = tc.constraint_name
		AND rc.constraint_schema = tc.table_schema
	WHERE tc.constraint_type = 'FOREIGN KEY'
	  AND tc.table_schema    = 'public'
	  AND tc.table_name      = $1
	  AND kcu.column_name    = $2`

type fkRow struct {
	table, column, foreignTable, foreignColumn, deleteRule string
}

func queryFK(t *testing.T, tableName, colName string) *fkRow {
	t.Helper()
	db := helpers.MustDB(t)
	row := &fkRow{}
	err := db.QueryRow(fkQuery, tableName, colName).Scan(
		&row.table, &row.column, &row.foreignTable, &row.foreignColumn, &row.deleteRule,
	)
	if err != nil {
		return nil
	}
	return row
}

// TestForeignKeysOnUsers asserts that critical owner columns have FK to users(id).
func TestForeignKeysOnUsers(t *testing.T) {
	t.Parallel()
	helpers.MustLoadEnv()

	cases := []struct {
		table  string
		column string
	}{
		{"agents", "owner_user_id"},
		{"api_keys", "owner_user_id"},
		{"agent_teams", "owner_user_id"},
		{"cron_jobs", "user_id"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.table+"."+tc.column, func(t *testing.T) {
			t.Parallel()
			row := queryFK(t, tc.table, tc.column)
			if row == nil {
				t.Errorf("%s.%s: no FK constraint found — expected REFERENCES users(id)", tc.table, tc.column)
				return
			}
			if row.foreignTable != "users" || row.foreignColumn != "id" {
				t.Errorf("%s.%s: FK points to %s(%s), expected users(id)",
					tc.table, tc.column, row.foreignTable, row.foreignColumn)
			}
		})
	}
}

// TestNullableUserFKs asserts that specific user_id columns are NULLABLE
// (these tables retain orphaned rows when a user is deleted).
func TestNullableUserFKs(t *testing.T) {
	t.Parallel()
	helpers.MustLoadEnv()
	db := helpers.MustDB(t)

	nullable := []struct {
		table  string
		column string
	}{
		{"kg_entities", "user_id"},
		{"kg_relations", "user_id"},
		{"paired_devices", "user_id"},
		{"activity_logs", "user_id"},
		{"memory_documents", "user_id"},
	}

	for _, tc := range nullable {
		tc := tc
		t.Run(tc.table+"."+tc.column, func(t *testing.T) {
			t.Parallel()
			var isNullable string
			err := db.QueryRow(`
				SELECT is_nullable FROM information_schema.columns
				WHERE table_schema = 'public'
				  AND table_name   = $1
				  AND column_name  = $2`, tc.table, tc.column).Scan(&isNullable)
			if err != nil {
				t.Fatalf("query nullable for %s.%s: %v", tc.table, tc.column, err)
			}
			if isNullable != "YES" {
				t.Errorf("%s.%s must be NULLABLE (is_nullable=%q)", tc.table, tc.column, isNullable)
			}
		})
	}
}
