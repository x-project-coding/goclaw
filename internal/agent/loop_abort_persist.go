package agent

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// 42bucks fork patch: deterministic transcripts for user-cancelled runs.
//
// goclaw only flushes a run's messages at checkpoint/finalize, so a run
// cancelled mid-stage used to leave NO trace: the user's message was never
// persisted and the chat client's optimistic bubble could never be confirmed
// by any history snapshot — the "my last request shows again at the end,
// unanswered" phantom (reported + live-reproduced 2026-07-20). The cancel
// path now persists the user message and closes the turn with a stop marker.

// ErrRunAbortedByUser is the cancellation cause Router.AbortRun attaches when
// a user stops a run (chat.abort RPC, the cancel keyword, session aborts).
// It is what distinguishes a deliberate stop from every other way a run
// context dies — cron/delegate/webhook timeouts, client disconnects, process
// shutdown — which must NOT persist a stop marker.
var ErrRunAbortedByUser = errors.New("run aborted by user")

// runAbortedByUser reports whether ctx died specifically because a user
// aborted the run.
func runAbortedByUser(ctx context.Context) bool {
	return ctx.Err() != nil && errors.Is(context.Cause(ctx), ErrRunAbortedByUser)
}

// abortedTurnMarker closes a user-cancelled turn on the assistant side.
// Without it the dangling user row would break user/assistant alternation and
// sanitizeHistory would merge it into the NEXT user message on the following
// run. It also tells both the reader and the model that the request was
// deliberately stopped.
//
// Deliberately NOT localized: like "[Tool result missing — session was
// compacted]" and the pruning placeholders, this is transcript-protocol
// content — persisted once and replayed to the model on every later turn, so
// it must stay byte-stable regardless of the locale the session happens to
// carry at cancel time. Clients that want localized presentation can key on
// the literal.
const abortedTurnMarker = "[Stopped by user]"

// userMessageFlusher persists the run's user message exactly once, whether
// the run reaches a real flush (checkpoint/finalize) or is user-cancelled
// first. One instance per run, used by makeFlushMessages and the cancel
// path — all strictly sequential on the run goroutine (checkpoint and
// finalize flushes happen inside pipeline.Run; persistAbortedTurn only after
// it returns), so a plain bool suffices.
type userMessageFlusher struct {
	flushed   bool
	loop      *Loop
	req       *RunRequest
	createdAt time.Time
}

func (l *Loop) newUserMessageFlusher(req *RunRequest) *userMessageFlusher {
	// Stamp the user message with its receipt time so created_at reflects
	// when the user sent it, not when the flush landed — the first flush can
	// land many minutes later on long tool loops. Callers that don't set
	// MessageCreatedAt fall back to run start, still far closer to arrival
	// than flush time.
	createdAt := req.MessageCreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	return &userMessageFlusher{loop: l, req: req, createdAt: createdAt}
}

// flushIfNeeded persists the user message if no flush has done so yet.
// Returns true when THIS call performed the persist.
func (f *userMessageFlusher) flushIfNeeded(ctx context.Context, sessionKey string) bool {
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

// persistAbortedTurn makes a user-cancelled run leave an honest transcript.
// When no flush ran before the cancellation, it persists the user message
// and a stop marker; when a checkpoint flush already persisted the turn
// (user message plus whatever partial output existed), only the write-through
// Save is still needed — AddMessage appends to the in-process cache and the
// finalize that normally Saves never runs on the cancelled path. Callers pass
// a context that survives the cancellation (context.WithoutCancel).
func (l *Loop) persistAbortedTurn(ctx context.Context, sessionKey string, f *userMessageFlusher) {
	flushedHere := f.flushIfNeeded(ctx, sessionKey)
	if flushedHere {
		now := time.Now().UTC()
		l.sessions.AddMessage(ctx, sessionKey, providers.Message{
			Role:      "assistant",
			Content:   abortedTurnMarker,
			CreatedAt: &now,
		})
	} else if f.req.HideInput || f.req.Message == "" {
		// Nothing was or will be persisted for this run — don't create a
		// session row for hidden/announce runs.
		return
	}
	if err := l.sessions.Save(ctx, sessionKey); err != nil {
		slog.Warn("abort persist: session save failed — aborted turn stays cache-only",
			"session", sessionKey, "error", err)
	}
}
