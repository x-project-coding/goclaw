package workspace

import (
	"context"
	"path/filepath"
	"testing"
)

// In v4 the personal workspace is always shared at the agent directory.
// resolvePersonal must no longer branch on AgentType — the field is gone
// from ResolveParams. ActivePath collapses to <base>/<agent>; per-user
// chat segments under personal scope are removed.
//
// RED until Phase 04 lands.
func TestResolve_PersonalAlwaysShared(t *testing.T) {
	base := t.TempDir()
	r := NewResolver()
	wc, err := r.Resolve(context.Background(), ResolveParams{
		AgentID:  "agent-123",
		UserID:   "user-456",
		ChatID:   "chat-789",
		PeerKind: "direct",
		BaseDir:  base,
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	want := filepath.Join(base, "agent-123")
	if wc.ActivePath != want {
		t.Errorf("ActivePath = %q, want %q (no per-user segment)", wc.ActivePath, want)
	}
	if wc.Scope != ScopePersonal {
		t.Errorf("Scope = %q, want personal", wc.Scope)
	}
}

// Even with a group PeerKind (the old "open + group chat" branch), personal
// scope must collapse to the agent dir.
func TestResolve_PersonalGroupNoChatSegment(t *testing.T) {
	base := t.TempDir()
	r := NewResolver()
	wc, err := r.Resolve(context.Background(), ResolveParams{
		AgentID:  "agent-123",
		UserID:   "user-456",
		ChatID:   "chat-789",
		PeerKind: "group",
		BaseDir:  base,
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	want := filepath.Join(base, "agent-123")
	if wc.ActivePath != want {
		t.Errorf("ActivePath = %q, want %q (group must not append chat segment)", wc.ActivePath, want)
	}
}

// EnforcementLabel for personal scope is always the shared label.
func TestResolve_PersonalEnforcementLabelShared(t *testing.T) {
	base := t.TempDir()
	r := NewResolver()
	wc, err := r.Resolve(context.Background(), ResolveParams{
		AgentID: "agent-1",
		UserID:  "user-1",
		BaseDir: base,
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	want := DefaultEnforcementLabel(ScopePersonal, true)
	if wc.EnforcementLabel != want {
		t.Errorf("EnforcementLabel = %q, want %q", wc.EnforcementLabel, want)
	}
}
