//go:build e2e

// Package cli_test exercises the goclaw CLI surface end-to-end.
// Builds the binary once per package run and exec's it for each test.
package cli_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// goclawBin is the path to the built CLI binary, valid for the package run.
var goclawBin string

// TestMain builds the binary once before any test runs.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "goclaw-cli-e2e-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "tempdir:", err)
		os.Exit(2)
	}
	defer os.RemoveAll(dir)

	bin := filepath.Join(dir, "goclaw")
	repoRoot, err := findRepoRoot()
	if err != nil {
		fmt.Fprintln(os.Stderr, "find repo root:", err)
		os.Exit(2)
	}
	cmd := exec.Command("go", "build", "-o", bin, ".")
	cmd.Dir = repoRoot
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "build goclaw:", err)
		os.Exit(2)
	}
	goclawBin = bin
	os.Exit(m.Run())
}

// findRepoRoot walks upward from CWD until go.mod is found.
func findRepoRoot() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for d := cwd; d != filepath.Dir(d); d = filepath.Dir(d) {
		if _, err := os.Stat(filepath.Join(d, "go.mod")); err == nil {
			return d, nil
		}
	}
	return "", fmt.Errorf("go.mod not found above %s", cwd)
}
