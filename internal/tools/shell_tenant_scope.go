package tools

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
)

// Cross-tenant filesystem isolation for the UNSANDBOXED host-exec path.
//
// PR #100 / commit 4ed4eb95 ("Issue 68") narrowed the Docker-sandbox bind
// mount to the caller's own tenant subtree so a sandboxed `sh -c` cannot reach
// /app/workspace/tenants/<other> via an absolute path. That fix only helps when
// a Docker sandbox is actually engaged. In deployments where sandboxing is not
// configured (the current production state — no docker.sock, no Sandbox config)
// 100% of exec calls fall through to executeOnHost, whose only prior
// confinement was the command's working directory. A shell command that NAMES
// an absolute path (`find /app/workspace ...`, `cat /app/workspace/tenants/<x>`)
// or escapes the cwd with `../` therefore walked the entire multi-tenant tree.
//
// tenantExecScope mirrors the sandbox mount-narrowing on the host path: it
// resolves the caller's own tenant subtree and rejects any command argument
// whose (symlink-resolved) absolute path would reach a sibling tenant.

// tenantExecScope captures the boundaries of the caller's tenant workspace
// subtree used to validate host-exec path arguments.
type tenantExecScope struct {
	wsRoot      string // global workspace root, e.g. /app/workspace
	tenantsBase string // <wsRoot>/tenants — the shared multi-tenant directory
	ownRoot     string // <tenantsBase>/<own-tenant> — the caller's own subtree
}

// tenantExecScopeFor derives the caller's tenant subtree from context.
//
// The gate is purely workspace-derived: enforcement applies exactly when the
// caller's resolved workspace lives under <globalWorkspace>/tenants/<slug>,
// which is by definition a single tenant that must not read siblings. This is
// deliberately NOT gated on store.IsMasterScope — that predicate is true when
// the tenant id is merely unset (uuid.Nil), which would silently disable
// isolation on a transient context gap, and it would exempt the very
// system/QA agents (e.g. the flagged e2e-QA session) the issue wants confined.
//
// ok=false means NO enforcement applies: genuine master/super-admin runs use
// the global workspace root (…/<agent>/<user>, no tenants/ prefix) and fall out
// here, as do empty-workspace and legacy single-tenant layouts.
func tenantExecScopeFor(ctx context.Context, globalWorkspace string) (tenantExecScope, bool) {
	ws := ToolWorkspaceFromCtx(ctx)
	if ws == "" || globalWorkspace == "" {
		return tenantExecScope{}, false
	}
	wsRoot := canonicalSandboxWorkspace(globalWorkspace)
	tenantsBase := canonicalSandboxWorkspace(filepath.Join(wsRoot, "tenants"))
	wsReal := canonicalSandboxWorkspace(ws)

	// The caller must live under the shared tenants/ tree for a sibling-tenant
	// boundary to exist. Master workspaces (…/<agent>/<user>, no tenants/
	// prefix) fall out here.
	if !isPathInside(wsReal, tenantsBase) {
		return tenantExecScope{}, false
	}
	rel, err := filepath.Rel(tenantsBase, wsReal)
	if err != nil {
		return tenantExecScope{}, false
	}
	first := filepath.ToSlash(rel)
	if idx := strings.IndexByte(first, '/'); idx >= 0 {
		first = first[:idx]
	}
	if first == "" || first == "." || first == ".." {
		return tenantExecScope{}, false
	}
	return tenantExecScope{
		wsRoot:      wsRoot,
		tenantsBase: tenantsBase,
		ownRoot:     filepath.Join(tenantsBase, first),
	}, true
}

// crossesTenantBoundary reports whether a resolved absolute path would let the
// caller reach OUTSIDE its own tenant subtree into a sibling tenant, either:
//   - directly: the path is under tenants/<other> (or the tenants/ index
//     itself, which enumerates sibling names), or
//   - via a recursive walk: the path is the workspace root or an ancestor of
//     it (e.g. `find /app/workspace`, `grep -r /`), from which find/grep/ls -R
//     descend into every sibling tenant.
//
// Paths inside the caller's OWN subtree, and paths entirely outside the
// workspace tree (/tmp, /app/bin, /etc, …), are allowed — this is the
// conservative "block only what actually crosses tenants" rule.
func (s tenantExecScope) crossesTenantBoundary(realPath string) bool {
	if isPathInside(realPath, s.ownRoot) {
		return false // own subtree — always allowed (incl. own absolute paths)
	}
	if isPathInside(realPath, s.tenantsBase) {
		return true // a sibling tenant subtree, or the tenants/ index itself
	}
	// realPath is the workspace root or an ancestor of it; a recursive tool
	// rooted here would descend into sibling tenants under tenants/. The
	// filesystem root ("/") is handled explicitly because isPathInside builds
	// parent+separator ("//"), which never prefix-matches (e.g. `find /`).
	if realPath == string(filepath.Separator) || isPathInside(s.wsRoot, realPath) {
		return true
	}
	return false // outside the workspace tree entirely — not a cross-tenant read
}

// enforceTenantPathScope validates every filesystem-path argument in a host
// shell command against the caller's tenant subtree. Returns a tool error
// Result naming the offending path (models can retry with a relative path or an
// in-tenant path) when the command would cross into another tenant, or nil when
// the command is in-bounds or no tenant scoping applies.
//
// baseDir is the command's working directory: relative candidates are resolved
// against it so `../` escapes are caught the same way absolute paths are.
func (t *ExecTool) enforceTenantPathScope(ctx context.Context, command, baseDir string) *Result {
	scope, ok := tenantExecScopeFor(ctx, t.workspace)
	if !ok {
		return nil
	}
	// Normalize (NFKC + zero-width strip) so unicode-obfuscated paths cannot
	// slip past the ancestor/sibling comparisons. extractPathCandidates already
	// narrows each word to path-shaped tokens (absolute, ./, ../, contains "/",
	// ~/, tenants/, …), so every candidate is symlink-resolved against baseDir
	// and checked — this catches absolute leaks (`find /app/workspace`), `../`
	// escapes, relative symlink hops, and `tenants/*` globs alike.
	for _, word := range parseExecCommandWords(normalizeCommand(command)) {
		for _, candidate := range extractPathCandidates(word) {
			realPath, err := canonicalizeExecPath(candidate, baseDir)
			if err != nil {
				continue
			}
			if scope.crossesTenantBoundary(realPath) {
				return ErrorResult(fmt.Sprintf(
					"exec: path %q is blocked by cross-tenant isolation policy — it resolves outside your own workspace and would reach another tenant's files. Use a relative path or an absolute path under your own workspace (%s).",
					candidate, scope.ownRoot))
			}
		}
	}
	return nil
}
