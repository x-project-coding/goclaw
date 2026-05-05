package workspace

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// TestResolve_Personal verifies the v4 single personal-scope shape: every
// personal agent shares its directory at agent level. The legacy open-agent
// per-user / group-chat segmenting was removed when AgentType went away.
func TestResolve_Personal(t *testing.T) {
	base := t.TempDir()
	r := NewResolver()
	wc, err := r.Resolve(context.Background(), ResolveParams{
		AgentID:  "agent-123",
		UserID:   "user-456",
		PeerKind: "direct",
		BaseDir:  base,
	})
	if err != nil {
		t.Fatal(err)
	}

	want := filepath.Join(base, "agent-123")
	if wc.ActivePath != want {
		t.Errorf("ActivePath = %q, want %q", wc.ActivePath, want)
	}
	if wc.Scope != ScopePersonal {
		t.Errorf("Scope = %q, want personal", wc.Scope)
	}
	if wc.OwnerID != "user-456" {
		t.Errorf("OwnerID = %q", wc.OwnerID)
	}
	if wc.MemoryScope != "user" {
		t.Errorf("MemoryScope = %q, want user", wc.MemoryScope)
	}
	assertDirExists(t, wc.ActivePath)
}

func TestResolve_PredefinedShared(t *testing.T) {
	base := t.TempDir()
	r := NewResolver()
	wc, err := r.Resolve(context.Background(), ResolveParams{
		AgentID:    "agent-pre",
		UserID:     "user-1",
		PeerKind:   "direct",
		BaseDir:    base,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Predefined = shared, no user subdir
	want := filepath.Join(base, "agent-pre")
	if wc.ActivePath != want {
		t.Errorf("ActivePath = %q, want %q", wc.ActivePath, want)
	}
}

func TestResolve_TeamShared(t *testing.T) {
	base := t.TempDir()
	r := NewResolver()
	teamID := "team-abc"
	wc, err := r.Resolve(context.Background(), ResolveParams{
		AgentID:    "agent-1",
		UserID:     "user-1",
		ChatID:     "chat-1",
		PeerKind:   "direct",
		TeamID:     &teamID,
		TeamConfig: &TeamWorkspaceConfig{WorkspaceScope: "shared"},
		BaseDir:    base,
	})
	if err != nil {
		t.Fatal(err)
	}

	want := filepath.Join(base, "teams", "team-abc")
	if wc.ActivePath != want {
		t.Errorf("ActivePath = %q, want %q", wc.ActivePath, want)
	}
	if wc.Scope != ScopeTeam {
		t.Errorf("Scope = %q, want team", wc.Scope)
	}
	if wc.TeamPath == nil || *wc.TeamPath != want {
		t.Errorf("TeamPath = %v, want %q", wc.TeamPath, want)
	}
	if wc.MemoryScope != "shared" {
		t.Errorf("MemoryScope = %q, want shared", wc.MemoryScope)
	}
}

func TestResolve_TeamIsolated(t *testing.T) {
	base := t.TempDir()
	r := NewResolver()
	teamID := "team-abc"
	wc, err := r.Resolve(context.Background(), ResolveParams{
		AgentID:    "agent-1",
		UserID:     "user-1",
		ChatID:     "chat-1",
		PeerKind:   "direct",
		TeamID:     &teamID,
		TeamConfig: &TeamWorkspaceConfig{WorkspaceScope: "isolated"},
		BaseDir:    base,
	})
	if err != nil {
		t.Fatal(err)
	}

	teamRoot := filepath.Join(base, "teams", "team-abc")
	want := filepath.Join(teamRoot, "chat-1")
	if wc.ActivePath != want {
		t.Errorf("ActivePath = %q, want %q", wc.ActivePath, want)
	}
	if wc.TeamPath == nil || *wc.TeamPath != teamRoot {
		t.Errorf("TeamPath = %v, want %q", wc.TeamPath, teamRoot)
	}
	if wc.MemoryScope != "user" {
		t.Errorf("MemoryScope = %q, want user", wc.MemoryScope)
	}
}

func TestResolve_Delegation(t *testing.T) {
	base := t.TempDir()
	sharedPath := filepath.Join(base, "shared-task")
	exportPath := filepath.Join(base, "exports")

	r := NewResolver()
	wc, err := r.Resolve(context.Background(), ResolveParams{
		AgentID:   "agent-1",
		UserID:    "user-1",
		BaseDir:   base,
		DelegateCtx: &DelegateContext{
			LinkID:      "link-1",
			SharedPath:  sharedPath,
			ExportPaths: []string{exportPath},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	if wc.ActivePath != sharedPath {
		t.Errorf("ActivePath = %q, want %q", wc.ActivePath, sharedPath)
	}
	if wc.Scope != ScopeDelegate {
		t.Errorf("Scope = %q, want delegate", wc.Scope)
	}
	if len(wc.ReadOnlyPaths) != 1 || wc.ReadOnlyPaths[0] != exportPath {
		t.Errorf("ReadOnlyPaths = %v", wc.ReadOnlyPaths)
	}
	assertDirExists(t, wc.ActivePath)
}

func TestResolve_DelegationEscapesBaseDir(t *testing.T) {
	base := t.TempDir()
	r := NewResolver()
	_, err := r.Resolve(context.Background(), ResolveParams{
		AgentID: "agent-1",
		UserID:  "user-1",
		BaseDir: base,
		DelegateCtx: &DelegateContext{
			SharedPath: "/etc/shadow",
		},
	})
	if err == nil {
		t.Error("expected error for delegate path escaping base dir")
	}
}

func TestResolve_EnforcementLabel(t *testing.T) {
	tests := []struct {
		name   string
		scope  Scope
		shared bool
		substr string
	}{
		{"personal", ScopePersonal, false, "personal workspace"},
		{"team_shared", ScopeTeam, true, "shared team workspace"},
		{"team_isolated", ScopeTeam, false, "isolated team workspace"},
		{"delegate", ScopeDelegate, false, "delegated task"},
		{"project", ScopeProject, false, "project workspace"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			label := DefaultEnforcementLabel(tt.scope, tt.shared)
			if !strings.Contains(label, tt.substr) {
				t.Errorf("label = %q, missing %q", label, tt.substr)
			}
		})
	}
}

func TestResolve_EmptyBaseDir(t *testing.T) {
	r := NewResolver()
	_, err := r.Resolve(context.Background(), ResolveParams{
		AgentID: "agent-1",
		UserID:  "user-1",
	})
	if err == nil {
		t.Error("expected error for empty BaseDir")
	}
}

func TestResolve_DefaultUser(t *testing.T) {
	base := t.TempDir()
	r := NewResolver()
	wc, err := r.Resolve(context.Background(), ResolveParams{
		AgentID:  "agent-1",
		UserID:   "user-1",
		PeerKind: "direct",
		BaseDir:  base,
	})
	if err != nil {
		t.Fatal(err)
	}

	// v4 personal scope shares the agent directory across users; no per-user segment.
	want := filepath.Join(base, "agent-1")
	if wc.ActivePath != want {
		t.Errorf("ActivePath = %q, want %q", wc.ActivePath, want)
	}
}

// TestResolve_ProjectPriority verifies that when ProjectID + ProjectSlug are set
// the workspace resolver routes the session to the project workspace path and
// returns ScopeProject — not the personal or team branch.
func TestResolve_ProjectPriority(t *testing.T) {
	base := t.TempDir()
	// Override workspace root so ProjectWorkspacePath resolves under base.
	t.Setenv("GOCLAW_WORKSPACE_ROOT", base)

	projectID := uuid.MustParse("01900000-0000-7000-8000-000000000001")
	slug := "my-project"

	r := NewResolver()
	wc, err := r.Resolve(context.Background(), ResolveParams{
		AgentID:     "agent-1",
		UserID:      "user-1",
		PeerKind:    "direct",
		BaseDir:     base,
		ProjectID:   &projectID,
		ProjectSlug: slug,
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if wc.Scope != ScopeProject {
		t.Errorf("Scope = %q, want project", wc.Scope)
	}
	if wc.ProjectID == nil || *wc.ProjectID != projectID {
		t.Errorf("ProjectID = %v, want %v", wc.ProjectID, projectID)
	}
	if wc.ProjectSlug != slug {
		t.Errorf("ProjectSlug = %q, want %q", wc.ProjectSlug, slug)
	}
	// Active path must be under <base>/projects/<slug>
	want := filepath.Join(base, "projects", slug)
	if wc.ActivePath != want {
		t.Errorf("ActivePath = %q, want %q", wc.ActivePath, want)
	}
	assertDirExists(t, wc.ActivePath)
}

func TestResolve_SingleTenantPath(t *testing.T) {
	base := t.TempDir()
	r := NewResolver()
	wc, err := r.Resolve(context.Background(), ResolveParams{
		AgentID:  "agent-1",
		UserID:   "user-1",
		PeerKind: "direct",
		BaseDir:  base,
	})
	if err != nil {
		t.Fatal(err)
	}

	want := filepath.Join(base, "agent-1")
	if wc.ActivePath != want {
		t.Errorf("ActivePath = %q, want %q", wc.ActivePath, want)
	}
}

func assertDirExists(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Errorf("directory %q does not exist: %v", path, err)
	} else if !info.IsDir() {
		t.Errorf("%q exists but is not a directory", path)
	}
}
