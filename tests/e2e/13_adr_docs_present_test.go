//go:build e2e

package e2e_test

import (
	"os"
	"path/filepath"
	"testing"
)

// TestADRsExist asserts that the four v4 EPIC-04 ADRs landed during Phase 13
// (LOG-2, LOG-3, audit MISS-2, audit MISS-3) are present and non-empty.
// Each is a load-bearing record that future contributors must be able to
// look up before re-litigating a settled decision.
func TestADRsExist(t *testing.T) {
	repoRoot := mustRepoRoot(t)
	adrDir := filepath.Join(repoRoot, "docs", "adr")

	required := []string{
		"2026-05-v4-vault-custom-scope-reserved.md",
		"2026-05-v4-vault-no-encryption-defer.md",
		"2026-05-v4-sessions-naming-divergence.md",
		"2026-05-v4-activity-logs-retention-defer.md",
	}

	for _, name := range required {
		path := filepath.Join(adrDir, name)
		info, err := os.Stat(path)
		if err != nil {
			t.Errorf("ADR missing: %s (%v)", name, err)
			continue
		}
		if info.Size() < 200 {
			t.Errorf("ADR %s is suspiciously short (%d bytes) — likely a stub", name, info.Size())
		}
	}
}
