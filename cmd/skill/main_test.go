package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/skillcatalog"
)

// restoreCatalog snapshots the process-global catalog and restores it after the
// test, since loadCatalogOverride mutates it via skillcatalog.Load.
func restoreCatalog(t *testing.T) {
	t.Helper()
	origJSON, err := json.Marshal(skillcatalog.Catalog())
	if err != nil {
		t.Fatalf("marshal original catalog: %v", err)
	}
	origVer := skillcatalog.Version()
	t.Cleanup(func() {
		if err := skillcatalog.Load(origJSON, origVer); err != nil {
			t.Fatalf("restore catalog: %v", err)
		}
	})
}

// writeMarkedCatalog writes a valid catalog (derived from the live one) with a
// marker summary on the first op to path and returns the target op id.
func writeMarkedCatalog(t *testing.T, path, marker string) string {
	t.Helper()
	ops := append([]skillcatalog.Operation(nil), skillcatalog.Catalog()...)
	if len(ops) == 0 {
		t.Fatal("empty live catalog")
	}
	ops[0].Summary = marker
	b, err := json.Marshal(ops)
	if err != nil {
		t.Fatalf("marshal ops: %v", err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatalf("write catalog file: %v", err)
	}
	return ops[0].ID
}

func TestLoadCatalogOverride_EnvPathWins(t *testing.T) {
	restoreCatalog(t)
	const marker = "CLI-OVERRIDE-MARKER"
	file := filepath.Join(t.TempDir(), "override.json")
	target := writeMarkedCatalog(t, file, marker)

	t.Setenv("GOCLAW_SKILL_CATALOG", file)
	loadCatalogOverride()

	if op, ok := skillcatalog.Lookup(target); !ok || op.Summary != marker {
		t.Fatalf("override not applied: Lookup(%q) summary = %q, want %q", target, op.Summary, marker)
	}
	if !strings.Contains(skillcatalog.Description(), marker) {
		t.Fatal("override not visible via Description()")
	}
}

func TestLoadCatalogOverride_MissingFileFallsBack(t *testing.T) {
	restoreCatalog(t)
	beforeVer := skillcatalog.Version()
	beforeLen := len(skillcatalog.Catalog())

	t.Setenv("GOCLAW_SKILL_CATALOG", filepath.Join(t.TempDir(), "does-not-exist.json"))
	loadCatalogOverride() // must not panic

	if skillcatalog.Version() != beforeVer || len(skillcatalog.Catalog()) != beforeLen {
		t.Fatalf("missing file changed the catalog (ver %q→%q, len %d→%d)",
			beforeVer, skillcatalog.Version(), beforeLen, len(skillcatalog.Catalog()))
	}
}

func TestLoadCatalogOverride_InvalidFileFallsBack(t *testing.T) {
	restoreCatalog(t)
	beforeVer := skillcatalog.Version()
	beforeLen := len(skillcatalog.Catalog())

	bad := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(bad, []byte("{not a catalog"), 0o644); err != nil {
		t.Fatalf("write bad file: %v", err)
	}
	t.Setenv("GOCLAW_SKILL_CATALOG", bad)
	loadCatalogOverride() // silent fallback, no panic

	if skillcatalog.Version() != beforeVer || len(skillcatalog.Catalog()) != beforeLen {
		t.Fatalf("invalid file changed the catalog (ver %q→%q, len %d→%d)",
			beforeVer, skillcatalog.Version(), beforeLen, len(skillcatalog.Catalog()))
	}
}
