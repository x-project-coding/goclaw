package cmd

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/store"
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

func TestBuildReviewFallbackMessage(t *testing.T) {
	m := buildReviewFallbackMessage("Landing page build", "roman", "It's live at https://x")
	if !strings.HasPrefix(m, "roman finished: Landing page build\n\n") {
		t.Errorf("fallback title line wrong: %s", m)
	}
	if !strings.Contains(m, "It's live at https://x") {
		t.Errorf("fallback missing the raw result: %s", m)
	}

	// Generic wording when the label / specialist name are unknown.
	fb := buildReviewFallbackMessage("", "", "raw result")
	if !strings.HasPrefix(fb, "The specialist finished: the delegated task\n\n") {
		t.Errorf("generic fallback title wrong: %s", fb)
	}
	if !strings.Contains(fb, "raw result") {
		t.Errorf("generic fallback missing content: %s", fb)
	}
}

// TestDeliverDelegateResultFallbackOnReviewFailure proves the never-silent
// guarantee on the SESSION lane: when the ops-lead review run fails, the
// specialist's result is delivered as a plain announce into the origin chat
// EXACTLY ONCE — a re-emitted completion event for the same run is skipped by
// the optimistic resultDeliveredRunID stamp — and the resultDelivered* stamps
// keep the daily backstop from re-reporting a result the user already has.
func TestDeliverDelegateResultFallbackOnReviewFailure(t *testing.T) {
	const delegateKey = "system:delegate:ws1:abc123"
	const originKey = "agent:samantha:ws:direct:user-1"

	sessStore := newFakeSessionStore()
	sessStore.SetSessionMetadata(context.Background(), delegateKey, map[string]string{
		dmMetaOriginSessionKey: originKey,
		dmMetaOriginUserID:     "user-1",
		dmMetaTargetAgent:      "roman",
		dmMetaGoal:             "Landing page build",
	})
	msgBus := bus.New()
	sched := failingScheduler(errors.New("upstream 400"))
	d := &gatewayDeps{msgBus: msgBus, pgStores: &store.Stores{Sessions: sessStore}}

	d.deliverDelegateResult(uuid.Nil, delegateKey, "run-1", "the finished result", sched)

	// The fallback announce is persisted into the ORIGIN session with the
	// manager framing (goal + specialist) and the raw result.
	msgs := sessStore.history(originKey)
	if len(msgs) != 1 {
		t.Fatalf("fallback messages persisted = %d, want exactly 1", len(msgs))
	}
	if msgs[0].Role != "assistant" || msgs[0].SenderID != "samantha" {
		t.Errorf("fallback message role/sender = %q/%q, want assistant/samantha", msgs[0].Role, msgs[0].SenderID)
	}
	for _, want := range []string{"roman finished: Landing page build", "the finished result"} {
		if !strings.Contains(msgs[0].Content, want) {
			t.Errorf("fallback message missing %q\ncontent: %s", want, msgs[0].Content)
		}
	}
	// ...and pushed to the origin channel.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if out, ok := msgBus.SubscribeOutbound(ctx); !ok || out.Channel != "ws" || out.ChatID != "user-1" ||
		!strings.Contains(out.Content, "the finished result") {
		t.Errorf("expected the fallback announce outbound to ws/user-1, got ok=%v out=%+v", ok, out)
	}
	// Delivered (via fallback) → both stamps set: the per-run guard and the
	// timestamp the daily backstop reads.
	meta := sessStore.GetSessionMetadata(context.Background(), delegateKey)
	if meta[dmMetaResultDeliveredRunID] != "run-1" {
		t.Errorf("resultDeliveredRunID = %q, want run-1", meta[dmMetaResultDeliveredRunID])
	}
	if meta[dmMetaResultDeliveredAt] == "" {
		t.Error("resultDeliveredAt not stamped after fallback delivery")
	}

	// A re-emitted completion event for the SAME run: at-most-once.
	d.deliverDelegateResult(uuid.Nil, delegateKey, "run-1", "the finished result", sched)
	if got := len(sessStore.history(originKey)); got != 1 {
		t.Fatalf("fallback messages after duplicate event = %d, want still exactly 1", got)
	}
	noCtx, noCancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer noCancel()
	if out, ok := msgBus.SubscribeOutbound(noCtx); ok {
		t.Errorf("no second fallback outbound expected on duplicate event, got %+v", out)
	}
}

// TestDeliverDelegateResultSuccessNoFallback proves a successful review turn
// never posts the fallback announce — the ops-lead's own reply is the only
// user-facing message.
func TestDeliverDelegateResultSuccessNoFallback(t *testing.T) {
	const delegateKey = "system:delegate:ws1:def456"
	const originKey = "agent:samantha:ws:direct:user-2"

	sessStore := newFakeSessionStore()
	sessStore.SetSessionMetadata(context.Background(), delegateKey, map[string]string{
		dmMetaOriginSessionKey: originKey,
		dmMetaOriginUserID:     "user-2",
	})
	msgBus := bus.New()
	var captured agent.RunRequest
	sched := capturingScheduler("Reviewed: shipping it.", &captured)
	d := &gatewayDeps{msgBus: msgBus, pgStores: &store.Stores{Sessions: sessStore}}

	d.deliverDelegateResult(uuid.Nil, delegateKey, "run-1", "the finished result", sched)

	if got := len(sessStore.history(originKey)); got != 0 {
		t.Errorf("no fallback message should be persisted on a successful review, got %d", got)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if out, ok := msgBus.SubscribeOutbound(ctx); !ok || out.Content != "Reviewed: shipping it." {
		t.Errorf("expected only the ops-lead reply outbound, got ok=%v out=%+v", ok, out)
	}
	if meta := sessStore.GetSessionMetadata(context.Background(), delegateKey); meta[dmMetaResultDeliveredAt] == "" {
		t.Error("resultDeliveredAt not stamped after a successful review turn")
	}
}
