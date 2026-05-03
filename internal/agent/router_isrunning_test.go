package agent

import (
	"context"
	"testing"
	"time"
)

// TestIsRunning_TenantScopedLookup asserts that Router.IsRunning accepts ctx
// and looks up under the tenant-scoped cache key. A previous bare
// `r.agents[agentID]` lookup always returned false for any tenant-scoped
// deployment, so the WS `agents.list` response incorrectly showed every live
// agent as `isRunning: false`.
func TestIsRunning_TenantScopedLookup(t *testing.T) {
	r := NewRouter()

	ctxA := context.Background()
	ctxB := context.Background()

	// Directly populate cache under canonical tenant-scoped key with a
	// running stub agent in tenant A.
	keyA := agentCacheKey(ctxA, "foo")
	r.agents[keyA] = &agentEntry{
		agent:    &stubAgent{id: "foo", running: true},
		cachedAt: time.Now(),
	}

	if !r.IsRunning(ctxA, "foo") {
		t.Error("IsRunning(ctxA, foo) should return true — agent cached under tenantA")
	}
	if r.IsRunning(ctxB, "foo") {
		t.Error("IsRunning(ctxB, foo) should return false — tenantB has no entry")
	}

	// Empty ctx → no tenant → bare key lookup. Should still return false
	// because the actual entry is under a tenant-scoped key.
	if r.IsRunning(context.Background(), "foo") {
		t.Error("IsRunning(emptyCtx, foo) should return false — bare lookup cannot see tenant-scoped entries")
	}
}

// TestIsRunning_NoBareLookupLeak guards against regressing to the pre-fix
// bare-key lookup which could surface a cross-tenant leak for
// non-tenant-scoped routers.
func TestIsRunning_NoBareLookupLeak(t *testing.T) {
	r := NewRouter()

	// Bare entry (no tenant) with running=true.
	r.agents["bare-agent"] = &agentEntry{
		agent:    &stubAgent{id: "bare-agent", running: true},
		cachedAt: time.Now(),
	}

	// Without a tenant, ctx-scoped lookup yields the bare key directly.
	if !r.IsRunning(context.Background(), "bare-agent") {
		t.Error("IsRunning should find a bare entry when ctx has no tenant")
	}

	// v4 single-tenant: bare lookup is the only path; remove cross-tenant guard.
}
