package agent

import (
	"context"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// savingSessionStore also counts Save calls — the aborted-turn persist must
// write through to the DB, not just the cache.
type savingSessionStore struct {
	recordingSessionStore
	saves int
}

func (s *savingSessionStore) Save(_ context.Context, _ string) error {
	s.saves++
	return nil
}

// ─── persistAbortedTurn: a cancelled run leaves an honest transcript ────────
//
// goclaw only flushes a run's messages at checkpoint/finalize, so a run
// cancelled mid-stage used to vanish from the transcript entirely: the user's
// message was never persisted, the UI's optimistic bubble could never be
// confirmed, and the chat client rendered the "my last request shows again at
// the end, unanswered" phantom (reported + live-reproduced 2026-07-20).

func TestPersistAbortedTurn_PersistsUserMessageAndStopMarker(t *testing.T) {
	rec := &savingSessionStore{}
	l := &Loop{sessions: rec}
	req := &RunRequest{MessageID: "msg-1", Message: "do the thing", SenderID: "u1", SenderName: "Alice"}

	f := l.newUserMessageFlusher(req)
	l.persistAbortedTurn(context.Background(), "sess-1", f)

	if len(rec.added) != 2 {
		t.Fatalf("AddMessage called %d times, want 2 (user + stop marker)", len(rec.added))
	}
	user := rec.added[0]
	if user.Role != "user" || user.Content != "do the thing" || user.ID != "msg-1" {
		t.Errorf("user message wrong: %+v", user)
	}
	if user.SenderID != "u1" || user.SenderName != "Alice" {
		t.Errorf("sender identity lost: %+v", user)
	}
	marker := rec.added[1]
	if marker.Role != "assistant" || marker.Content != abortedTurnMarker {
		t.Errorf("stop marker wrong: %+v", marker)
	}
	if marker.CreatedAt == nil {
		t.Error("stop marker CreatedAt is nil, want stamped")
	}
	if rec.saves == 0 {
		t.Error("session was not saved — aborted turn stays cache-only")
	}
}

func TestPersistAbortedTurn_NoOpWhenARunFlushAlreadyPersisted(t *testing.T) {
	rec := &savingSessionStore{}
	l := &Loop{sessions: rec}
	req := &RunRequest{Message: "long job"}

	f := l.newUserMessageFlusher(req)
	// A checkpoint/finalize flush already persisted the user message (and
	// whatever partial output existed) — the turn is closed by real rows, so
	// the cancel path must not add a marker or a duplicate user message.
	flush := l.makeFlushMessages(f)
	if err := flush(context.Background(), "sess-1", []providers.Message{{Role: "assistant", Content: "partial"}}); err != nil {
		t.Fatalf("flush returned error: %v", err)
	}
	addedBefore := len(rec.added)

	l.persistAbortedTurn(context.Background(), "sess-1", f)

	if len(rec.added) != addedBefore {
		t.Fatalf("persistAbortedTurn added %d messages after a real flush, want 0",
			len(rec.added)-addedBefore)
	}
}

func TestPersistAbortedTurn_NoOpForHiddenOrEmptyInput(t *testing.T) {
	for _, req := range []*RunRequest{
		{Message: "secret", HideInput: true},
		{Message: ""},
	} {
		rec := &savingSessionStore{}
		l := &Loop{sessions: rec}
		f := l.newUserMessageFlusher(req)

		l.persistAbortedTurn(context.Background(), "sess-1", f)

		if len(rec.added) != 0 {
			t.Errorf("HideInput=%v Message=%q: AddMessage called %d times, want 0",
				req.HideInput, req.Message, len(rec.added))
		}
	}
}

func TestMakeFlushMessages_SkipsUserMessageAfterAbortPersist(t *testing.T) {
	rec := &savingSessionStore{}
	l := &Loop{sessions: rec}
	req := &RunRequest{Message: "do the thing"}

	f := l.newUserMessageFlusher(req)
	// Pure once-guard property: however the persist paths are ordered, the
	// user message lands exactly once.
	l.persistAbortedTurn(context.Background(), "sess-1", f)
	flush := l.makeFlushMessages(f)
	if err := flush(context.Background(), "sess-1", nil); err != nil {
		t.Fatalf("flush returned error: %v", err)
	}

	users := 0
	for _, m := range rec.added {
		if m.Role == "user" {
			users++
		}
	}
	if users != 1 {
		t.Fatalf("user message persisted %d times, want exactly 1", users)
	}
}

func TestSanitizeHistory_AbortedTurnDoesNotMergeIntoNextRequest(t *testing.T) {
	// The stop marker exists precisely so the dangling user row of a
	// cancelled run keeps user/assistant alternation: without it,
	// sanitizeHistory's consecutive-same-role merge would fold the aborted
	// request into the NEXT user message ("doomed\n\nreal") and the UI would
	// render them as one bubble.
	history := []providers.Message{
		{Role: "user", Content: "doomed question"},
		{Role: "assistant", Content: abortedTurnMarker},
		{Role: "user", Content: "real question"},
	}

	sanitized, dropped := sanitizeHistory(history)

	if dropped != 0 {
		t.Errorf("sanitizeHistory dropped/merged %d messages, want 0", dropped)
	}
	if len(sanitized) != 3 {
		t.Fatalf("sanitized to %d messages, want 3: %+v", len(sanitized), sanitized)
	}
	if sanitized[2].Content != "real question" {
		t.Errorf("next request mutated: %q", sanitized[2].Content)
	}
}

func TestRunAbortedByUser_DistinguishesUserAbortsFromOtherCancellations(t *testing.T) {
	// User abort: cancelled with the router's sentinel cause.
	userCtx, userCancel := context.WithCancelCause(context.Background())
	userCancel(ErrRunAbortedByUser)
	if !runAbortedByUser(userCtx) {
		t.Error("user-abort cause not recognized")
	}

	// Deadline (cron JobTimeout, delegate/webhook timeouts): NOT a user abort.
	deadlineCtx, cancel := context.WithTimeout(context.Background(), 0)
	defer cancel()
	<-deadlineCtx.Done()
	if runAbortedByUser(deadlineCtx) {
		t.Error("deadline expiry misclassified as user abort — would write a false '[Stopped by user]' marker")
	}

	// Plain cancellation (client disconnect, shutdown): NOT a user abort.
	plainCtx, plainCancel := context.WithCancel(context.Background())
	plainCancel()
	if runAbortedByUser(plainCtx) {
		t.Error("plain cancellation misclassified as user abort")
	}

	// Live context: not aborted at all.
	if runAbortedByUser(context.Background()) {
		t.Error("live context misclassified as user abort")
	}
}

func TestAbortRun_CancelsWithUserAbortCause(t *testing.T) {
	r := NewRouter()
	runCtx, cancel := context.WithCancelCause(context.Background())
	r.RegisterRun(context.Background(), "run-abort-1", "sess-1", "agent-1", cancel)
	defer r.UnregisterRun("run-abort-1")

	go func() {
		// Let AbortRun's grace wait observe the goroutine finishing.
		<-runCtx.Done()
		r.UnregisterRun("run-abort-1")
	}()
	res := r.AbortRun("run-abort-1", "sess-1")
	if res.NotFound || res.Unauthorized {
		t.Fatalf("unexpected abort result: %+v", res)
	}
	if !runAbortedByUser(runCtx) {
		t.Error("AbortRun did not cancel with ErrRunAbortedByUser cause")
	}
}

func TestPersistAbortedTurn_SavesEvenWhenAlreadyFlushed(t *testing.T) {
	rec := &savingSessionStore{}
	l := &Loop{sessions: rec}
	req := &RunRequest{Message: "long job"}

	f := l.newUserMessageFlusher(req)
	// A checkpoint flush persisted the turn into the CACHE (AddMessage only)…
	flush := l.makeFlushMessages(f)
	if err := flush(context.Background(), "sess-1", []providers.Message{{Role: "assistant", Content: "partial"}}); err != nil {
		t.Fatalf("flush returned error: %v", err)
	}
	// …and finalize never ran (mid-stage cancel), so nothing has Saved yet.
	l.persistAbortedTurn(context.Background(), "sess-1", f)

	if rec.saves == 0 {
		t.Error("persistAbortedTurn skipped Save for a checkpoint-flushed turn — aborted rows stay cache-only and vanish on restart")
	}
}
