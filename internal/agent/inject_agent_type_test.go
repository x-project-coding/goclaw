package agent

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// These tests pin the REAL context wiring, not just the interceptor logic:
// injectContext is the function the chat pipeline calls to build the tool
// execution context, so AgentTypeFromContext must resolve the agent's type
// from its output. A regression here silently disables every agent-type-gated
// behavior downstream (shared/private memory routing, context-file gating).

func injectCtxForType(t *testing.T, agentType string) context.Context {
	t.Helper()
	loop := NewLoop(LoopConfig{
		ID:        "wiring-test",
		AgentUUID: uuid.New(),
		AgentType: agentType,
		Sessions:  &nopSessionStore{},
	})
	req := &RunRequest{
		UserID:     "user1",
		SessionKey: "sess-wiring",
		Message:    "hello",
	}
	result, err := loop.injectContext(context.Background(), req)
	if err != nil {
		t.Fatalf("injectContext failed: %v", err)
	}
	return result.ctx
}

func TestInjectContext_PropagatesPredefinedAgentType(t *testing.T) {
	ctx := injectCtxForType(t, store.AgentTypePredefined)
	if got := store.AgentTypeFromContext(ctx); got != store.AgentTypePredefined {
		t.Errorf("AgentTypeFromContext = %q, want %q", got, store.AgentTypePredefined)
	}
}

func TestInjectContext_PropagatesOpenAgentType(t *testing.T) {
	ctx := injectCtxForType(t, store.AgentTypeOpen)
	if got := store.AgentTypeFromContext(ctx); got != store.AgentTypeOpen {
		t.Errorf("AgentTypeFromContext = %q, want %q", got, store.AgentTypeOpen)
	}
}

// TestInjectContext_RunContextCarriesAgentType pins the RunContext snapshot
// fallback: even a consumer reading only the RunContext (not the individual
// ctx key) must see the agent type.
func TestInjectContext_RunContextCarriesAgentType(t *testing.T) {
	ctx := injectCtxForType(t, store.AgentTypePredefined)
	rc := store.RunContextFromCtx(ctx)
	if rc == nil {
		t.Fatal("expected RunContext in injected ctx")
	}
	if rc.AgentType != store.AgentTypePredefined {
		t.Errorf("RunContext.AgentType = %q, want %q", rc.AgentType, store.AgentTypePredefined)
	}
}
