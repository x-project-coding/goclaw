package pipeline

import (
	"context"
	"log/slog"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// metaWrapupNudge is the one-shot instruction injected when the model's final
// round was a meta-remark and the turn produced no other visible text.
const metaWrapupNudge = "[System] Answer the user's last message directly — do not comment on the conversation state."

// metaWrapupLogPreviewRunes bounds substituted content in structured logs.
const metaWrapupLogPreviewRunes = 200

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
		if s.handleMetaWrapup(state, resp) {
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

// handleMetaWrapup salvages a turn whose final round is a short meta-remark about
// the conversation ("no new text …", "unrelated technical content got mixed into
// our chat …") instead of the substantive answer the model already worked on.
// Returns true when it took over final-content selection.
//
// Two salvage paths:
//  1. The turn already emitted visible text on a tool-call round — deliver that
//     (LastBlockReply) instead of the meta-remark.
//  2. Nothing substantive was said — give the model exactly one more round with a
//     direct nudge rather than shipping the dismissal. MetaWrapupRetried bounds
//     this to one retry per run, and the retry is skipped when the iteration
//     budget has no round left to retry into; in both cases the remark falls
//     through and is delivered as-is, so the user always gets something.
//
// Delivery caveat for path 1: on streaming channels the substituted block reply
// has already been streamed to the user, and the gateway's interim dedup
// (cmd/gateway_consumer_normal.go) only tracks non-streaming deliveries, so the
// final message repeats it. Accepted deliberately — repeating the turn's real
// output is better than the dismissal it replaces, and suppressing the final
// message instead would leave the stream bubble as the turn's only record.
func (s *ObserveStage) handleMetaWrapup(state *RunState, resp *providers.ChatResponse) bool {
	if s.deps.IsMetaWrapupReply == nil || !s.deps.IsMetaWrapupReply(resp.Content) {
		return false
	}

	if state.Observe.LastBlockReply != "" {
		slog.Warn("observe: meta wrap-up final reply replaced with last block reply",
			"session", state.Input.SessionKey,
			"discarded", resp.Content,
			"delivered", logPreview(state.Observe.LastBlockReply, metaWrapupLogPreviewRunes))
		state.Observe.FinalContent = state.Observe.LastBlockReply
		// The meta round's reasoning explains the discarded remark, not the text
		// being delivered — persisting it would misdescribe the reply.
		state.Observe.FinalThinking = ""
		return true
	}

	// The retry needs an iteration to land in: Pipeline.Run clears
	// ContinueAfterFinal and continues, so requesting it on the last allowed
	// iteration just exits the loop with no content and FinalizeStage delivers
	// "..." — strictly worse than the remark. This is exactly where ThinkStage's
	// 90%-of-budget nudge pushes the model toward a wrap-up remark, so it is a
	// live case, not a corner one.
	maxIter := s.deps.Config.MaxIterations
	if state.Observe.MetaWrapupRetried || (maxIter > 0 && state.Iteration+1 >= maxIter) {
		return false
	}
	state.Observe.MetaWrapupRetried = true
	slog.Warn("observe: meta wrap-up final reply with no block reply, retrying once",
		"session", state.Input.SessionKey,
		"discarded", resp.Content)

	// Transient: the meta-remark and the nudge are runtime-only context for the
	// retry round. FinalizeStage persists the definitive assistant message.
	state.Messages.AppendPending(providers.Message{
		Role:      "assistant",
		Content:   resp.Content,
		Thinking:  resp.Thinking,
		Transient: true,
	})
	state.Messages.AppendPending(providers.Message{
		Role:      "user",
		Content:   metaWrapupNudge,
		Transient: true,
	})
	state.Observe.FinalContent = ""
	state.Observe.FinalThinking = ""
	state.Observe.ContinueAfterFinal = true
	return true
}

// logPreview bounds a string to max runes for structured logging.
func logPreview(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max]) + "…"
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
