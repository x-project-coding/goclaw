package agent

import (
	"context"
	"sync"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// 42bucks fork patch: deterministic transcripts for cancelled runs.
//
// goclaw only flushes a run's messages at checkpoint/finalize, so a run
// cancelled mid-stage used to leave NO trace: the user's message was never
// persisted and the chat client's optimistic bubble could never be confirmed
// by any history snapshot — the "my last request shows again at the end,
// unanswered" phantom (reported + live-reproduced 2026-07-20). The cancel
// path now persists the user message and closes the turn with a stop marker.

// abortedTurnMarker closes a cancelled turn on the assistant side. Without it
// the dangling user row would break user/assistant alternation and
// sanitizeHistory would merge it into the NEXT user message on the following
// run. It also tells both the reader and the model that the request was
// deliberately stopped.
const abortedTurnMarker = "[Stopped by user]"

// userMessageFlusher persists the run's user message exactly once, whether
// the run reaches a real flush (checkpoint/finalize) or is cancelled first.
// One instance per run, shared by makeFlushMessages and the cancel path.
type userMessageFlusher struct {
	mu        sync.Mutex
	flushed   bool
	loop      *Loop
	req       *RunRequest
	createdAt time.Time
}

func (l *Loop) newUserMessageFlusher(req *RunRequest) *userMessageFlusher {
	// Stamp the user message with its receipt time so created_at reflects
	// when the user sent it, not when the flush landed (same rule as the
	// receipt-timestamp patch in makeFlushMessages).
	createdAt := req.MessageCreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	return &userMessageFlusher{loop: l, req: req, createdAt: createdAt}
}

// flushIfNeeded persists the user message if no flush has done so yet.
// Returns true when THIS call performed the persist.
func (f *userMessageFlusher) flushIfNeeded(ctx context.Context, sessionKey string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.flushed || f.req.HideInput || f.req.Message == "" {
		return false
	}
	f.flushed = true
	f.loop.sessions.AddMessage(ctx, sessionKey, providers.Message{
		ID:         f.req.MessageID,
		Role:       "user",
		Content:    f.req.Message,
		SenderID:   f.req.SenderID,
		SenderName: f.req.SenderName,
		CreatedAt:  &f.createdAt,
	})
	return true
}

// persistAbortedTurn makes a cancelled run leave an honest transcript. When
// no flush ran before the cancellation, it persists the user message and a
// stop marker; when a checkpoint/finalize flush already persisted the turn
// (user message plus whatever partial output existed), it does nothing.
// Callers pass a context that survives the cancellation
// (context.WithoutCancel).
func (l *Loop) persistAbortedTurn(ctx context.Context, sessionKey string, f *userMessageFlusher) {
	if !f.flushIfNeeded(ctx, sessionKey) {
		return
	}
	now := time.Now().UTC()
	l.sessions.AddMessage(ctx, sessionKey, providers.Message{
		Role:      "assistant",
		Content:   abortedTurnMarker,
		CreatedAt: &now,
	})
	l.sessions.Save(ctx, sessionKey)
}
