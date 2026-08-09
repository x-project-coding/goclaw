package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// tenantScopeFixture builds a realistic multi-tenant workspace tree on disk:
//
//	<root>/                                  (global workspace root, t.workspace)
//	  tenants/
//	    own/agentA/userA/file.txt            (caller's own workspace = cwd)
//	    own/agentA/userA/sub/nested.txt
//	    other/agentB/userB/secret.txt        (sibling tenant — must stay hidden)
//	    other/.git/objects/blob              (leaked in prod repro #2)
//
// Real directories are used so EvalSymlinks resolves exactly and symlink-escape
// cases are exercised faithfully.
type tenantScopeFixture struct {
	root    string
	ownWS   string
	otherWS string
}

func newTenantScopeFixture(t *testing.T) tenantScopeFixture {
	t.Helper()
	// Resolve symlinks in the temp root (e.g. macOS /var → /private/var) so the
	// fixture paths match what canonicalSandboxWorkspace produces at runtime.
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("evalsymlinks temp root: %v", err)
	}
	own := filepath.Join(root, "tenants", "own", "agentA", "userA")
	other := filepath.Join(root, "tenants", "other", "agentB", "userB")
	mustMkdir(t, filepath.Join(own, "sub"))
	mustMkdir(t, other)
	mustMkdir(t, filepath.Join(root, "tenants", "other", ".git", "objects"))
	mustWrite(t, filepath.Join(own, "file.txt"), "own")
	mustWrite(t, filepath.Join(own, "sub", "nested.txt"), "own-nested")
	mustWrite(t, filepath.Join(other, "secret.txt"), "sibling-secret")
	mustWrite(t, filepath.Join(root, "tenants", "other", ".git", "objects", "blob"), "gitblob")
	return tenantScopeFixture{root: root, ownWS: own, otherWS: other}
}

func mustMkdir(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", p, err)
	}
}

func mustWrite(t *testing.T, p, content string) {
	t.Helper()
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", p, err)
	}
}

// tenantCtx returns a non-master, tenant-scoped context whose tool workspace is
// the caller's own tenant subtree.
func tenantCtx(ws string) context.Context {
	ctx := store.WithTenantID(context.Background(), uuid.New())
	return WithToolWorkspace(ctx, ws)
}

func TestEnforceTenantPathScope(t *testing.T) {
	fx := newTenantScopeFixture(t)
	tool := &ExecTool{workspace: fx.root}
	ctx := tenantCtx(fx.ownWS)

	cases := []struct {
		name       string
		command    string
		wantDenied bool
	}{
		{
			name:       "own tenant absolute path allowed",
			command:    "cat " + filepath.Join(fx.ownWS, "file.txt"),
			wantDenied: false,
		},
		{
			name:       "own tenant nested absolute path allowed",
			command:    "cat " + filepath.Join(fx.ownWS, "sub", "nested.txt"),
			wantDenied: false,
		},
		{
			name:       "relative in-tenant path allowed",
			command:    "cat sub/nested.txt",
			wantDenied: false,
		},
		{
			name:       "relative dotdot staying in own tenant allowed",
			command:    "cat ../userA/file.txt",
			wantDenied: false,
		},
		{
			name:       "tmp absolute path allowed",
			command:    "cat /tmp/anything.txt",
			wantDenied: false,
		},
		{
			name:       "sibling tenant absolute path rejected",
			command:    "cat " + filepath.Join(fx.otherWS, "secret.txt"),
			wantDenied: true,
		},
		{
			name:       "sibling tenant git objects rejected",
			command:    "cat " + filepath.Join(fx.root, "tenants", "other", ".git", "objects", "blob"),
			wantDenied: true,
		},
		{
			// The exact production repro shape: find rooted at the workspace
			// root walks into every sibling tenant.
			name:       "find at workspace root rejected",
			command:    "find " + fx.root + " -name index.html -path *mario*",
			wantDenied: true,
		},
		{
			name:       "listing tenants index rejected",
			command:    "ls " + filepath.Join(fx.root, "tenants"),
			wantDenied: true,
		},
		{
			name:       "find at filesystem root rejected",
			command:    "find / -name secret.txt",
			wantDenied: true,
		},
		{
			// tenants/*/... glob: literal resolves under tenants base but not
			// own subtree.
			name:       "sibling glob under tenants rejected",
			command:    "cat " + filepath.Join(fx.root, "tenants", "*", "agentB", "userB", "secret.txt"),
			wantDenied: true,
		},
		{
			// ../ escape out of own tenant into the sibling.
			name:       "relative dotdot escape into sibling rejected",
			command:    "cat ../../../other/agentB/userB/secret.txt",
			wantDenied: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := tool.enforceTenantPathScope(ctx, tc.command, fx.ownWS)
			denied := res != nil
			if denied != tc.wantDenied {
				t.Fatalf("command %q: denied=%v, want %v (result=%+v)", tc.command, denied, tc.wantDenied, res)
			}
			if denied {
				if !res.IsError {
					t.Fatalf("denied result must be an error result")
				}
				if !strings.Contains(res.ForLLM, "cross-tenant isolation policy") {
					t.Fatalf("deny message should name the policy, got: %q", res.ForLLM)
				}
			}
		})
	}
}

// TestEnforceTenantPathScopeSymlinkEscape verifies a symlink inside the caller's
// own tenant that points at a sibling tenant is rejected after resolution.
func TestEnforceTenantPathScopeSymlinkEscape(t *testing.T) {
	fx := newTenantScopeFixture(t)
	link := filepath.Join(fx.ownWS, "escape")
	if err := os.Symlink(filepath.Join(fx.root, "tenants", "other"), link); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	tool := &ExecTool{workspace: fx.root}
	ctx := tenantCtx(fx.ownWS)

	// Both absolute and relative addressing of the escaping symlink must be denied.
	for _, cmd := range []string{
		"cat " + filepath.Join(link, "agentB", "userB", "secret.txt"),
		"cat escape/agentB/userB/secret.txt",
	} {
		if res := tool.enforceTenantPathScope(ctx, cmd, fx.ownWS); res == nil {
			t.Fatalf("symlink escape not denied for %q", cmd)
		}
	}
}

// TestEnforceTenantPathScopeMasterExempt verifies a caller whose workspace is
// not under the tenants/ tree (master / super-admin / global-root scope) is not
// constrained — it may reference any path.
func TestEnforceTenantPathScopeMasterExempt(t *testing.T) {
	fx := newTenantScopeFixture(t)
	tool := &ExecTool{workspace: fx.root}
	// Master workspace layout: <root>/<agent>/<user>, no tenants/ prefix.
	masterWS := filepath.Join(fx.root, "masterAgent", "masterUser")
	mustMkdir(t, masterWS)
	ctx := WithToolWorkspace(store.WithTenantID(context.Background(), store.MasterTenantID), masterWS)

	cmd := "cat " + filepath.Join(fx.otherWS, "secret.txt")
	if res := tool.enforceTenantPathScope(ctx, cmd, masterWS); res != nil {
		t.Fatalf("master scope should not be constrained, got deny: %q", res.ForLLM)
	}
}

// TestExecuteOnHostBlocksCrossTenant proves the enforcement is wired into the
// host execution path end-to-end (the command must never run).
func TestExecuteOnHostBlocksCrossTenant(t *testing.T) {
	fx := newTenantScopeFixture(t)
	tool := &ExecTool{workspace: fx.root}
	ctx := tenantCtx(fx.ownWS)

	res := tool.executeOnHost(ctx, "cat "+filepath.Join(fx.otherWS, "secret.txt"), fx.ownWS)
	if res == nil || !res.IsError {
		t.Fatalf("expected error result, got %+v", res)
	}
	if strings.Contains(res.ForLLM, "sibling-secret") {
		t.Fatalf("sibling file contents leaked through despite deny: %q", res.ForLLM)
	}
	if !strings.Contains(res.ForLLM, "cross-tenant isolation policy") {
		t.Fatalf("expected policy deny message, got: %q", res.ForLLM)
	}
}
