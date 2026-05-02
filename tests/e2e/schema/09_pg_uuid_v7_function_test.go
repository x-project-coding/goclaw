//go:build e2e

package schema_test

import (
	"testing"

	"github.com/nextlevelbuilder/goclaw/tests/e2e/helpers"
)

// TestUUIDGenerateV7Exists confirms the custom uuid_generate_v7() SQL function
// is installed. This function produces time-ordered UUID v7 values which give
// B-tree locality on hot-write tables compared to random UUID v4.
func TestUUIDGenerateV7Exists(t *testing.T) {
	t.Parallel()
	helpers.MustLoadEnv()
	db := helpers.MustDB(t)

	var exists bool
	err := db.QueryRow(`
		SELECT EXISTS (
			SELECT 1 FROM pg_proc
			WHERE proname = 'uuid_generate_v7'
		)`).Scan(&exists)
	if err != nil {
		t.Fatalf("query pg_proc for uuid_generate_v7: %v", err)
	}
	if !exists {
		t.Error("uuid_generate_v7() SQL function not found — must be defined in the initial migration")
	}
}

// TestHotTablesUseV7Default asserts that the five highest-write tables default
// their id column to uuid_generate_v7(). Mixing v4 defaults on hot tables
// regresses insert latency due to random B-tree page splits.
func TestHotTablesUseV7Default(t *testing.T) {
	t.Parallel()
	helpers.MustLoadEnv()
	db := helpers.MustDB(t)

	hotTables := []string{
		"agent_sessions",
		"traces",
		"spans",
		"memory_documents",
		"memory_chunks",
	}

	for _, tbl := range hotTables {
		tbl := tbl
		t.Run(tbl, func(t *testing.T) {
			t.Parallel()
			var colDefault *string
			err := db.QueryRow(`
				SELECT column_default
				FROM information_schema.columns
				WHERE table_schema = 'public'
				  AND table_name   = $1
				  AND column_name  = 'id'`, tbl).Scan(&colDefault)
			if err != nil {
				t.Fatalf("%s.id: query column_default: %v", tbl, err)
			}
			if colDefault == nil {
				t.Errorf("%s.id: column_default is NULL — expected uuid_generate_v7()", tbl)
				return
			}
			if *colDefault != "uuid_generate_v7()" {
				t.Errorf("%s.id: column_default = %q, want 'uuid_generate_v7()'", tbl, *colDefault)
			}
		})
	}
}
