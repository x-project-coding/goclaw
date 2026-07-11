package pg

import (
	"context"
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// argsContain reports whether want is present anywhere in args.
func argsContain(args []any, want any) bool {
	for _, a := range args {
		if a == want {
			return true
		}
	}
	return false
}

// TestBuildSessionFilter_ManagedByOnly verifies the ops-lead delegation filter
// emits a parameterized metadata->>'managedBy' equality and nothing else when it
// is the only opt set (bare context ⇒ no tenant clause).
func TestBuildSessionFilter_ManagedByOnly(t *testing.T) {
	where, args := buildSessionFilter(context.Background(), store.SessionListOpts{ManagedBy: "ops-lead-1"}, "")

	if !strings.Contains(where, "metadata->>'managedBy' = $1") {
		t.Fatalf("expected managedBy clause with $1 placeholder, got %q", where)
	}
	if len(args) != 1 || args[0] != "ops-lead-1" {
		t.Fatalf("expected single arg [ops-lead-1], got %v", args)
	}
	// The JSON key must be a hardcoded literal, never interpolated from input.
	if strings.Contains(where, "managed_by") {
		t.Fatalf("clause must query the camelCase metadata key 'managedBy', got %q", where)
	}
}

// TestBuildSessionFilter_ManagedByEmpty verifies an empty ManagedBy adds no clause.
func TestBuildSessionFilter_ManagedByEmpty(t *testing.T) {
	where, args := buildSessionFilter(context.Background(), store.SessionListOpts{}, "")
	if where != "" || args != nil {
		t.Fatalf("expected no filter for empty opts, got where=%q args=%v", where, args)
	}
	if strings.Contains(where, "metadata") {
		t.Fatalf("empty ManagedBy must not emit a metadata clause, got %q", where)
	}
}

// TestBuildSessionFilter_ManagedByComposesWithOtherFilters verifies the filter is
// purely additive (AND) and keeps stable positional numbering alongside AgentID
// and UserID. This is the scoping-composition invariant: managed_by narrows, never
// widens, the existing agent/user scope.
func TestBuildSessionFilter_ManagedByComposesWithOtherFilters(t *testing.T) {
	opts := store.SessionListOpts{
		AgentID:   "samantha",
		UserID:    "user-42",
		ManagedBy: "ops-lead-1",
	}
	where, args := buildSessionFilter(context.Background(), opts, "s")

	// All three conditions present, ANDed together, with alias prefix applied.
	for _, want := range []string{
		"s.session_key LIKE $1",
		"s.user_id = $2",
		"s.metadata->>'managedBy' = $3",
	} {
		if !strings.Contains(where, want) {
			t.Fatalf("expected %q in where clause, got %q", want, where)
		}
	}
	if !strings.HasPrefix(where, " WHERE ") || strings.Count(where, " AND ") != 2 {
		t.Fatalf("expected 3 AND-composed conditions, got %q", where)
	}
	if !argsContain(args, "user-42") || !argsContain(args, "ops-lead-1") {
		t.Fatalf("expected user_id and managedBy args present, got %v", args)
	}
}

// TestBuildSessionFilter_ManagedByInjectionSafe verifies a hostile ManagedBy value
// travels as a bound parameter, never concatenated into the SQL text.
func TestBuildSessionFilter_ManagedByInjectionSafe(t *testing.T) {
	evil := "x' OR '1'='1"
	where, args := buildSessionFilter(context.Background(), store.SessionListOpts{ManagedBy: evil}, "")

	if strings.Contains(where, evil) {
		t.Fatalf("hostile value leaked into SQL text: %q", where)
	}
	if !argsContain(args, evil) {
		t.Fatalf("hostile value must be passed as a bound arg, got %v", args)
	}
}
