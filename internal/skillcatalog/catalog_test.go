package skillcatalog

import (
	"strings"
	"testing"
)

func TestOperationIDsFor_NilMeansAll(t *testing.T) {
	all := OperationIDsFor(nil)
	if len(all) != len(Catalog()) {
		t.Fatalf("nil allowed: got %d ids, want %d (full catalog)", len(all), len(Catalog()))
	}
	if got := OperationIDs(); len(got) != len(all) {
		t.Fatalf("OperationIDs() = %d ids, want %d", len(got), len(all))
	}
}

func TestOperationIDsFor_FiltersBySkill(t *testing.T) {
	allowed := map[string]bool{"manage-skills": true, "deploy": true}
	ids := OperationIDsFor(allowed)
	if len(ids) == 0 {
		t.Fatal("expected some ids for manage-skills+deploy")
	}
	for _, id := range ids {
		op, ok := Lookup(id)
		if !ok {
			t.Fatalf("filtered id %q not in catalog", id)
		}
		if !allowed[op.Skill] {
			t.Fatalf("id %q belongs to skill %q, not in allowed set", id, op.Skill)
		}
	}
	// manage-skills has 5 ops + deploy has 1 in the Phase 1 catalog.
	want := 0
	for _, op := range Catalog() {
		if allowed[op.Skill] {
			want++
		}
	}
	if len(ids) != want {
		t.Fatalf("got %d ids, want %d", len(ids), want)
	}
}

func TestOperationIDsFor_EmptyMapGatesEverything(t *testing.T) {
	if ids := OperationIDsFor(map[string]bool{}); len(ids) != 0 {
		t.Fatalf("empty allowed map should gate all ops, got %v", ids)
	}
	if ids := OperationIDsFor(map[string]bool{"no-such-skill": true}); len(ids) != 0 {
		t.Fatalf("unknown skill should gate all ops, got %v", ids)
	}
}

func TestDescriptionFor_MatchesFiltering(t *testing.T) {
	if Description() != DescriptionFor(nil) {
		t.Fatal("Description() must equal DescriptionFor(nil)")
	}
	allowed := map[string]bool{"research": true}
	desc := DescriptionFor(allowed)
	if !strings.Contains(desc, "research.search") {
		t.Fatal("filtered description missing research.search")
	}
	if strings.Contains(desc, "manage-view.set") {
		t.Fatal("filtered description leaked a gated operation")
	}
}
