package cmd

import (
	"strings"
	"testing"
)

func TestEvaluateDelegateDelivery(t *testing.T) {
	const origin = "agent:samantha:ws:direct:user-1"
	base := map[string]string{dmMetaOriginSessionKey: origin}

	cases := []struct {
		name        string
		sessionKey  string
		meta        map[string]string
		runID       string
		content     string
		wantDeliver bool
		wantReason  string
	}{
		{
			name:        "delivers a delegate completion with origin + real content",
			sessionKey:  "system:delegate:ws1:abc123",
			meta:        base,
			runID:       "run-1",
			content:     "Deployed the landing page: https://cdn.example/x.html",
			wantDeliver: true,
			wantReason:  "ok",
		},
		{
			name:        "prefix gate excludes a normal ws chat session",
			sessionKey:  origin, // agent:...:ws:direct:... — NOT system:delegate:*
			meta:        base,
			runID:       "run-1",
			content:     "hello",
			wantDeliver: false,
			wantReason:  "not-delegate",
		},
		{
			name:        "skips when no originSessionKey was stamped (degrades to backstop)",
			sessionKey:  "system:delegate:ws1:abc123",
			meta:        map[string]string{dmMetaGoal: "ship it"},
			runID:       "run-1",
			content:     "done",
			wantDeliver: false,
			wantReason:  "no-origin",
		},
		{
			name:        "idempotent: same run already delivered",
			sessionKey:  "system:delegate:ws1:abc123",
			meta:        map[string]string{dmMetaOriginSessionKey: origin, dmMetaResultDeliveredRunID: "run-1"},
			runID:       "run-1",
			content:     "done again",
			wantDeliver: false,
			wantReason:  "already-delivered",
		},
		{
			name:       "follow-up run delivers even after a prior delivery (per-run, not a lock)",
			sessionKey: "system:delegate:ws1:abc123",
			meta: map[string]string{
				dmMetaOriginSessionKey:     origin,
				dmMetaResultDeliveredRunID: "run-1",
				dmMetaResultDeliveredAt:    "2026-07-12T10:00:00Z",
			},
			runID:       "run-2",
			content:     "second round result",
			wantDeliver: true,
			wantReason:  "ok",
		},
		{
			name:        "skips empty content",
			sessionKey:  "system:delegate:ws1:abc123",
			meta:        base,
			runID:       "run-1",
			content:     "   ",
			wantDeliver: false,
			wantReason:  "empty-or-silent",
		},
		{
			name:        "skips NO_REPLY finals",
			sessionKey:  "system:delegate:ws1:abc123",
			meta:        base,
			runID:       "run-1",
			content:     "NO_REPLY: nothing to add",
			wantDeliver: false,
			wantReason:  "empty-or-silent",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := evaluateDelegateDelivery(tc.sessionKey, tc.meta, tc.runID, tc.content)
			if got.Deliver != tc.wantDeliver {
				t.Fatalf("Deliver = %v, want %v (reason=%q)", got.Deliver, tc.wantDeliver, got.Reason)
			}
			if got.Reason != tc.wantReason {
				t.Fatalf("Reason = %q, want %q", got.Reason, tc.wantReason)
			}
		})
	}
}

func TestParseSessionRouting(t *testing.T) {
	cases := []struct {
		key               string
		channel, pk, chat string
	}{
		{"agent:samantha:ws:direct:user-1", "ws", "direct", "user-1"},
		{"agent:samantha:telegram:direct:12345", "telegram", "direct", "12345"},
		{"agent:samantha:ws:direct:user:with:colons", "ws", "direct", "user:with:colons"},
		{"system:delegate:ws1:abc", "", "", ""},
		{"malformed", "", "", ""},
	}
	for _, tc := range cases {
		ch, pk, chat := parseSessionRouting(tc.key)
		if ch != tc.channel || pk != tc.pk || chat != tc.chat {
			t.Errorf("parseSessionRouting(%q) = (%q,%q,%q), want (%q,%q,%q)",
				tc.key, ch, pk, chat, tc.channel, tc.pk, tc.chat)
		}
	}
}

func TestRunCompletedContent(t *testing.T) {
	if got := runCompletedContent(map[string]any{"content": "hi there"}); got != "hi there" {
		t.Errorf("content = %q, want %q", got, "hi there")
	}
	if got := runCompletedContent(map[string]any{"thinking": "x"}); got != "" {
		t.Errorf("missing content should be empty, got %q", got)
	}
	if got := runCompletedContent("not a map"); got != "" {
		t.Errorf("non-map payload should be empty, got %q", got)
	}
}

func TestBuildDelegateReviewPrompt(t *testing.T) {
	p := buildDelegateReviewPrompt("Landing page build", "roman", "It's live at https://x")
	for _, want := range []string{"Landing page build", "roman", "It's live at https://x", "in your own words", "review-fix tool"} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt missing %q\nprompt: %s", want, p)
		}
	}

	// Fallbacks when label / targetAgent are missing.
	fb := buildDelegateReviewPrompt("", "", "raw result")
	if !strings.Contains(fb, "the specialist") || !strings.Contains(fb, "the delegated task") {
		t.Errorf("fallback prompt missing generic phrasing: %s", fb)
	}
	if !strings.Contains(fb, "raw result") {
		t.Errorf("fallback prompt missing content: %s", fb)
	}
}
