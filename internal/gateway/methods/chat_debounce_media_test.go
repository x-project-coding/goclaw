package methods

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/config"
)

// TestChatDebounceDelay_HasMediaWithZeroConfigAppliesFloor — Phase 1.5 Rule #2.
// When global debounce is disabled, no agent override, and the message carries
// media, the 1000ms media floor MUST be applied.
func TestChatDebounceDelay_HasMediaWithZeroConfigAppliesFloor(t *testing.T) {
	got := chatDebounceDelay(&config.Config{}, nil, true)
	want := time.Duration(chatMediaDebounceFloorMs) * time.Millisecond
	if got != want {
		t.Fatalf("chatDebounceDelay(cfg=0, hasMedia=true) = %s, want %s", got, want)
	}
}

// TestChatDebounceDelay_AgentOverrideBelowFloorHonored — Rule #2 precedence.
// Floor fires only when post-override delay == 0. A 500ms override MUST be honored.
func TestChatDebounceDelay_AgentOverrideBelowFloorHonored(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.InboundDebounceMs = 0
	got := chatDebounceDelay(cfg, []byte(`{"inbound_debounce_ms":500}`), true)
	if got != 500*time.Millisecond {
		t.Fatalf("override 500ms with media = %s, want 500ms (floor must not raise)", got)
	}
}

// TestChatDebounceDelay_NoMediaZeroConfig: floor does NOT apply when no media.
func TestChatDebounceDelay_NoMediaZeroConfig(t *testing.T) {
	got := chatDebounceDelay(&config.Config{}, nil, false)
	if got != 0 {
		t.Fatalf("chatDebounceDelay(cfg=0, hasMedia=false) = %s, want 0", got)
	}
}

// TestChatDebounceDelay_MediaConfigAboveFloorUnchanged: cfg already above floor → unchanged.
func TestChatDebounceDelay_MediaConfigAboveFloorUnchanged(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.InboundDebounceMs = 2000
	got := chatDebounceDelay(cfg, nil, true)
	if got != 2000*time.Millisecond {
		t.Fatalf("chatDebounceDelay(cfg=2000, hasMedia=true) = %s, want 2s", got)
	}
}

// TestChatDebouncer_NoMediaFollowupMergesIntoBufferedMedia — Rule #4.
// When a follow-up Push arrives with delay==0 while a buffer exists for the key,
// it MUST merge into the buffer rather than dispatch immediately.
func TestChatDebouncer_NoMediaFollowupMergesIntoBufferedMedia(t *testing.T) {
	out := make(chan []chatSendRequest, 2)
	d := newChatDebouncer(func(items []chatSendRequest) {
		out <- items
	})
	defer d.Stop()

	// First push: media-bearing, 50ms window.
	d.Push("u1:s1", 50*time.Millisecond, chatSendRequest{params: chatSendParams{Message: "caption"}})
	// Second push: arrives while buffered, delay==0 (no media follow-up).
	time.Sleep(10 * time.Millisecond)
	d.Push("u1:s1", 0, chatSendRequest{params: chatSendParams{Message: "ps"}})

	items := waitChatDebounce(t, out)
	if len(items) != 2 {
		t.Fatalf("flushed items = %d, want 2 (follow-up must merge, not bypass)", len(items))
	}
	merged := mergeChatSendRequests(items).Message
	if merged != "caption\nps" {
		t.Fatalf("merged = %q, want %q", merged, "caption\nps")
	}

	assertNoChatDebounceFlush(t, out)
}

// TestMergeChatSendRequests_UnionsMediaAcrossItems — 42bucks fork patch
// (debounce-media-union). Every non-Message field of the merge is last-wins,
// which silently dropped attachments whenever a text follow-up landed inside
// the media debounce window (attachment-only send + quick typed text = file
// gone). Media MUST survive the merge.
func TestMergeChatSendRequests_UnionsMediaAcrossItems(t *testing.T) {
	items := []chatSendRequest{
		{params: chatSendParams{Message: "", Media: json.RawMessage(`[{"path":"/v1/files/a.pdf","filename":"a.pdf"}]`)}},
		{params: chatSendParams{Message: "hi"}},
	}
	merged := mergeChatSendRequests(items)
	media := merged.parseMedia()
	if len(media) != 1 || media[0].Path != "/v1/files/a.pdf" || media[0].Filename != "a.pdf" {
		t.Fatalf("media dropped in merge: %#v", media)
	}
	if merged.Message != "hi" {
		t.Fatalf("merged message = %q, want %q", merged.Message, "hi")
	}
}

// TestMergeChatSendRequests_UnionsLegacyAndNewFormats: legacy []string media and
// new [{path,filename}] media union chronologically, deduped by path.
func TestMergeChatSendRequests_UnionsLegacyAndNewFormats(t *testing.T) {
	items := []chatSendRequest{
		{params: chatSendParams{Media: json.RawMessage(`["/v1/files/x.png"]`)}},
		{params: chatSendParams{Media: json.RawMessage(`[{"path":"/v1/files/y.png","filename":"y.png"},{"path":"/v1/files/x.png"}]`)}},
	}
	merged := mergeChatSendRequests(items)
	media := merged.parseMedia()
	if len(media) != 2 {
		t.Fatalf("want 2 deduped items, got %#v", media)
	}
	if media[0].Path != "/v1/files/x.png" || media[1].Path != "/v1/files/y.png" {
		t.Fatalf("order/dedup wrong: %#v", media)
	}
	if media[1].Filename != "y.png" {
		t.Fatalf("filename lost: %#v", media[1])
	}
}

// TestParseMedia_LegacyFormatHasNoPhantomItem: unmarshalling legacy paths into
// []chatMediaItem records the FIRST element's type error but keeps decoding, so
// the failed parse leaves one zero-value element PER PATH ELEMENT — the legacy
// fallback must not append onto that partially-populated slice. Two elements
// distinguish per-element phantoms from a single leading one.
func TestParseMedia_LegacyFormatHasNoPhantomItem(t *testing.T) {
	p := chatSendParams{Media: json.RawMessage(`["/v1/files/x.png","/v1/files/y.png"]`)}
	items := p.parseMedia()
	if len(items) != 2 || items[0].Path != "/v1/files/x.png" || items[1].Path != "/v1/files/y.png" {
		t.Fatalf("legacy parse wrong: %#v", items)
	}
}

// TestMergeChatSendRequests_DupPathBackfillsFilenameAndDropsPathless: a
// duplicate path keeps its first (chronological) slot but gains a filename it
// was missing; items with no path at all never enter the union.
func TestMergeChatSendRequests_DupPathBackfillsFilenameAndDropsPathless(t *testing.T) {
	items := []chatSendRequest{
		{params: chatSendParams{Media: json.RawMessage(`["/v1/files/x.png"]`)}},
		{params: chatSendParams{Media: json.RawMessage(`[{"path":"/v1/files/x.png","filename":"invoice.png"},{"filename":"pathless.pdf"}]`)}},
	}
	merged := mergeChatSendRequests(items)
	media := merged.parseMedia()
	if len(media) != 1 {
		t.Fatalf("want 1 item (dup collapsed, pathless dropped), got %#v", media)
	}
	if media[0].Path != "/v1/files/x.png" || media[0].Filename != "invoice.png" {
		t.Fatalf("filename not backfilled on dup: %#v", media[0])
	}
}

// TestChatDebouncer_MediaSurvivesMergedFlush: end-to-end through the real
// debouncer path — a buffered media send followed by a delay-0 text follow-up
// must flush ONE dispatch whose merged params retain the attachment.
func TestChatDebouncer_MediaSurvivesMergedFlush(t *testing.T) {
	out := make(chan []chatSendRequest, 1)
	d := newChatDebouncer(func(items []chatSendRequest) { out <- items })
	d.Push("k", time.Hour, chatSendRequest{params: chatSendParams{Media: json.RawMessage(`[{"path":"/v1/files/a.pdf","filename":"a.pdf"}]`)}})
	d.Push("k", 0, chatSendRequest{params: chatSendParams{Message: "look at this"}})
	select {
	case items := <-out:
		merged := mergeChatSendRequests(items)
		media := merged.parseMedia()
		if merged.Message != "look at this" || len(media) != 1 || media[0].Path != "/v1/files/a.pdf" {
			t.Fatalf("media dropped through the debouncer flush: msg=%q media=%#v", merged.Message, media)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("merged flush never fired")
	}
}

// TestMergeChatSendRequests_NoMediaStaysNil: text-only merges keep Media nil so
// downstream hasMedia checks stay false.
func TestMergeChatSendRequests_NoMediaStaysNil(t *testing.T) {
	merged := mergeChatSendRequests([]chatSendRequest{
		{params: chatSendParams{Message: "a"}},
		{params: chatSendParams{Message: "b"}},
	})
	if merged.Media != nil {
		t.Fatalf("expected nil media, got %s", merged.Media)
	}
}

// TestChatDebouncer_DelayZeroNoBufferStillDispatches: delay==0 with empty buffer
// dispatches immediately (preserves existing behavior for plain text sends).
func TestChatDebouncer_DelayZeroNoBufferStillDispatches(t *testing.T) {
	out := make(chan []chatSendRequest, 1)
	d := newChatDebouncer(func(items []chatSendRequest) {
		out <- items
	})
	defer d.Stop()

	d.Push("u1:s1", 0, chatSendRequest{params: chatSendParams{Message: "one"}})
	items := waitChatDebounce(t, out)
	if len(items) != 1 || items[0].params.Message != "one" {
		t.Fatalf("dispatch = %#v, want single 'one'", items)
	}
}
