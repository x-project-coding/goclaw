package agent

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// virtualCompactionStore extends nopSessionStore with mutable summary,
// metadata, and recorders — enough to observe what compaction persists.
type virtualCompactionStore struct {
	nopSessionStore
	mu          sync.Mutex
	summary     string
	meta        map[string]string
	truncated   bool
	setHistory  [][]providers.Message
	compactions int
	saves       int
}

func (v *virtualCompactionStore) GetSummary(_ context.Context, _ string) string {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.summary
}
func (v *virtualCompactionStore) SetSummary(_ context.Context, _, s string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.summary = s
}
func (v *virtualCompactionStore) GetSessionMetadata(_ context.Context, _ string) map[string]string {
	v.mu.Lock()
	defer v.mu.Unlock()
	out := make(map[string]string, len(v.meta))
	for k, val := range v.meta {
		out[k] = val
	}
	return out
}
func (v *virtualCompactionStore) SetSessionMetadata(_ context.Context, _ string, m map[string]string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.meta == nil {
		v.meta = make(map[string]string)
	}
	for k, val := range m {
		v.meta[k] = val
	}
}
func (v *virtualCompactionStore) TruncateHistory(_ context.Context, _ string, keepLast int) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.truncated = true
	if len(v.history) > keepLast {
		v.history = v.history[len(v.history)-keepLast:]
	}
}
func (v *virtualCompactionStore) SetHistory(_ context.Context, _ string, msgs []providers.Message) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.setHistory = append(v.setHistory, msgs)
	v.history = msgs
}
func (v *virtualCompactionStore) IncrementCompaction(_ context.Context, _ string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.compactions++
}
func (v *virtualCompactionStore) Save(_ context.Context, _ string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.saves++
	return nil
}
func (v *virtualCompactionStore) historyLen() int {
	v.mu.Lock()
	defer v.mu.Unlock()
	return len(v.history)
}
func (v *virtualCompactionStore) metaStart() string {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.meta[store.SessionMetaContextStartIndex]
}

func overThresholdHistory(n int) []providers.Message {
	longContent := makeLongString(9000)
	history := make([]providers.Message, n)
	for i := range history {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		history[i] = providers.Message{Role: role, Content: longContent + " #" + strconv.Itoa(i)}
	}
	return history
}

func summarizeLoop(sessions store.SessionStore, sp providers.Provider) *Loop {
	return &Loop{
		provider:      sp,
		model:         "claude-3-5-sonnet",
		contextWindow: 10000,
		sessions:      sessions,
		hasMemory:     false,
		compactionCfg: nil, // DefaultHistoryShare 0.85, keepLast 4
	}
}

// ─── Virtual compaction: the transcript is never destroyed ─────────────────
//
// Production incident 2026-07-20: post-turn compaction TruncateHistory'd the
// sessions.messages array — the ONLY store backing the chat UI transcript —
// collapsing a 5-week conversation to its last 4 messages (~90% of the
// visible history gone, unrecoverable outside nightly backups). Compaction
// must bound the LLM context WINDOW, not the persisted transcript.

func TestMaybeSummarize_PreservesTranscriptAndSetsWindowStart(t *testing.T) {
	history := overThresholdHistory(10)
	done := make(chan struct{}, 1)
	sp := &signallingProvider{capturingProvider: capturingProvider{response: "summary text"}, done: done}
	sessions := &virtualCompactionStore{nopSessionStore: nopSessionStore{history: history}}
	loop := summarizeLoop(sessions, sp)

	loop.maybeSummarize(context.Background(), "sess-vc")
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for summarize call")
	}
	// The pointer write happens after the LLM call returns — poll briefly.
	deadline := time.Now().Add(3 * time.Second)
	for sessions.metaStart() == "" && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	if sessions.truncated {
		t.Error("TruncateHistory was called — compaction destroyed the persisted transcript")
	}
	if got := sessions.historyLen(); got != len(history) {
		t.Errorf("persisted history has %d messages after compaction, want %d (untouched)", got, len(history))
	}
	// keepLast=4 → window starts at len-4.
	if got, want := sessions.metaStart(), strconv.Itoa(len(history)-4); got != want {
		t.Errorf("context start index = %q, want %q", got, want)
	}
	sessions.mu.Lock()
	defer sessions.mu.Unlock()
	if sessions.summary != "summary text" {
		t.Errorf("summary = %q, want the LLM response", sessions.summary)
	}
	if sessions.compactions != 1 {
		t.Errorf("compaction count increments = %d, want 1", sessions.compactions)
	}
	if sessions.saves == 0 {
		t.Error("session was not saved")
	}
}

func TestMaybeSummarize_WindowedEstimateDoesNotRetrigger(t *testing.T) {
	// Already-compacted session: full array is over threshold but the ACTIVE
	// window (last 4 messages) is far below it. Compaction must not fire
	// again — with a transcript that never shrinks, estimating over the full
	// array would re-summarize on every turn forever.
	history := overThresholdHistory(10)
	done := make(chan struct{}, 1)
	sp := &signallingProvider{capturingProvider: capturingProvider{response: "should not be called"}, done: done}
	sessions := &virtualCompactionStore{
		nopSessionStore: nopSessionStore{history: history},
		meta:            map[string]string{store.SessionMetaContextStartIndex: "6"},
	}
	loop := summarizeLoop(sessions, sp)

	loop.maybeSummarize(context.Background(), "sess-vc2")

	select {
	case <-done:
		t.Fatal("summarize LLM call fired for an already-windowed session — infinite re-compaction")
	case <-time.After(300 * time.Millisecond):
	}
	if sessions.metaStart() != "6" {
		t.Errorf("context start index changed to %q, want unchanged \"6\"", sessions.metaStart())
	}
}

func TestMaybeSummarize_SummarizesOnlyTheWindow(t *testing.T) {
	// Second compaction of a windowed session must summarize only the window
	// (pre-window content is already in the rolling summary).
	history := overThresholdHistory(16)
	done := make(chan struct{}, 1)
	sp := &signallingProvider{capturingProvider: capturingProvider{response: "rolling summary"}, done: done}
	sessions := &virtualCompactionStore{
		nopSessionStore: nopSessionStore{history: history},
		meta:            map[string]string{store.SessionMetaContextStartIndex: "6"},
		summary:         "earlier summary",
	}
	loop := summarizeLoop(sessions, sp)

	loop.maybeSummarize(context.Background(), "sess-vc3")
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for summarize call")
	}

	req := sp.captured[0]
	prompt := req.Messages[0].Content
	if strings.Contains(prompt, "#3") {
		t.Error("summarize prompt contains pre-window message #3 — must summarize only the active window")
	}
	if !strings.Contains(prompt, "#7") {
		t.Error("summarize prompt is missing window message #7")
	}
	if !strings.Contains(prompt, "earlier summary") {
		t.Error("summarize prompt is missing the existing rolling summary")
	}

	deadline := time.Now().Add(3 * time.Second)
	for sessions.metaStart() == "6" && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	// window = 16-6 = 10 msgs, keepLast 4 → new start = 6 + 10 - 4 = 12.
	if got := sessions.metaStart(); got != "12" {
		t.Errorf("context start index = %q, want \"12\"", got)
	}
	if got := sessions.historyLen(); got != len(history) {
		t.Errorf("persisted history has %d messages, want %d (untouched)", got, len(history))
	}
}

// ─── LLM context assembly reads the window, the wire keeps the transcript ──

func TestLoadSessionHistory_ReturnsActiveWindow(t *testing.T) {
	history := overThresholdHistory(10)
	sessions := &virtualCompactionStore{
		nopSessionStore: nopSessionStore{history: history},
		meta:            map[string]string{store.SessionMetaContextStartIndex: "6"},
		summary:         "the summary",
	}
	l := &Loop{sessions: sessions}

	got, summary := l.makeLoadSessionHistory()(context.Background(), "sess-w")

	if len(got) != 4 {
		t.Fatalf("window has %d messages, want 4 (messages[6:])", len(got))
	}
	if !strings.HasSuffix(got[0].Content, "#6") {
		t.Errorf("window[0] = %q…, want message #6", got[0].Content[:20])
	}
	if summary != "the summary" {
		t.Errorf("summary = %q", summary)
	}
}

func TestLoadSessionHistory_ClampsCorruptStartIndex(t *testing.T) {
	history := overThresholdHistory(4)
	sessions := &virtualCompactionStore{
		nopSessionStore: nopSessionStore{history: history},
		meta:            map[string]string{store.SessionMetaContextStartIndex: "999"},
	}
	l := &Loop{sessions: sessions}

	got, _ := l.makeLoadSessionHistory()(context.Background(), "sess-clamp")
	if len(got) != 0 {
		t.Errorf("window has %d messages for out-of-range start, want 0 (clamped)", len(got))
	}

	sessions.SetSessionMetadata(context.Background(), "sess-clamp",
		map[string]string{store.SessionMetaContextStartIndex: "-3"})
	got, _ = l.makeLoadSessionHistory()(context.Background(), "sess-clamp")
	if len(got) != 4 {
		t.Errorf("window has %d messages for negative start, want 4 (clamped to 0)", len(got))
	}
}

func TestLoadSessionHistory_CarriesRecentMediaRefsIntoWindow(t *testing.T) {
	// Pre-window media must stay referenceable by the model (the old
	// destructive path re-injected refs into the first kept message). The
	// carry-in is IN-MEMORY only — the persisted transcript stays clean so
	// the UI doesn't sprout 30 stale attachments on a random message.
	history := overThresholdHistory(8)
	history[1].MediaRefs = []providers.MediaRef{{Kind: "image", Path: "a.png"}}
	history[3].MediaRefs = []providers.MediaRef{{Kind: "image", Path: "b.png"}}
	sessions := &virtualCompactionStore{
		nopSessionStore: nopSessionStore{history: history},
		meta:            map[string]string{store.SessionMetaContextStartIndex: "6"},
	}
	l := &Loop{sessions: sessions}

	got, _ := l.makeLoadSessionHistory()(context.Background(), "sess-media")

	if len(got) != 2 {
		t.Fatalf("window has %d messages, want 2", len(got))
	}
	var paths []string
	for _, ref := range got[0].MediaRefs {
		paths = append(paths, ref.Path)
	}
	if len(paths) != 2 {
		t.Fatalf("window[0] carries %d media refs %v, want 2 (b.png, a.png)", len(paths), paths)
	}
	// Persisted transcript unmutated.
	sessions.mu.Lock()
	defer sessions.mu.Unlock()
	if len(sessions.history[6].MediaRefs) != 0 {
		t.Error("carry-in mutated the persisted message — refs must be in-memory only")
	}
}

// ─── store-level helpers ───────────────────────────────────────────────────

func TestContextStartIndexFromMetadata(t *testing.T) {
	cases := []struct {
		meta map[string]string
		n    int
		want int
	}{
		{nil, 10, 0},
		{map[string]string{store.SessionMetaContextStartIndex: "6"}, 10, 6},
		{map[string]string{store.SessionMetaContextStartIndex: "999"}, 10, 10},
		{map[string]string{store.SessionMetaContextStartIndex: "-2"}, 10, 0},
		{map[string]string{store.SessionMetaContextStartIndex: "junk"}, 10, 0},
	}
	for _, c := range cases {
		if got := store.ContextStartIndex(c.meta, c.n); got != c.want {
			t.Errorf("ContextStartIndex(%v, %d) = %d, want %d", c.meta, c.n, got, c.want)
		}
	}
}

func TestNextContextStartIndex_ClampsAndNeverMovesBackward(t *testing.T) {
	cases := []struct{ current, historyLen, keepLast, want int }{
		{0, 100, 4, 96},     // first compaction
		{96, 110, 4, 106},   // second compaction advances
		{100, 110, 50, 100}, // keepLast larger than window → never move backward
		{0, 3, 99999, 0},    // keepLast > len → clamp at 0, not negative
		{96, 100, 0, 100},   // degenerate keepLast
	}
	for _, c := range cases {
		if got := store.NextContextStartIndex(c.current, c.historyLen, c.keepLast); got != c.want {
			t.Errorf("NextContextStartIndex(%d,%d,%d) = %d, want %d", c.current, c.historyLen, c.keepLast, got, c.want)
		}
	}
}

func TestAdvancePastToolRows_CleanWindowBoundary(t *testing.T) {
	msgs := []providers.Message{
		{Role: "user", Content: "q"},
		{Role: "assistant", Content: "", ToolCalls: []providers.ToolCall{{ID: "t1"}}},
		{Role: "tool", Content: "r1", ToolCallID: "t1"},
		{Role: "tool", Content: "r2", ToolCallID: "t2"},
		{Role: "assistant", Content: "done"},
		{Role: "user", Content: "next"},
	}
	// A naive cut landing on the tool rows must advance past them so the
	// window never starts with orphaned tool results (which sanitize would
	// re-drop in memory on every turn forever).
	if got := advancePastToolRows(msgs, 2); got != 4 {
		t.Errorf("advancePastToolRows(msgs, 2) = %d, want 4", got)
	}
	if got := advancePastToolRows(msgs, 5); got != 5 {
		t.Errorf("clean boundary moved: got %d, want 5", got)
	}
	if got := advancePastToolRows(msgs, 6); got != 6 {
		t.Errorf("end boundary: got %d, want 6", got)
	}
}

func TestSetContextStartIndex_MonotonicAgainstConcurrentAdvance(t *testing.T) {
	// A summarizer holding a pre-LLM-call value must not regress a pointer
	// that a concurrent sessions.compact advanced further.
	sessions := &virtualCompactionStore{
		nopSessionStore: nopSessionStore{history: overThresholdHistory(10)},
		meta:            map[string]string{store.SessionMetaContextStartIndex: "116"},
	}
	l := &Loop{sessions: sessions}
	l.setContextStartIndex(context.Background(), "sess-mono", 96)
	if got := sessions.metaStart(); got != "116" {
		t.Errorf("pointer regressed to %q, want kept at \"116\"", got)
	}
	l.setContextStartIndex(context.Background(), "sess-mono", 120)
	if got := sessions.metaStart(); got != "120" {
		t.Errorf("pointer = %q, want advanced to \"120\"", got)
	}
}
