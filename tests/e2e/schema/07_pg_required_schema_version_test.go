//go:build e2e

package schema_test

import (
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/upgrade"
	"github.com/nextlevelbuilder/goclaw/tests/e2e/helpers"
)

// TestRequiredSchemaVersion asserts the constant in internal/upgrade/version.go
// equals 1 (v4 greenfield reset — not an increment from v3).
func TestRequiredSchemaVersion(t *testing.T) {
	t.Parallel()
	helpers.MustLoadEnv()

	const want uint = 1
	if upgrade.RequiredSchemaVersion != want {
		t.Errorf("RequiredSchemaVersion = %d, want %d", upgrade.RequiredSchemaVersion, want)
	}
}
