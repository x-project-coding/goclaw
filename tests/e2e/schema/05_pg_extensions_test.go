//go:build e2e

package schema_test

import (
	"testing"

	"github.com/nextlevelbuilder/goclaw/tests/e2e/helpers"
)

// TestExtensions asserts that pgcrypto and vector extensions are enabled.
// pgcrypto is required for the uuid_generate_v7() SQL function (uses gen_random_bytes).
// vector is required for memory + knowledge graph embeddings.
func TestExtensions(t *testing.T) {
	t.Parallel()
	helpers.MustLoadEnv()
	db := helpers.MustDB(t)

	required := []string{"pgcrypto", "vector"}

	for _, ext := range required {
		ext := ext
		t.Run(ext, func(t *testing.T) {
			t.Parallel()
			var exists bool
			err := db.QueryRow(`
				SELECT EXISTS (
					SELECT 1 FROM pg_extension WHERE extname = $1)`,
				ext).Scan(&exists)
			if err != nil {
				t.Fatalf("query extension %s: %v", ext, err)
			}
			if !exists {
				t.Errorf("extension %q not installed — required by v4 schema", ext)
			}
		})
	}
}
