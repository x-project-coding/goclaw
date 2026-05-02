//go:build e2e

package helpers

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
)

// migrationsFileURI converts an (already-absolute or relative) path to the
// file:// URI format expected by golang-migrate's file source driver.
// Handles Windows drive letters safely on any OS.
func migrationsFileURI(dir string) string {
	abs, err := filepath.Abs(dir)
	if err != nil {
		abs = dir
	}
	abs = strings.ReplaceAll(filepath.ToSlash(abs), `\`, `/`)
	if len(abs) >= 2 && abs[1] == ':' {
		abs = "/" + abs
	}
	return "file://" + abs
}

// newMigrator builds a golang-migrate instance pointing at the e2e PG and
// the migrations directory. GOCLAW_MIGRATIONS_DIR is treated as relative to
// repo root (the directory containing go.mod), not the test's CWD.
func newMigrator(t *testing.T) *migrate.Migrate {
	t.Helper()
	MustLoadEnv()
	dsn := DatabaseURL()

	dir := MigrationsDir()
	if !filepath.IsAbs(dir) {
		root, err := findRepoRootFromCwd()
		if err != nil {
			t.Fatalf("e2e/migrate: locate repo root: %v", err)
		}
		dir = filepath.Join(root, dir)
	}

	m, err := migrate.New(migrationsFileURI(dir), dsn)
	if err != nil {
		t.Fatalf("e2e/migrate: create migrator: %v", err)
	}
	return m
}

// findRepoRootFromCwd walks up from CWD until go.mod is found.
func findRepoRootFromCwd() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	dir := cwd
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("go.mod not found walking up from " + cwd)
		}
		dir = parent
	}
}

// MigrateUp runs all pending migrations. Idempotent: already-applied steps are
// skipped. Calls t.Fatalf on errors other than migrate.ErrNoChange.
func MigrateUp(t *testing.T) {
	t.Helper()
	m := newMigrator(t)
	defer m.Close()
	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		t.Fatalf("e2e/migrate: up: %v", err)
	}
}

// MigrateDown runs exactly one step down. Used by round-trip tests.
// Calls t.Fatalf on errors other than migrate.ErrNoChange / ErrNilVersion.
func MigrateDown(t *testing.T) {
	t.Helper()
	m := newMigrator(t)
	defer m.Close()
	if err := m.Down(); err != nil && err != migrate.ErrNoChange {
		t.Fatalf("e2e/migrate: down: %v", err)
	}
}

// MigrateVersion returns the current applied version (0 = none applied).
func MigrateVersion(t *testing.T) uint {
	t.Helper()
	m := newMigrator(t)
	defer m.Close()
	v, dirty, err := m.Version()
	if err != nil && err != migrate.ErrNilVersion {
		t.Fatalf("e2e/migrate: version: %v", err)
	}
	if dirty {
		t.Fatalf("e2e/migrate: schema is in dirty state at version %d — manual intervention required", v)
	}
	return uint(v)
}

// MustMigrateClean drops all tables (by running Down to completion) and then
// applies Up. Used by round-trip tests that need a fresh schema state.
func MustMigrateClean(t *testing.T) {
	t.Helper()
	m := newMigrator(t)
	defer m.Close()

	// Down to nothing (ignore ErrNoChange / ErrNilVersion).
	if err := m.Down(); err != nil && err != migrate.ErrNoChange && err != migrate.ErrNilVersion {
		t.Logf("e2e/migrate: clean down warning: %v", err)
	}
	// Fresh up.
	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		t.Fatalf("e2e/migrate: clean up: %v", err)
	}
}
