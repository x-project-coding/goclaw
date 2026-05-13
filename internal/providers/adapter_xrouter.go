package providers

import (
	"fmt"
	"net/http"
)

// XRouterAdapter routes LLM traffic through the 42bucks router gateway
// (router.42bucks.com). The wire protocol is OpenAI Chat Completions, so this
// adapter delegates body construction + response parsing to the embedded
// OpenAIAdapter and adds three identity headers on the outbound request so
// x-router can attribute usage to the caller's workspace / agent / user /
// session for billing + per-dimension stats.
//
// Workspace is implicit — bound to the xrt_* router key configured on the
// llm_providers row this adapter was constructed from. The three optional
// per-request headers are pulled from req.Options (populated by the agent
// loop in internal/agent/loop_pipeline_callbacks.go):
//
//	X-Router-Agent-Id    ← OptAgentID
//	X-Router-User-Id     ← OptUserID
//	X-Router-Session-Id  ← OptSessionKey
//
// Missing options are silently skipped — the request still succeeds, just
// with thinner billing attribution. The workspace anchor (the xrt_* bearer
// token) is always present so workspace-level billing is never lost.
type XRouterAdapter struct {
	inner *OpenAIAdapter
}

// NewXRouterAdapter creates an x-router adapter. cfg.APIKey must be an xrt_*
// router key issued by router.42bucks.com (one key per workspace).
func NewXRouterAdapter(cfg ProviderConfig) (ProviderAdapter, error) {
	inner, err := NewOpenAIAdapter(cfg)
	if err != nil {
		return nil, err
	}
	// Explicit type check mirrors adapter_dashscope.go:28-31 so a future
	// refactor of NewOpenAIAdapter's concrete return type fails loudly at
	// construction rather than panicking on first ToRequest call.
	oa, ok := inner.(*OpenAIAdapter)
	if !ok {
		return nil, fmt.Errorf("xrouter adapter: unexpected inner type %T", inner)
	}
	return &XRouterAdapter{inner: oa}, nil
}

func (a *XRouterAdapter) Name() string                       { return "xrouter" }
func (a *XRouterAdapter) Capabilities() ProviderCapabilities { return a.inner.Capabilities() }

func (a *XRouterAdapter) FromResponse(data []byte) (*ChatResponse, error) {
	return a.inner.FromResponse(data)
}

func (a *XRouterAdapter) FromStreamChunk(data []byte) (*StreamChunk, error) {
	return a.inner.FromStreamChunk(data)
}

// ToRequest builds the OpenAI-shape body via the wrapped adapter, then layers
// on the X-Router-* identity headers from req.Options. Type-assertion failures
// and empty strings are silently dropped — never blocks the request.
func (a *XRouterAdapter) ToRequest(req ChatRequest) ([]byte, http.Header, error) {
	body, h, err := a.inner.ToRequest(req)
	if err != nil {
		return nil, nil, err
	}
	if v, ok := req.Options[OptAgentID].(string); ok && v != "" {
		h.Set("X-Router-Agent-Id", v)
	}
	if v, ok := req.Options[OptUserID].(string); ok && v != "" {
		h.Set("X-Router-User-Id", v)
	}
	if v, ok := req.Options[OptSessionKey].(string); ok && v != "" {
		h.Set("X-Router-Session-Id", v)
	}
	return body, h, nil
}
