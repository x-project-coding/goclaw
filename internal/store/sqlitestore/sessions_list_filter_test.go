//go:build sqlite || sqliteonly

package sqlitestore

import (
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// TestBuildSessionFilter_ManagedBy verifies the SQLite session filter emits a
// parameterized metadata->>'managedBy' equality for the ops-lead delegation
// filter (dual-DB parity with the PostgreSQL store).
func TestBuildSessionFilter_ManagedBy(t *testing.T) {
	where, args := buildSessionFilter(store.SessionListOpts{ManagedBy: "ops-lead-1"}, "")

	if !strings.Contains(where, "metadata->>'managedBy' = ?") {
		t.Fatalf("expected managedBy clause with ? placeholder, got %q", where)
	}
	if len(args) != 1 || args[0] != "ops-lead-1" {
		t.Fatalf("expected single arg [ops-lead-1], got %v", args)
	}
}

// TestBuildSessionFilter_ManagedByEmpty verifies an empty ManagedBy adds no clause.
func TestBuildSessionFilter_ManagedByEmpty(t *testing.T) {
	where, _ := buildSessionFilter(store.SessionListOpts{}, "")
	if strings.Contains(where, "metadata") {
		t.Fatalf("empty ManagedBy must not emit a metadata clause, got %q", where)
	}
}
