//go:build integration

// Package integration — shared helpers for foundation rollup tests.
package integration

import (
	"os"
	"path/filepath"
	"testing"
)

// repoRoot walks up from the process CWD until it finds a directory containing
// go.mod, which is the repository root. Returns the root path or fails the test.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("repoRoot: getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("repoRoot: go.mod not found walking up from %s", dir)
		}
		dir = parent
	}
}
