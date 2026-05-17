package providers

import (
	"context"
	"net/http"
)

// xrouterIdentityKey is a private context-key type used to thread per-request
// agent / user / session identity into XRouterProvider's HTTP transport layer
// without altering OpenAIProvider call signatures. The workspace anchor is
// implicit — bound to the xrt_* bearer key on the llm_providers row.
type xrouterIdentityKey struct{}

type xrouterIdentity struct {
	agentID   string
	userID    string
	sessionID string
	// routingMode is the per-session routing mode ('auto'|'fast'|'complex').
	// 42bucks fork patch: surfaced as the X-Router-Mode header so x-router
	// dispatches to the right upstream model for the session's mode.
	routingMode string
}

// xrouterRoundTripper wraps an http.RoundTripper and adds X-Router-* headers
// when the request context carries an xrouterIdentity. Missing / empty fields
// are silently skipped — the request still succeeds, just with thinner
// attribution on the router side.
type xrouterRoundTripper struct{ inner http.RoundTripper }

func (rt *xrouterRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if id, ok := req.Context().Value(xrouterIdentityKey{}).(xrouterIdentity); ok {
		if id.agentID != "" {
			req.Header.Set("X-Router-Agent-Id", id.agentID)
		}
		if id.userID != "" {
			req.Header.Set("X-Router-User-Id", id.userID)
		}
		if id.sessionID != "" {
			req.Header.Set("X-Router-Session-Id", id.sessionID)
		}
		// 42bucks fork patch: per-session routing mode → X-Router-Mode header.
		if id.routingMode != "" {
			req.Header.Set("X-Router-Mode", id.routingMode)
		}
	}
	return rt.inner.RoundTrip(req)
}

// XRouterProvider routes OpenAI-shape traffic through the 42bucks router
// gateway (router.42bucks.com). It composes *OpenAIProvider and wraps the
// embedded http.Client.Transport with xrouterRoundTripper so per-request
// identity from ChatRequest.Options surfaces as X-Router-* headers at HTTP
// send time.
//
// Identity keys read from req.Options are the same the Claude CLI provider
// uses (OptAgentID / OptUserID / OptSessionKey) so no agent-loop changes are
// needed — internal/agent/loop_pipeline_callbacks.go already populates them.
type XRouterProvider struct {
	*OpenAIProvider
}

// NewXRouterProvider constructs an XRouterProvider. apiKey must be an xrt_*
// router key issued by router.42bucks.com (one key per workspace; that's
// where the workspace billing attribution comes from).
func NewXRouterProvider(name, apiKey, apiBase, defaultModel string) *XRouterProvider {
	inner := NewOpenAIProvider(name, apiKey, apiBase, defaultModel)
	base := inner.client.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	inner.client.Transport = &xrouterRoundTripper{inner: base}
	return &XRouterProvider{OpenAIProvider: inner}
}

func (p *XRouterProvider) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	return p.OpenAIProvider.Chat(injectXRouterIdentity(ctx, req), req)
}

func (p *XRouterProvider) ChatStream(ctx context.Context, req ChatRequest, onChunk func(StreamChunk)) (*ChatResponse, error) {
	return p.OpenAIProvider.ChatStream(injectXRouterIdentity(ctx, req), req, onChunk)
}

// injectXRouterIdentity reads OptAgentID / OptUserID / OptSessionKey and the
// 42bucks-fork OptRoutingMode off req.Options and stashes them on a derived
// ctx so xrouterRoundTripper can read them at HTTP send time. Returns ctx
// unchanged if nothing identity-y is present.
func injectXRouterIdentity(ctx context.Context, req ChatRequest) context.Context {
	if req.Options == nil {
		return ctx
	}
	var id xrouterIdentity
	if v, ok := req.Options[OptAgentID].(string); ok {
		id.agentID = v
	}
	if v, ok := req.Options[OptUserID].(string); ok {
		id.userID = v
	}
	if v, ok := req.Options[OptSessionKey].(string); ok {
		id.sessionID = v
	}
	// 42bucks fork patch: per-session routing mode → X-Router-Mode header.
	if v, ok := req.Options[OptRoutingMode].(string); ok {
		id.routingMode = v
	}
	if id.agentID == "" && id.userID == "" && id.sessionID == "" && id.routingMode == "" {
		return ctx
	}
	return context.WithValue(ctx, xrouterIdentityKey{}, id)
}
