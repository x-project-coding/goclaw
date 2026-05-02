//go:build e2e

package schema_test

import (
	"testing"

	"github.com/nextlevelbuilder/goclaw/tests/e2e/helpers"
)

// TestUserSessionsFamilyIDColumn asserts user_sessions.family_id is uuid type
// and NOT NULL. The family_id column implements token-family theft detection:
// all refresh tokens in a rotation chain share the same family_id so that
// detecting a reuse of any token in the family revokes the entire chain.
func TestUserSessionsFamilyIDColumn(t *testing.T) {
	t.Parallel()
	helpers.MustLoadEnv()
	db := helpers.MustDB(t)

	var dataType, isNullable string
	err := db.QueryRow(`
		SELECT data_type, is_nullable
		FROM information_schema.columns
		WHERE table_schema = 'public'
		  AND table_name   = 'user_sessions'
		  AND column_name  = 'family_id'`).Scan(&dataType, &isNullable)
	if err != nil {
		t.Fatalf("query user_sessions.family_id: %v — column may be missing", err)
	}
	if dataType != "uuid" {
		t.Errorf("user_sessions.family_id: data_type = %q, want 'uuid'", dataType)
	}
	if isNullable != "NO" {
		t.Errorf("user_sessions.family_id: is_nullable = %q, want 'NO' (must be NOT NULL)", isNullable)
	}
}
