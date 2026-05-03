//go:build e2e

package e2e_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPurgedSymbols asserts that specific identifiers retired during the v4
// EPIC-04 cleanup are gone from production code. We check by reading the
// source files where those identifiers used to live — a cheaper, more
// targeted check than a project-wide grep, and one that surfaces a clear
// "this file should not contain X" signal in CI logs.
func TestPurgedSymbols(t *testing.T) {
	repoRoot := mustRepoRoot(t)

	cases := []struct {
		path string
		// any of these substrings present in the file fails the test
		forbidden []string
	}{
		{
			path: filepath.Join("internal", "store", "context.go"),
			forbidden: []string{
				"MasterTenantID",
				"uuid.MustParse(\"0193a5b0-7000-7000-8000-000000000001\")",
			},
		},
		{
			path: filepath.Join("pkg", "browser", "browser.go"),
			forbidden: []string{
				"MasterTenantID",
				`"0193a5b0-7000-7000-8000-000000000001"`,
			},
		},
	}

	for _, tc := range cases {
		full := filepath.Join(repoRoot, tc.path)
		raw, err := os.ReadFile(full)
		if err != nil {
			t.Errorf("read %s: %v", tc.path, err)
			continue
		}
		body := string(raw)
		for _, needle := range tc.forbidden {
			if strings.Contains(body, needle) {
				t.Errorf("%s still contains %q — should be purged in Phase 13", tc.path, needle)
			}
		}
	}
}

// TestDeletedFiles asserts that legacy files removed during Phase 13
// are not silently re-created by future merges.
func TestDeletedFiles(t *testing.T) {
	repoRoot := mustRepoRoot(t)

	deleted := []string{
		filepath.Join("internal", "http", "tenant_scope_hotfix_test.go"),
		filepath.Join("tests", "invariants", "tenant_isolation_test.go"),
		filepath.Join("internal", "gateway", "methods", "api_keys_tenant_guard_test.go"),
	}

	for _, rel := range deleted {
		if _, err := os.Stat(filepath.Join(repoRoot, rel)); err == nil {
			t.Errorf("file %s should remain deleted (Phase 13)", rel)
		}
	}
}
