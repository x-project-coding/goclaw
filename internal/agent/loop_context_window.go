package agent

import (
	"context"
	"strconv"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// 42bucks fork patch: virtual compaction.
//
// sessions.messages is the ONLY store backing the chat UI transcript, and the
// summarizer used to TruncateHistory it in place — the 2026-07-20 incident
// collapsed a 5-week production conversation to its last 4 messages. The
// transcript is now append-only: compaction advances a per-session window
// pointer (store.SessionMetaContextStartIndex) and everything that feeds the
// MODEL reads messages[start:] + summary, while everything that feeds a
// READER (chat.history, /history/follow, sessions.preview) keeps the full
// array.

// maxCarriedMediaRefs caps how many pre-window media refs ride along into the
// window so the model can still reference recently shared files (mirrors the
// cap the old destructive path applied when re-injecting refs on truncation).
const maxCarriedMediaRefs = 30

// activeHistoryWindow returns the session's LLM-facing message window and its
// start offset within the full transcript.
func (l *Loop) activeHistoryWindow(ctx context.Context, sessionKey string) ([]providers.Message, int) {
	history := l.sessions.GetHistory(ctx, sessionKey)
	start := store.ContextStartIndex(l.sessions.GetSessionMetadata(ctx, sessionKey), len(history))
	return history[start:], start
}

// setContextStartIndex advances the session's window pointer.
func (l *Loop) setContextStartIndex(ctx context.Context, sessionKey string, start int) {
	l.sessions.SetSessionMetadata(ctx, sessionKey, map[string]string{
		store.SessionMetaContextStartIndex: strconv.Itoa(start),
	})
}

// carryRecentMediaRefs makes up to maxCarriedMediaRefs of the newest
// pre-window media refs visible to the model by prepending them to a CLONE of
// the window's first message. In-memory only — mutating the persisted row
// would make the UI sprout stale attachments on a random message.
func carryRecentMediaRefs(window, preWindow []providers.Message) []providers.Message {
	if len(window) == 0 || len(preWindow) == 0 {
		return window
	}
	var carried []providers.MediaRef
	for i := len(preWindow) - 1; i >= 0 && len(carried) < maxCarriedMediaRefs; i-- {
		for _, ref := range preWindow[i].MediaRefs {
			carried = append(carried, ref)
			if len(carried) >= maxCarriedMediaRefs {
				break
			}
		}
	}
	if len(carried) == 0 {
		return window
	}
	out := make([]providers.Message, len(window))
	copy(out, window)
	first := out[0]
	refs := make([]providers.MediaRef, 0, len(carried)+len(first.MediaRefs))
	refs = append(refs, carried...)
	refs = append(refs, first.MediaRefs...)
	if len(refs) > maxCarriedMediaRefs {
		refs = refs[:maxCarriedMediaRefs]
	}
	first.MediaRefs = refs
	out[0] = first
	return out
}
