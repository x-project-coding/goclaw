package agent

import (
	"context"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// recordingSessionStore captures AddMessage calls for flush assertions.
type recordingSessionStore struct {
	nopSessionStore
	added []providers.Message
}

func (r *recordingSessionStore) AddMessage(_ context.Context, _ string, msg providers.Message) {
	r.added = append(r.added, msg)
}

// ─── makeFlushMessages: user message receipt timestamp ────────────────────

func TestMakeFlushMessages_UserMessageKeepsReceiptTimestamp(t *testing.T) {
	rec := &recordingSessionStore{}
	l := &Loop{sessions: rec}
	receivedAt := time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC)
	req := &RunRequest{
		MessageID:        "msg-1",
		MessageCreatedAt: receivedAt,
		Message:          "hello",
		SenderID:         "u1",
		SenderName:       "Alice",
	}

	flush := l.makeFlushMessages(l.newUserMessageFlusher(req))
	// Simulate a flush that happens long after receipt (checkpoint/finalize).
	time.Sleep(5 * time.Millisecond)
	if err := flush(context.Background(), "sess-1", nil); err != nil {
		t.Fatalf("flush returned error: %v", err)
	}

	if len(rec.added) != 1 {
		t.Fatalf("AddMessage called %d times, want 1", len(rec.added))
	}
	got := rec.added[0]
	if got.CreatedAt == nil {
		t.Fatal("user message CreatedAt is nil, want receipt timestamp")
	}
	if !got.CreatedAt.Equal(receivedAt) {
		t.Errorf("user message CreatedAt = %v, want receipt time %v (flush-time stamping regression)",
			got.CreatedAt, receivedAt)
	}
	if got.ID != "msg-1" || got.Role != "user" || got.Content != "hello" {
		t.Errorf("user message identity wrong: %+v", got)
	}
	if got.SenderID != "u1" || got.SenderName != "Alice" {
		t.Errorf("sender identity lost: SenderID=%q SenderName=%q", got.SenderID, got.SenderName)
	}
}

func TestMakeFlushMessages_FallsBackToRunStartWhenUnset(t *testing.T) {
	rec := &recordingSessionStore{}
	l := &Loop{sessions: rec}
	req := &RunRequest{Message: "hi"}

	before := time.Now().UTC()
	flush := l.makeFlushMessages(l.newUserMessageFlusher(req))
	after := time.Now().UTC()

	// Flush later — the stamp must come from closure creation (run start), not flush time.
	time.Sleep(5 * time.Millisecond)
	if err := flush(context.Background(), "sess-1", nil); err != nil {
		t.Fatalf("flush returned error: %v", err)
	}

	if len(rec.added) != 1 {
		t.Fatalf("AddMessage called %d times, want 1", len(rec.added))
	}
	got := rec.added[0]
	if got.CreatedAt == nil {
		t.Fatal("user message CreatedAt is nil, want run-start fallback")
	}
	if got.CreatedAt.Before(before) || got.CreatedAt.After(after) {
		t.Errorf("CreatedAt = %v, want within run-start window [%v, %v]", got.CreatedAt, before, after)
	}
}

func TestMakeFlushMessages_UserMessagePersistedOnceAndPendingPassThrough(t *testing.T) {
	rec := &recordingSessionStore{}
	l := &Loop{sessions: rec}
	receivedAt := time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC)
	req := &RunRequest{Message: "hello", MessageCreatedAt: receivedAt}

	flush := l.makeFlushMessages(l.newUserMessageFlusher(req))

	pendingAt := time.Date(2026, 6, 30, 12, 5, 0, 0, time.UTC)
	pending := []providers.Message{{Role: "assistant", Content: "working...", CreatedAt: &pendingAt}}
	if err := flush(context.Background(), "sess-1", pending); err != nil {
		t.Fatalf("first flush returned error: %v", err)
	}
	if err := flush(context.Background(), "sess-1", nil); err != nil {
		t.Fatalf("second flush returned error: %v", err)
	}

	// user message once + one pending message
	if len(rec.added) != 2 {
		t.Fatalf("AddMessage called %d times, want 2 (user once + 1 pending)", len(rec.added))
	}
	if rec.added[0].Role != "user" || rec.added[1].Role != "assistant" {
		t.Errorf("flush order wrong: roles %q, %q", rec.added[0].Role, rec.added[1].Role)
	}
	if rec.added[1].CreatedAt == nil || !rec.added[1].CreatedAt.Equal(pendingAt) {
		t.Errorf("pending message CreatedAt = %v, want preserved %v", rec.added[1].CreatedAt, pendingAt)
	}
}

// ─── injectedSessionMessage: identity + arrival time propagation ──────────

func TestInjectedSessionMessage_CarriesIdentityAndTimestamp(t *testing.T) {
	arrivedAt := time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC)
	got := injectedSessionMessage(InjectedMessage{
		MessageID:  "msg-42",
		Content:    "follow-up",
		UserID:     "group:ws:chat-1",
		SenderID:   "u1",
		SenderName: "Alice",
		CreatedAt:  arrivedAt,
	})

	if got.ID != "msg-42" {
		t.Errorf("ID = %q, want 'msg-42'", got.ID)
	}
	if got.Role != "user" || got.Content != "follow-up" {
		t.Errorf("Role/Content wrong: %+v", got)
	}
	if got.SenderID != "u1" || got.SenderName != "Alice" {
		t.Errorf("sender identity lost: SenderID=%q SenderName=%q", got.SenderID, got.SenderName)
	}
	if got.CreatedAt == nil || !got.CreatedAt.Equal(arrivedAt) {
		t.Errorf("CreatedAt = %v, want arrival time %v", got.CreatedAt, arrivedAt)
	}
}

func TestInjectedSessionMessage_ZeroCreatedAtStaysNil(t *testing.T) {
	got := injectedSessionMessage(InjectedMessage{Content: "hi"})
	if got.CreatedAt != nil {
		t.Errorf("CreatedAt = %v, want nil for zero arrival time (store stamps fallback)", got.CreatedAt)
	}
}

// ─── Router.InjectMessage: arrival stamping through the channel ───────────

func TestRouterInjectMessage_StampsArrivalTime(t *testing.T) {
	r := NewRouter()
	ch := r.RegisterRun(context.Background(), "run-1", "sess-1", "agent-1", func() {})
	defer r.UnregisterRun("run-1")

	before := time.Now().UTC()
	if !r.InjectMessage("sess-1", InjectedMessage{Content: "hi", SenderID: "u1"}) {
		t.Fatal("InjectMessage returned false, want true")
	}
	after := time.Now().UTC()

	injected := <-ch
	if injected.CreatedAt.IsZero() {
		t.Fatal("injected CreatedAt is zero, want arrival stamp")
	}
	if injected.CreatedAt.Before(before) || injected.CreatedAt.After(after) {
		t.Errorf("CreatedAt = %v, want within injection window [%v, %v]", injected.CreatedAt, before, after)
	}

	// Drain→session conversion preserves the stamped arrival time + identity.
	msg := injectedSessionMessage(injected)
	if msg.CreatedAt == nil || !msg.CreatedAt.Equal(injected.CreatedAt) {
		t.Errorf("session message CreatedAt = %v, want %v", msg.CreatedAt, injected.CreatedAt)
	}
	if msg.SenderID != "u1" {
		t.Errorf("session message SenderID = %q, want 'u1'", msg.SenderID)
	}
}

func TestRouterInjectMessage_PreservesCallerTimestamp(t *testing.T) {
	r := NewRouter()
	ch := r.RegisterRun(context.Background(), "run-1", "sess-1", "agent-1", func() {})
	defer r.UnregisterRun("run-1")

	receivedAt := time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC)
	if !r.InjectMessage("sess-1", InjectedMessage{Content: "hi", CreatedAt: receivedAt}) {
		t.Fatal("InjectMessage returned false, want true")
	}

	injected := <-ch
	if !injected.CreatedAt.Equal(receivedAt) {
		t.Errorf("CreatedAt = %v, want caller receipt time %v", injected.CreatedAt, receivedAt)
	}
}
