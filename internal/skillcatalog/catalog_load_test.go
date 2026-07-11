package skillcatalog

import (
	"encoding/json"
	"strings"
	"testing"
)

// snapshotOriginal captures the live catalog so a test that swaps it in can
// restore the floor afterwards (the snapshot is process-global).
func snapshotOriginal(t *testing.T) {
	t.Helper()
	origJSON, err := json.Marshal(Catalog())
	if err != nil {
		t.Fatalf("marshal original catalog: %v", err)
	}
	origVer := Version()
	t.Cleanup(func() {
		if err := Load(origJSON, origVer); err != nil {
			t.Fatalf("restore original catalog: %v", err)
		}
	})
}

func TestLoad_RejectsInvalidAndKeepsSnapshot(t *testing.T) {
	cases := []struct {
		name string
		in   string
	}{
		{"bad JSON", `{not json`},
		{"empty array", `[]`},
		{"missing ID", `[{"Skill":"deploy","Method":"POST","Path":"/deploy/x"}]`},
		{"missing Skill", `[{"ID":"deploy.x","Method":"POST","Path":"/deploy/x"}]`},
		{"missing Method", `[{"ID":"deploy.x","Skill":"deploy","Path":"/deploy/x"}]`},
		{"missing Path", `[{"ID":"deploy.x","Skill":"deploy","Method":"POST"}]`},
		{"duplicate IDs", `[
			{"ID":"deploy.x","Skill":"deploy","Method":"POST","Path":"/deploy/x"},
			{"ID":"deploy.x","Skill":"deploy","Method":"POST","Path":"/deploy/y"}
		]`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			beforeVer := Version()
			beforeLen := len(Catalog())
			if err := Load([]byte(c.in), "should-not-apply"); err == nil {
				t.Fatalf("Load(%s) = nil error, want validation error", c.name)
			}
			if Version() != beforeVer {
				t.Fatalf("Version changed after rejected Load: got %q, want %q", Version(), beforeVer)
			}
			if len(Catalog()) != beforeLen {
				t.Fatalf("catalog size changed after rejected Load: got %d, want %d", len(Catalog()), beforeLen)
			}
		})
	}
}

func TestLoad_SwapVisibleToAccessors(t *testing.T) {
	snapshotOriginal(t)

	// Derive a valid new catalog from the live one with a distinctive summary on
	// the first op, so a successful swap is observable through DescriptionFor.
	ops := append([]Operation(nil), Catalog()...)
	if len(ops) == 0 {
		t.Fatal("empty live catalog")
	}
	const marker = "SWAPPED-SUMMARY-MARKER"
	target := ops[0].ID
	ops[0].Summary = marker
	newJSON, err := json.Marshal(ops)
	if err != nil {
		t.Fatalf("marshal new catalog: %v", err)
	}

	if err := Load(newJSON, "v-swapped"); err != nil {
		t.Fatalf("Load valid catalog: %v", err)
	}
	if Version() != "v-swapped" {
		t.Fatalf("Version() = %q, want v-swapped", Version())
	}
	if op, ok := Lookup(target); !ok || op.Summary != marker {
		t.Fatalf("Lookup(%q) summary = %q, want %q", target, op.Summary, marker)
	}
	if !strings.Contains(Description(), marker) {
		t.Fatal("Description() does not reflect the swapped summary")
	}
	if got, want := len(OperationIDs()), len(ops); got != want {
		t.Fatalf("OperationIDs() = %d, want %d", got, want)
	}
}
