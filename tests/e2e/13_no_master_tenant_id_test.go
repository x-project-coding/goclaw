//go:build e2e

package e2e_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestNoMasterTenantIDProjectWide asserts the v3 leftover identifier
// `MasterTenantID` is gone from all production Go code. Test files are allowed
// to mention the literal in fixture comments only — no symbol references.
//
// Phase 13 of the v4 EPIC-04 cleanup; see plans/260502-1635-v4-epic-04-schema/.
func TestNoMasterTenantIDProjectWide(t *testing.T) {
	repoRoot := mustRepoRoot(t)

	// Production code: zero tolerance.
	out, _ := runGrep(t, repoRoot,
		"-rln", "MasterTenantID", "--include=*.go",
		"--exclude-dir=node_modules", "--exclude-dir=.git")
	var prod []string
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line == "" {
			continue
		}
		if strings.HasSuffix(line, "_test.go") {
			continue
		}
		prod = append(prod, line)
	}
	if len(prod) != 0 {
		t.Fatalf("MasterTenantID still referenced in %d non-test files:\n  %s",
			len(prod), strings.Join(prod, "\n  "))
	}

	// Test code: at most a string-literal mention, never a symbol reference.
	// We allow the substring to appear in test names / fixture comments,
	// so this leg is a soft check that fails only if the count regresses
	// suspiciously high.
	out, _ = runGrep(t, repoRoot,
		"-rln", "store.MasterTenantID", "--include=*_test.go",
		"--exclude-dir=node_modules", "--exclude-dir=.git")
	if strings.TrimSpace(out) != "" {
		t.Fatalf("store.MasterTenantID symbol still referenced in tests:\n%s", out)
	}
}

// runGrep invokes grep on the repo root and returns stdout. A non-zero exit
// from grep is fine (it returns 1 when nothing matched) — caller checks the
// returned text instead.
func runGrep(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command("grep", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	return string(out), err
}

// mustRepoRoot resolves the repo root by walking up from this test file
// until we find go.mod. Tests run with a working dir of tests/e2e, so the
// walk is short.
func mustRepoRoot(t *testing.T) string {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for dir := cwd; dir != "/" && dir != ""; dir = filepath.Dir(dir) {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
	}
	t.Fatalf("could not find go.mod walking up from %s", cwd)
	return ""
}
