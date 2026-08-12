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
		"unrelated technical content",
		"got mixed into our chat",
		"not a request from you",
	} {
		if strings.Contains(lower, phrase) {
			return true
		}
	}
	// Standalone family: only when the phrase is effectively the whole reply.
	for _, phrase := range []string{"ready when you are", "waiting on your"} {
		if !strings.HasPrefix(lower, phrase) {
			continue
		}
		if len([]rune(strings.Trim(lower[len(phrase):], " .,!?—-'"))) <= 15 {
			return true
		}
	}
	return false
}

// metaWrapupDeps wires the classifier with iteration headroom to spare, so the
// retry path is reachable. Tests that need the budget edge override MaxIterations.
func metaWrapupDeps() *PipelineDeps {
	return &PipelineDeps{
		IsMetaWrapupReply: stubMetaWrapup,
		Config:            PipelineConfig{MaxIterations: 8},
	}
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
				Thinking:     "the user's message looks like noise",
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
			// The discarded round's reasoning must not be persisted against the
			// delivered text — it explains a reply the user never sees.
			if state.Observe.FinalThinking != "" {
				t.Errorf("FinalThinking = %q, want empty (reasoning belongs to the discarded remark)", state.Observe.FinalThinking)
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

// The retry needs a round to land in. On the last allowed iteration requesting it
// would exit the loop with no content at all (FinalizeStage then delivers "..."),
// so the remark is delivered instead — worse text beats no text.
func TestObserveStage_MetaWrapup_NoIterationHeadroom_DeliversRemark(t *testing.T) {
	t.Parallel()
	dismissive := knownMetaWrapups[0]
	deps := metaWrapupDeps()
	deps.Config.MaxIterations = 3
	stage := NewObserveStage(deps)
	state := defaultState()
	state.Iteration = 2 // last iteration the pipeline loop will run

	state.Think.LastResponse = &providers.ChatResponse{Content: dismissive, FinishReason: "stop"}

	if err := stage.Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if state.Observe.ContinueAfterFinal {
		t.Error("ContinueAfterFinal = true, want false — no iteration left to retry into")
	}
	if state.Observe.MetaWrapupRetried {
		t.Error("MetaWrapupRetried = true, want false — no retry was possible")
	}
	if state.Observe.FinalContent != dismissive {
		t.Fatalf("FinalContent = %q, want the remark delivered as-is (never empty)", state.Observe.FinalContent)
	}
	if pending := state.Messages.Pending(); len(pending) != 0 {
		t.Errorf("pending = %#v, want none (no retry round to prime)", pending)
	}
}

// With a round to spare the same input still retries — the headroom guard must
// not disable the salvage generally.
func TestObserveStage_MetaWrapup_WithIterationHeadroom_Retries(t *testing.T) {
	t.Parallel()
	deps := metaWrapupDeps()
	deps.Config.MaxIterations = 3
	stage := NewObserveStage(deps)
	state := defaultState()
	state.Iteration = 1 // one more iteration will run

	state.Think.LastResponse = &providers.ChatResponse{Content: knownMetaWrapups[0], FinishReason: "stop"}

	if err := stage.Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if !state.Observe.ContinueAfterFinal {
		t.Fatal("ContinueAfterFinal = false, want true")
	}
	if state.Observe.FinalContent != "" {
		t.Fatalf("FinalContent = %q, want empty pending the retry round", state.Observe.FinalContent)
	}
}

// Regression guard against over-suppression. Substitution overwrites both the
// delivered reply and the persisted assistant message, so a false positive is
// worse than the bug being fixed: these replies must survive verbatim even though
// the turn has a block reply to fall back on.
func TestObserveStage_LegitimateShortReply_NotSubstituted(t *testing.T) {
	t.Parallel()
	legits := []string{
		"Done — pushed the fix.",
		// Standalone-family wording used inside a reply that says something.
		"I've drafted the migration. Waiting on your go-ahead.",
		"Deployed. Ready when you are to run the smoke test.",
		"Your patch mixes in unrelated content — should I split it?",
	}
	for i, legit := range legits {
		t.Run("reply-"+strconv.Itoa(i), func(t *testing.T) {
			t.Parallel()
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
		})
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

// --- end-to-end: the real Think→Observe→Finalize loop ---

// metaWrapupPipeline wires the three real stages the way agent runs do.
func metaWrapupPipeline(deps *PipelineDeps) *Pipeline {
	return NewPipeline(
		nil,
		[]Stage{NewThinkStage(deps), NewObserveStage(deps)},
		[]Stage{NewFinalizeStage(deps)},
		*deps,
	)
}

// The turn must never end empty. With no iteration left for a retry the loop
// exits immediately and FinalizeStage substitutes "..." — so the remark has to be
// delivered instead. Regression guard for the reproduced last-iteration failure.
func TestPipeline_MetaWrapup_LastIteration_DeliversRemarkNotEllipsis(t *testing.T) {
	t.Parallel()
	dismissive := knownMetaWrapups[0]
	calls := 0
	deps := &PipelineDeps{
		Config:            PipelineConfig{MaxIterations: 1, MaxTokens: 1000},
		IsMetaWrapupReply: stubMetaWrapup,
		CallLLM: func(_ context.Context, _ *RunState, _ providers.ChatRequest) (*providers.ChatResponse, error) {
			calls++
			return &providers.ChatResponse{Content: dismissive, FinishReason: "stop"}, nil
		},
	}

	result, err := metaWrapupPipeline(deps).Run(context.Background(), buildMinimalRunState())
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	if result.Content == "..." {
		t.Fatal(`result content = "...", want the remark — the turn ended with no reply at all`)
	}
	if result.Content != dismissive {
		t.Fatalf("result content = %q, want %q", result.Content, dismissive)
	}
	if calls != 1 {
		t.Errorf("CallLLM count = %d, want 1 (no round was available to retry)", calls)
	}
}

// With headroom the same input is retried once and the real answer is delivered.
func TestPipeline_MetaWrapup_WithHeadroom_DeliversRetriedAnswer(t *testing.T) {
	t.Parallel()
	const answer = "Clause 2.1 in contract 291 differs from Шаблон 0 — here is the corrected wording."
	calls := 0
	deps := &PipelineDeps{
		Config:            PipelineConfig{MaxIterations: 3, MaxTokens: 1000},
		IsMetaWrapupReply: stubMetaWrapup,
		CallLLM: func(_ context.Context, _ *RunState, _ providers.ChatRequest) (*providers.ChatResponse, error) {
			calls++
			if calls == 1 {
				return &providers.ChatResponse{Content: knownMetaWrapups[0], FinishReason: "stop"}, nil
			}
			return &providers.ChatResponse{Content: answer, FinishReason: "stop"}, nil
		},
	}

	result, err := metaWrapupPipeline(deps).Run(context.Background(), buildMinimalRunState())
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	if result.Content != answer {
		t.Fatalf("result content = %q, want the retried answer %q", result.Content, answer)
	}
	if calls != 2 {
		t.Errorf("CallLLM count = %d, want 2 (one nudged retry)", calls)
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
