package pipeline

import (
	"context"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// ─── MessageBuffer.AppendPending: emission-time stamping ──────────────────

func TestMessageBuffer_AppendPending_StampsCreatedAt(t *testing.T) {
	t.Parallel()
	mb := NewMessageBuffer(providers.Message{Role: "system", Content: "s"})

	before := time.Now().UTC()
	mb.AppendPending(providers.Message{Role: "assistant", Content: "a"})
	after := time.Now().UTC()

	pending := mb.Pending()
	if len(pending) != 1 {
		t.Fatalf("Pending() len = %d, want 1", len(pending))
	}
	got := pending[0].CreatedAt
	if got == nil {
		t.Fatal("CreatedAt is nil, want emission-time stamp")
	}
	if got.Before(before) || got.After(after) {
		t.Errorf("CreatedAt = %v, want within append window [%v, %v]", got, before, after)
	}
}

func TestMessageBuffer_AppendPending_PreservesExistingCreatedAt(t *testing.T) {
	t.Parallel()
	mb := NewMessageBuffer(providers.Message{Role: "system", Content: "s"})

	receivedAt := time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC)
	mb.AppendPending(providers.Message{Role: "user", Content: "a", CreatedAt: &receivedAt})

	got := mb.Pending()[0].CreatedAt
	if got == nil || !got.Equal(receivedAt) {
		t.Errorf("CreatedAt = %v, want preserved %v", got, receivedAt)
	}
}

// ─── ObserveStage: injected message survives drain → pending → flush ──────

func TestObserveStage_InjectedMessagePreservedThroughFlush(t *testing.T) {
	t.Parallel()

	arrivedAt := time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC)
	deps := &PipelineDeps{
		DrainInjectCh: func() []providers.Message {
			return []providers.Message{{
				ID:         "msg-42",
				Role:       "user",
				Content:    "follow-up",
				SenderID:   "u1",
				SenderName: "Alice",
				CreatedAt:  &arrivedAt,
			}}
		},
	}
	state := defaultState()

	if err := NewObserveStage(deps).Execute(context.Background(), state); err != nil {
		t.Fatalf("ObserveStage.Execute returned error: %v", err)
	}

	flushed := state.Messages.FlushPending()
	if len(flushed) != 1 {
		t.Fatalf("FlushPending returned %d messages, want 1", len(flushed))
	}
	got := flushed[0]
	if got.ID != "msg-42" || got.SenderID != "u1" || got.SenderName != "Alice" {
		t.Errorf("injected identity lost through drain→flush: %+v", got)
	}
	if got.CreatedAt == nil || !got.CreatedAt.Equal(arrivedAt) {
		t.Errorf("injected CreatedAt = %v, want arrival time %v", got.CreatedAt, arrivedAt)
	}
}
