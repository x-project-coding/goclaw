package providers

import (
	"testing"
)

// mkXRouterAdapter is a tiny helper so each test reads its own intent.
func mkXRouterAdapter(t *testing.T) ProviderAdapter {
	t.Helper()
	a, err := NewXRouterAdapter(ProviderConfig{
		APIKey:  "xrt_test_key",
		BaseURL: "https://router.42bucks.com/v1",
		Model:   "openai/gpt-4o-mini",
	})
	if err != nil {
		t.Fatalf("NewXRouterAdapter: %v", err)
	}
	return a
}

// TestXRouterAdapter_Basics — Name() identifies the adapter and Capabilities()
// inherits OpenAI's (streaming + tool calling), since the wire protocol is
// identical to OpenAI Chat Completions.
func TestXRouterAdapter_Basics(t *testing.T) {
	a := mkXRouterAdapter(t)
	if a.Name() != "xrouter" {
		t.Errorf("Name() = %q, want xrouter", a.Name())
	}
	caps := a.Capabilities()
	if !caps.Streaming {
		t.Error("expected Streaming=true (inherited from OpenAIAdapter)")
	}
	if !caps.ToolCalling {
		t.Error("expected ToolCalling=true (inherited from OpenAIAdapter)")
	}
}

// TestXRouterAdapter_ToRequest_AddsIdentityHeaders — when the agent loop has
// populated req.Options with agent/user/session, the adapter must surface
// them as X-Router-* headers so x-router can attribute usage.
func TestXRouterAdapter_ToRequest_AddsIdentityHeaders(t *testing.T) {
	a := mkXRouterAdapter(t)
	req := ChatRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
		Options: map[string]any{
			OptAgentID:    "agent-42",
			OptUserID:     "user-99",
			OptSessionKey: "sess-2026-01-01",
		},
	}
	_, h, err := a.ToRequest(req)
	if err != nil {
		t.Fatalf("ToRequest: %v", err)
	}
	if got := h.Get("X-Router-Agent-Id"); got != "agent-42" {
		t.Errorf("X-Router-Agent-Id = %q, want agent-42", got)
	}
	if got := h.Get("X-Router-User-Id"); got != "user-99" {
		t.Errorf("X-Router-User-Id = %q, want user-99", got)
	}
	if got := h.Get("X-Router-Session-Id"); got != "sess-2026-01-01" {
		t.Errorf("X-Router-Session-Id = %q, want sess-2026-01-01", got)
	}
	// Underlying OpenAI auth must still be wired so the request can actually leave.
	if got := h.Get("Authorization"); got != "Bearer xrt_test_key" {
		t.Errorf("Authorization = %q, want Bearer xrt_test_key", got)
	}
}

// TestXRouterAdapter_ToRequest_SkipsMissingIdentity — calls without any
// identity in Options must still produce a valid request (just with thinner
// attribution at the router). The workspace anchor (Bearer key) is always
// present so this still bills the right workspace.
func TestXRouterAdapter_ToRequest_SkipsMissingIdentity(t *testing.T) {
	a := mkXRouterAdapter(t)
	req := ChatRequest{Messages: []Message{{Role: "user", Content: "hi"}}}
	_, h, err := a.ToRequest(req)
	if err != nil {
		t.Fatalf("ToRequest: %v", err)
	}
	for _, name := range []string{"X-Router-Agent-Id", "X-Router-User-Id", "X-Router-Session-Id"} {
		if got := h.Get(name); got != "" {
			t.Errorf("%s = %q, want empty when Options absent", name, got)
		}
	}
}

// TestXRouterAdapter_ToRequest_SkipsEmptyStringIdentity — defensive: an empty
// string in Options shouldn't produce a header with an empty value.
func TestXRouterAdapter_ToRequest_SkipsEmptyStringIdentity(t *testing.T) {
	a := mkXRouterAdapter(t)
	req := ChatRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
		Options: map[string]any{
			OptAgentID:    "",
			OptUserID:     "",
			OptSessionKey: "",
		},
	}
	_, h, err := a.ToRequest(req)
	if err != nil {
		t.Fatalf("ToRequest: %v", err)
	}
	for _, name := range []string{"X-Router-Agent-Id", "X-Router-User-Id", "X-Router-Session-Id"} {
		if got := h.Get(name); got != "" {
			t.Errorf("%s = %q, want empty when Options value is empty string", name, got)
		}
	}
}

// TestXRouterAdapter_ToRequest_SkipsNonStringIdentity — defensive: if a
// future code path puts a non-string into Options (e.g. a UUID type), the
// adapter must not panic and must skip the header rather than coercing.
func TestXRouterAdapter_ToRequest_SkipsNonStringIdentity(t *testing.T) {
	a := mkXRouterAdapter(t)
	req := ChatRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
		Options: map[string]any{
			OptAgentID: 42, // wrong type — should be skipped, not set as "42"
		},
	}
	_, h, err := a.ToRequest(req)
	if err != nil {
		t.Fatalf("ToRequest: %v", err)
	}
	if got := h.Get("X-Router-Agent-Id"); got != "" {
		t.Errorf("X-Router-Agent-Id = %q, want empty when Options value is non-string", got)
	}
}
