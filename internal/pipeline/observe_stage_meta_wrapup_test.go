package pipeline

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// knownMetaWrapups are the exact final-round replies observed in production
// (monitor issues cep62x82kb7y3qnb2q046ngzg, cbvi88r48fo6v3e2ngawxmw2y,
// cc6fk76drjcaeg9uo5x8e883i).
var knownMetaWrapups = []string{
	"Looks like some unrelated technical content got mixed into our chat just now, not a request from you. What would you like me to help you build today?",
	"(No new text — pills/template already cover the ask above.)",
	"Ready when you are, Henry.",
}

// stubMetaWrapup mirrors agent.IsMetaWrapupReply. internal/pipeline cannot import
// internal/agent (agent imports pipeline), so the stage tests stub the classifier
// and agent's TestIsMetaWrapupReply covers the phrase family itself.
func stubMetaWrapup(content string) bool {
	lower := strings.ToLower(strings.TrimSpace(content))
	if lower == "" || len([]rune(lower)) > 250 {
		return false
	}
	for _, phrase := range []string{
		"no new text",
		"ready when you are",
		"unrelated technical content",
		"got mixed into our chat",
		"not a request from you",
	} {
		if strings.Contains(lower, phrase) {
			return true
		}
	}
	return false
}

func metaWrapupDeps() *PipelineDeps {
	return &PipelineDeps{IsMetaWrapupReply: stubMetaWrapup}
}

// The turn already produced visible text on a tool-call round — deliver that
// instead of the dismissive wrap-up.
func TestObserveStage_MetaWrapup_SubstitutesLastBlockReply(t *testing.T) {
	t.Parallel()
	const substantive = "Clause 2.1 in contract 291 differs from Шаблон 0 — here is the corrected wording."

	for i, dismissive := range knownMetaWrapups {
		t.Run("phrasing-"+strconv.Itoa(i), func(t *testing.T) {
			t.Parallel()
			stage := NewObserveStage(metaWrapupDeps())
			state := defaultState()
			state.Observe.LastBlockReply = substantive
			state.Think.LastResponse = &providers.ChatResponse{
				Content:      dismissive,
				FinishReason: "stop",
			}

			if err := stage.Execute(context.Background(), state); err != nil {
				t.Fatalf("Execute() error: %v", err)
			}
			if state.Observe.FinalContent == dismissive {
				t.Fatalf("FinalContent = dismissive wrap-up %q, want the turn's substantive text", dismissive)
			}
			if state.Observe.FinalContent != substantive {
				t.Fatalf("FinalContent = %q, want %q", state.Observe.FinalContent, substantive)
			}
			if state.Observe.ContinueAfterFinal {
				t.Error("ContinueAfterFinal = true, want false (substitution needs no extra round)")
			}
			if state.Observe.MetaWrapupRetried {
				t.Error("MetaWrapupRetried = true, want false (no retry was spent)")
			}
		})
	}
}

// Nothing substantive was said this turn — ask for exactly one more round
// instead of shipping the dismissal.
func TestObserveStage_MetaWrapup_NoBlockReply_RetriesOnce(t *testing.T) {
	t.Parallel()
	dismissive := knownMetaWrapups[0]
	stage := NewObserveStage(metaWrapupDeps())
	state := defaultState()
	state.Think.LastResponse = &providers.ChatResponse{
		Content:      dismissive,
		Thinking:     "reasoning",
		FinishReason: "stop",
	}

	if err := stage.Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if state.Observe.FinalContent != "" {
		t.Fatalf("FinalContent = %q, want empty pending the retry round", state.Observe.FinalContent)
	}
	if !state.Observe.ContinueAfterFinal {
		t.Fatal("ContinueAfterFinal = false, want true")
	}
	if !state.Observe.MetaWrapupRetried {
		t.Fatal("MetaWrapupRetried = false, want true")
	}
	pending := state.Messages.Pending()
	if len(pending) != 2 {
		t.Fatalf("pending len = %d, want 2 (meta-remark + nudge)", len(pending))
	}
	if pending[0].Role != "assistant" || pending[0].Content != dismissive || !pending[0].Transient {
		t.Fatalf("pending[0] = %#v, want transient assistant meta-remark", pending[0])
	}
	if pending[1].Role != "user" || pending[1].Content != metaWrapupNudge || !pending[1].Transient {
		t.Fatalf("pending[1] = %#v, want transient user nudge", pending[1])
	}
}

// The retry is bounded: a second meta wrap-up is delivered rather than looping.
func TestObserveStage_MetaWrapup_RetriesAtMostOnce(t *testing.T) {
	t.Parallel()
	dismissive := knownMetaWrapups[1]
	stage := NewObserveStage(metaWrapupDeps())
	state := defaultState()
	state.Think.LastResponse = &providers.ChatResponse{Content: dismissive, FinishReason: "stop"}

	if err := stage.Execute(context.Background(), state); err != nil {
		t.Fatalf("first Execute() error: %v", err)
	}
	if !state.Observe.ContinueAfterFinal {
		t.Fatal("first round: ContinueAfterFinal = false, want true")
	}
	// Pipeline.Run clears the flag before running the extra iteration.
	state.Observe.ContinueAfterFinal = false

	if err := stage.Execute(context.Background(), state); err != nil {
		t.Fatalf("second Execute() error: %v", err)
	}
	if state.Observe.ContinueAfterFinal {
		t.Fatal("second round: ContinueAfterFinal = true, want false (retry budget spent)")
	}
	if state.Observe.FinalContent != dismissive {
		t.Fatalf("FinalContent = %q, want the reply delivered as-is after the single retry", state.Observe.FinalContent)
	}
}

// Regression guard against over-suppression: a genuinely short final reply is
// delivered untouched even when the turn has a block reply to fall back on.
func TestObserveStage_LegitimateShortReply_NotSubstituted(t *testing.T) {
	t.Parallel()
	const legit = "Done — pushed the fix."
	stage := NewObserveStage(metaWrapupDeps())
	state := defaultState()
	state.Observe.LastBlockReply = "Running the test suite now."
	state.Think.LastResponse = &providers.ChatResponse{Content: legit, FinishReason: "stop"}

	if err := stage.Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if state.Observe.FinalContent != legit {
		t.Fatalf("FinalContent = %q, want %q", state.Observe.FinalContent, legit)
	}
	if state.Observe.ContinueAfterFinal || state.Observe.MetaWrapupRetried {
		t.Error("legitimate reply triggered the meta wrap-up path")
	}
}

// Without the dep wired (lite edition, direct pipeline construction) behaviour is
// exactly as before.
func TestObserveStage_MetaWrapup_NilDepKeepsLegacyBehaviour(t *testing.T) {
	t.Parallel()
	dismissive := knownMetaWrapups[0]
	stage := NewObserveStage(&PipelineDeps{})
	state := defaultState()
	state.Think.LastResponse = &providers.ChatResponse{Content: dismissive, FinishReason: "stop"}

	if err := stage.Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if state.Observe.FinalContent != dismissive {
		t.Fatalf("FinalContent = %q, want unchanged legacy behaviour", state.Observe.FinalContent)
	}
	if state.Observe.ContinueAfterFinal {
		t.Error("ContinueAfterFinal = true, want false")
	}
}

// A late user injection still wins: those messages must be answered, and that
// path already continues the run.
func TestObserveStage_MetaWrapup_InjectedMessagesTakePrecedence(t *testing.T) {
	t.Parallel()
	dismissive := knownMetaWrapups[0]
	deps := metaWrapupDeps()
	deps.DrainInjectCh = func() []providers.Message {
		return []providers.Message{{Role: "user", Content: "and also check clause 3.2"}}
	}
	stage := NewObserveStage(deps)
	state := defaultState()
	state.Think.LastResponse = &providers.ChatResponse{Content: dismissive, FinishReason: "stop"}

	if err := stage.Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if !state.Observe.ContinueAfterFinal {
		t.Fatal("ContinueAfterFinal = false, want true")
	}
	if state.Observe.MetaWrapupRetried {
		t.Error("MetaWrapupRetried = true, want false — the injection path handled this round")
	}
	pending := state.Messages.Pending()
	if len(pending) != 2 || pending[1].Content != "and also check clause 3.2" {
		t.Fatalf("pending = %#v, want transient assistant + injected user message", pending)
	}
}
