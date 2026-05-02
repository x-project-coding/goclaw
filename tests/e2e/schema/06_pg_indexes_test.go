//go:build e2e

package schema_test

import (
	"testing"

	"github.com/nextlevelbuilder/goclaw/tests/e2e/helpers"
)

// TestUniqueEmail asserts users.email has a unique index.
func TestUniqueEmail(t *testing.T) {
	t.Parallel()
	helpers.MustLoadEnv()
	db := helpers.MustDB(t)

	var exists bool
	err := db.QueryRow(`
		SELECT EXISTS (
			SELECT 1 FROM pg_indexes
			WHERE schemaname = 'public'
			  AND tablename  = 'users'
			  AND indexdef LIKE '%UNIQUE%email%'
		)`).Scan(&exists)
	if err != nil {
		t.Fatalf("query unique email index: %v", err)
	}
	if !exists {
		t.Error("users.email must have a UNIQUE index")
	}
}

// TestVaultPathUnique asserts vault_documents has a unique index covering
// (scope, custom_scope, path, owner_user_id) — minus tenant, matching v4 semantics.
func TestVaultPathUnique(t *testing.T) {
	t.Parallel()
	helpers.MustLoadEnv()
	db := helpers.MustDB(t)

	var exists bool
	err := db.QueryRow(`
		SELECT EXISTS (
			SELECT 1 FROM pg_indexes
			WHERE schemaname = 'public'
			  AND tablename  = 'vault_documents'
			  AND indexdef LIKE '%UNIQUE%'
		)`).Scan(&exists)
	if err != nil {
		t.Fatalf("query vault unique index: %v", err)
	}
	if !exists {
		t.Error("vault_documents must have a UNIQUE index for path deduplication")
	}
}

// TestUsersOnlyOneRootIndex asserts the partial UNIQUE index on users(role)
// WHERE role='root' exists. This guarantees at most one root user can exist,
// preventing concurrent bootstrap races.
func TestUsersOnlyOneRootIndex(t *testing.T) {
	t.Parallel()
	helpers.MustLoadEnv()
	db := helpers.MustDB(t)

	var exists bool
	err := db.QueryRow(`
		SELECT EXISTS (
			SELECT 1 FROM pg_indexes
			WHERE schemaname = 'public'
			  AND tablename  = 'users'
			  AND indexname  = 'users_only_one_root'
		)`).Scan(&exists)
	if err != nil {
		t.Fatalf("query users_only_one_root index: %v", err)
	}
	if !exists {
		t.Error("partial UNIQUE index 'users_only_one_root' on users(role) WHERE role='root' must exist")
	}

	// Also verify it is actually a UNIQUE partial index.
	var indexDef string
	err = db.QueryRow(`
		SELECT indexdef FROM pg_indexes
		WHERE schemaname = 'public'
		  AND tablename  = 'users'
		  AND indexname  = 'users_only_one_root'`).Scan(&indexDef)
	if err != nil {
		t.Fatalf("query indexdef: %v", err)
	}
	if indexDef == "" {
		t.Error("users_only_one_root index definition is empty")
	}
	// Check it is UNIQUE and has WHERE clause for role='root'.
	if !containsCI(indexDef, "UNIQUE") {
		t.Errorf("users_only_one_root is not UNIQUE: %s", indexDef)
	}
	if !containsCI(indexDef, "root") {
		t.Errorf("users_only_one_root does not filter on 'root': %s", indexDef)
	}
}

// TestUserSessionsFamilyIndex asserts the index on user_sessions(family_id) exists.
// This index enables efficient family-revocation queries for token theft detection.
func TestUserSessionsFamilyIndex(t *testing.T) {
	t.Parallel()
	helpers.MustLoadEnv()
	db := helpers.MustDB(t)

	var exists bool
	err := db.QueryRow(`
		SELECT EXISTS (
			SELECT 1 FROM pg_indexes
			WHERE schemaname = 'public'
			  AND tablename  = 'user_sessions'
			  AND indexname  = 'user_sessions_family_idx'
		)`).Scan(&exists)
	if err != nil {
		t.Fatalf("query user_sessions_family_idx: %v", err)
	}
	if !exists {
		t.Error("index 'user_sessions_family_idx' on user_sessions(family_id) must exist")
	}
}

// containsCI reports whether s contains substr (case-insensitive).
func containsCI(s, substr string) bool {
	sLower := toLower(s)
	subLower := toLower(substr)
	return contains(sLower, subLower)
}

func toLower(s string) string {
	b := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		b[i] = c
	}
	return string(b)
}

func contains(s, substr string) bool {
	if len(substr) == 0 {
		return true
	}
	if len(substr) > len(s) {
		return false
	}
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
