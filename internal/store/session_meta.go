package store

import "strconv"

// SessionMetaContextStartIndex marks where a session's ACTIVE context window
// begins inside the persisted messages array. Compaction is virtual: instead
// of truncating the transcript (which also backs every chat UI — the
// 2026-07-20 incident collapsed a 5-week conversation to 4 messages), the
// summarizer advances this pointer and the LLM context is assembled from
// messages[start:] plus the rolling summary. Stored in session metadata —
// the same pattern as last_prompt_tokens — so no schema migration is needed
// and older binaries ignore it safely.
const SessionMetaContextStartIndex = "context_start_index"

// ContextStartIndex parses the window start from session metadata, clamped
// to [0, historyLen]. Absent or unparseable values mean "window is the whole
// transcript" (uncompacted session).
func ContextStartIndex(meta map[string]string, historyLen int) int {
	raw, ok := meta[SessionMetaContextStartIndex]
	if !ok {
		return 0
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return 0
	}
	if n > historyLen {
		return historyLen
	}
	return n
}

// NextContextStartIndex computes where the window should start after a
// compaction that keeps the last keepLast window messages: clamped to
// [current, historyLen] so a compaction can never move the pointer BACKWARD
// (re-exposing already-summarized messages — or, with a large caller-supplied
// keepLast, going negative and un-compacting the whole transcript).
func NextContextStartIndex(current, historyLen, keepLast int) int {
	next := historyLen - keepLast
	if next < current {
		next = current
	}
	if next > historyLen {
		next = historyLen
	}
	if next < 0 {
		next = 0
	}
	return next
}
