package providers

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// captureTransport snapshots the headers of the FIRST request it sees,
// returns a small canned OK body, then forwards any further requests to
// the wrapped transport. Used so the test reads what the RoundTripper put
// on the wire without needing a fake upstream.
type captureTransport struct {
	headers atomic.Pointer[http.Header]
	hit     atomic.Int32
}

func (c *captureTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if c.hit.Add(1) == 1 {
		h := req.Header.Clone()
		c.headers.Store(&h)
	}
	body := io.NopCloser(strings.NewReader(`{"choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	return &http.Response{StatusCode: 200, Header: make(http.Header), Body: body, Request: req}, nil
}

// mkXRouterProviderWithCapture installs a captureTransport beneath the
// xrouterRoundTripper so the test can assert what headers reach the wire.
func mkXRouterProviderWithCapture(t *testing.T) (*XRouterProvider, *captureTransport) {
	t.Helper()
	p := NewXRouterProvider("xrouter-test", "xrt_test_key", "https://router.42bucks.com/v1", "openai/gpt-4o-mini")
	rt, ok := p.OpenAIProvider.client.Transport.(*xrouterRoundTripper)
	if !ok {
		t.Fatalf("expected *xrouterRoundTripper transport, got %T", p.OpenAIProvider.client.Transport)
	}
	cap := &captureTransport{}
	rt.inner = cap
	return p, cap
}

// TestXRouterProvider_Chat_SetsIdentityHeaders — happy path. When the agent
// loop has populated req.Options with all three identity values, the
// transport must surface them as X-Router-* headers AND keep the
// inherited Authorization Bearer header on the request.
func TestXRouterProvider_Chat_SetsIdentityHeaders(t *testing.T) {
	p, cap := mkXRouterProviderWithCapture(t)
	_, err := p.Chat(context.Background(), ChatRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
		Model:    "openai/gpt-4o-mini",
		Options: map[string]any{
			OptAgentID:    "agent-42",
			OptUserID:     "user-99",
			OptSessionKey: "sess-2026-01-01",
		},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	h := cap.headers.Load()
	if h == nil {
		t.Fatal("no request captured")
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
	if got := h.Get("Authorization"); got != "Bearer xrt_test_key" {
		t.Errorf("Authorization = %q, want Bearer xrt_test_key", got)
	}
}

// TestXRouterProvider_Chat_SkipsMissingIdentity — calls without identity in
// Options still go through cleanly (workspace anchor via Bearer key is
// always present), they just have thinner attribution at the router.
func TestXRouterProvider_Chat_SkipsMissingIdentity(t *testing.T) {
	p, cap := mkXRouterProviderWithCapture(t)
	_, err := p.Chat(context.Background(), ChatRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
		Model:    "openai/gpt-4o-mini",
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	h := cap.headers.Load()
	if h == nil {
		t.Fatal("no request captured")
	}
	for _, name := range []string{"X-Router-Agent-Id", "X-Router-User-Id", "X-Router-Session-Id", "X-Router-Mode"} {
		if got := h.Get(name); got != "" {
			t.Errorf("%s = %q, want empty when Options absent", name, got)
		}
	}
}

// TestXRouterProvider_Chat_SetsRoutingModeHeader — 42bucks fork patch. When
// the agent loop populates OptRoutingMode (per-session routing mode), the
// transport must surface it as the X-Router-Mode header so x-router dispatches
// to the upstream model for the session's mode.
func TestXRouterProvider_Chat_SetsRoutingModeHeader(t *testing.T) {
	p, cap := mkXRouterProviderWithCapture(t)
	_, err := p.Chat(context.Background(), ChatRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
		Model:    "openai/gpt-4o-mini",
		Options: map[string]any{
			OptAgentID:     "agent-42",
			OptRoutingMode: "fast",
		},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	h := cap.headers.Load()
	if h == nil {
		t.Fatal("no request captured")
	}
	if got := h.Get("X-Router-Mode"); got != "fast" {
		t.Errorf("X-Router-Mode = %q, want fast", got)
	}
	if got := h.Get("X-Router-Agent-Id"); got != "agent-42" {
		t.Errorf("X-Router-Agent-Id = %q, want agent-42", got)
	}
}

// TestXRouterProvider_Chat_RoutingModeOnly — routingMode is the sole entry in
// Options. The X-Router-Mode header must still be set even though no identity
// fields are present (injectXRouterIdentity must not early-return).
func TestXRouterProvider_Chat_RoutingModeOnly(t *testing.T) {
	p, cap := mkXRouterProviderWithCapture(t)
	_, err := p.Chat(context.Background(), ChatRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
		Model:    "openai/gpt-4o-mini",
		Options:  map[string]any{OptRoutingMode: "complex"},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	h := cap.headers.Load()
	if h == nil {
		t.Fatal("no request captured")
	}
	if got := h.Get("X-Router-Mode"); got != "complex" {
		t.Errorf("X-Router-Mode = %q, want complex", got)
	}
}

// TestXRouterProvider_Chat_SkipsMissingRoutingMode — no routingMode in Options
// means no X-Router-Mode header (custom-mode runs use modelOverride only and
// must send no routing-mode header — matches existing behaviour).
func TestXRouterProvider_Chat_SkipsMissingRoutingMode(t *testing.T) {
	p, cap := mkXRouterProviderWithCapture(t)
	_, err := p.Chat(context.Background(), ChatRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
		Model:    "openai/gpt-4o-mini",
		Options:  map[string]any{OptAgentID: "agent-42"},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	h := cap.headers.Load()
	if h == nil {
		t.Fatal("no request captured")
	}
	if got := h.Get("X-Router-Mode"); got != "" {
		t.Errorf("X-Router-Mode = %q, want empty when routingMode absent", got)
	}
}

// TestXRouterProvider_Chat_SkipsEmptyStringIdentity — defensive: empty
// strings in Options shouldn't materialize as empty-valued headers.
func TestXRouterProvider_Chat_SkipsEmptyStringIdentity(t *testing.T) {
	p, cap := mkXRouterProviderWithCapture(t)
	_, err := p.Chat(context.Background(), ChatRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
		Model:    "openai/gpt-4o-mini",
		Options: map[string]any{
			OptAgentID:     "",
			OptUserID:      "",
			OptSessionKey:  "",
			OptRoutingMode: "",
		},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	h := cap.headers.Load()
	if h == nil {
		t.Fatal("no request captured")
	}
	for _, name := range []string{"X-Router-Agent-Id", "X-Router-User-Id", "X-Router-Session-Id", "X-Router-Mode"} {
		if got := h.Get(name); got != "" {
			t.Errorf("%s = %q, want empty when Options is empty-string", name, got)
		}
	}
}

// TestXRouterProvider_Chat_PartialIdentity — only one of three populated;
// the missing two should not appear as headers but the present one should.
func TestXRouterProvider_Chat_PartialIdentity(t *testing.T) {
	p, cap := mkXRouterProviderWithCapture(t)
	_, err := p.Chat(context.Background(), ChatRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
		Options:  map[string]any{OptAgentID: "lonely-agent"},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	h := cap.headers.Load()
	if got := h.Get("X-Router-Agent-Id"); got != "lonely-agent" {
		t.Errorf("X-Router-Agent-Id = %q, want lonely-agent", got)
	}
	if got := h.Get("X-Router-User-Id"); got != "" {
		t.Errorf("X-Router-User-Id = %q, want empty", got)
	}
}

// TestXRouterProvider_NameInheritedFromOpenAI — verifies composition keeps
// the parent's Name() (we deliberately don't override). The store registers
// the provider by row name, not by Name(), so this is purely for downstream
// logging clarity.
func TestXRouterProvider_NameInheritedFromOpenAI(t *testing.T) {
	p := NewXRouterProvider("xrouter-acme", "xrt_anything", "https://router.42bucks.com/v1", "")
	if p.Name() != "xrouter-acme" {
		t.Errorf("Name() = %q, want xrouter-acme", p.Name())
	}
}

// Smoke check that the package-private setupServer recipe still works for
// adapter parity tests — guards against future refactors of httptest that
// could break the capture pattern above.
var _ = httptest.NewServer
