package pipeline

import (
	"context"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// ObserveStage runs per iteration after ToolStage. Drains InjectCh,
// accumulates final content when no tool calls, tracks block replies.
// Does NOT implement StageWithResult — never controls flow.
type ObserveStage struct {
	deps *PipelineDeps
}

// NewObserveStage creates an ObserveStage.
func NewObserveStage(deps *PipelineDeps) *ObserveStage {
	return &ObserveStage{deps: deps}
}

func (s *ObserveStage) Name() string { return "observe" }

// Execute drains injected messages, accumulates final content + block replies.
func (s *ObserveStage) Execute(_ context.Context, state *RunState) error {
	injected := s.drainInjectedMessages()

	resp := state.Think.LastResponse
	if resp == nil {
		appendPendingMessages(state, injected)
		return nil
	}

	// Track block replies only for tool-iteration responses. Final answers do
	// not count, otherwise gateway dedup can suppress delivery.
	if resp.Content != "" && len(resp.ToolCalls) > 0 {
		state.Observe.BlockReplies++
		state.Observe.LastBlockReply = resp.Content
	}

	if len(resp.ToolCalls) == 0 {
		s.observeFinalResponse(state, resp, injected)
	} else {
		appendPendingMessages(state, injected)
	}

	s.accumulateAssistantImages(state, resp)
	return nil
}

func (s *ObserveStage) drainInjectedMessages() []providers.Message {
	if s.deps.DrainInjectCh == nil {
		return nil
	}
	return s.deps.DrainInjectCh()
}

func (s *ObserveStage) observeFinalResponse(state *RunState, resp *providers.ChatResponse, injected []providers.Message) {
	if len(injected) == 0 {
		if s.applyOutputClaimGuard(state, resp) {
			return
		}
		state.Observe.FinalContent = resp.Content
		state.Observe.FinalThinking = resp.Thinking
		return
	}

	state.Messages.AppendPending(providers.Message{
		Role:      "assistant",
		Content:   resp.Content,
		Thinking:  resp.Thinking,
		Transient: true,
	})
	appendPendingMessages(state, injected)
	state.Observe.FinalContent = ""
	state.Observe.FinalThinking = ""
	state.Observe.ContinueAfterFinal = true
}

// claimGuardHoldingMessage replaces a flagged reply in "block" mode: neutral,
// no invented specifics, no completion claim.
const claimGuardHoldingMessage = "I'm still working on this - I don't have a verified result to share yet. I'll follow up once the work has actually run."

// applyOutputClaimGuard checks a candidate final reply for completion-claim
// language ("built/tested/deployed/verified") that has no backing tool call
// anywhere in the run (sync or async). Legitimate zero-tool-call replies
// (plain Q&A, refusals, plans) don't match the guard's patterns; async job
// announcements (cmd.handleCodeAnnounce) never enter the pipeline at all, so
// they cannot be flagged here.
//
// Returns true when the stage consumed the response (forced a corrective
// retry or substituted a holding message) — the caller must not set
// FinalContent from resp in that case.
func (s *ObserveStage) applyOutputClaimGuard(state *RunState, resp *providers.ChatResponse) bool {
	action := s.deps.OutputClaimGuardAction
	if s.deps.ScanOutputClaims == nil || action == "off" || resp.Content == "" {
		return false
	}
	if state.Tool.TotalToolCalls > 0 || len(state.Tool.AsyncToolCalls) > 0 {
		return false
	}
	names, phrase := s.deps.ScanOutputClaims(resp.Content)
	if len(names) == 0 {
		return false
	}
	// One corrective retry per run — a repeat trigger degrades to log-only so
	// the guard can never loop the run. Same when no iteration remains for the
	// retry to actually execute.
	if action == "warn" && (state.Observe.ClaimGuardRetried ||
		(s.deps.Config.MaxIterations > 0 && state.Iteration+1 >= s.deps.Config.MaxIterations)) {
		action = "log"
	}
	if s.deps.EmitClaimGuardTrigger != nil {
		s.deps.EmitClaimGuardTrigger(action, names, phrase)
	}
	switch action {
	case "warn":
		// Same shape as the late-injection path above: keep the draft as a
		// transient assistant turn, append a corrective [System] user note
		// (matching the loop_job_result_reminder / loop_team_reminders
		// convention), and give the model one more turn.
		state.Observe.ClaimGuardRetried = true
		state.Messages.AppendPending(providers.Message{
			Role:      "assistant",
			Content:   resp.Content,
			Thinking:  resp.Thinking,
			Transient: true,
		})
		state.Messages.AppendPending(providers.Message{
			Role:    "user",
			Content: claimGuardRetryNote(phrase),
		})
		state.Observe.FinalContent = ""
		state.Observe.FinalThinking = ""
		state.Observe.ContinueAfterFinal = true
		return true
	case "block":
		state.Observe.FinalContent = claimGuardHoldingMessage
		state.Observe.FinalThinking = ""
		return true
	default: // "log" (or unknown): record only, deliver unchanged
		return false
	}
}

// claimGuardRetryNote builds the corrective [System] note for "warn" mode.
func claimGuardRetryNote(phrase string) string {
	return "[System] Your last draft claimed \"" + phrase + "\" but you made no tool calls this turn. " +
		"If the work is genuinely already done from an earlier tool call, restate it without new invented specifics. " +
		"Otherwise either do the work now via the right tool, or tell the user plainly what's blocking you - " +
		"do not describe a result that didn't happen."
}

func (s *ObserveStage) accumulateAssistantImages(state *RunState, resp *providers.ChatResponse) {
	if len(resp.Images) == 0 {
		return
	}
	for _, img := range resp.Images {
		if img.Partial {
			continue
		}
		state.Observe.AssistantImages = append(state.Observe.AssistantImages, img)
	}
	// Clear on response so a re-processing pass (for example a retry) does not double-count.
	resp.Images = nil
}

func appendPendingMessages(state *RunState, messages []providers.Message) {
	for _, msg := range messages {
		state.Messages.AppendPending(msg)
	}
}
