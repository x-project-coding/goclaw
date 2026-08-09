package pipeline

import (
	"context"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// Duplicate-continuation dedup (42bucks fork patch): a continuation turn that
// exists only because the previous turn carried a tool call must not re-emit
// that turn's reply text. Same-run only — separate runs never suppress.

// scriptedThinkDeps returns deps whose CallLLM pops responses off script in
// order and records every emitted block reply.
func scriptedThinkDeps(script []*providers.ChatResponse, emitted *[]string) *PipelineDeps {
	i := 0
	return &PipelineDeps{
		Config: PipelineConfig{MaxIterations: 10, MaxTokens: 1000},
		CallLLM: func(_ context.Context, _ *RunState, _ providers.ChatRequest) (*providers.ChatResponse, error) {
			resp := script[i]
			i++
			return resp, nil
		},
		EmitBlockReplyWithSource: func(content, _ string) {
			*emitted = append(*emitted, content)
		},
	}
}

func toolCallResp(content string) *providers.ChatResponse {
	return &providers.ChatResponse{
		Content:      content,
		FinishReason: "tool_calls",
		ToolCalls:    []providers.ToolCall{{ID: "tc1", Name: "call_skill_service", Arguments: map[string]any{"skill": "manage-view"}}},
	}
}

// runThinkIterations executes ThinkStage once per scripted response on a
// single RunState, advancing state.Iteration like Pipeline.Run does.
func runThinkIterations(t *testing.T, state *RunState, deps *PipelineDeps, iterations int) *ThinkStage {
	t.Helper()
	stage := NewThinkStage(deps)
	for i := 0; i < iterations; i++ {
		state.Iteration = i
		if err := stage.Execute(context.Background(), state); err != nil {
			t.Fatalf("iteration %d Execute() error: %v", i, err)
		}
	}
	return stage
}

func TestThinkStage_DuplicateContinuation_SuppressesBlockReply(t *testing.T) {
	t.Parallel()
	const greeting = "Hey! I'm Roman. What would you like to build today?"
	variants := []struct {
		name   string
		second string
	}{
		{"exact repeat", greeting},
		{"markdown bold variant", "**Hey! I'm Roman.** What would you like to build today?"},
		{"whitespace variant", "Hey! I'm Roman.\n\nWhat would you like to build today?"},
	}
	for _, tc := range variants {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var emitted []string
			deps := scriptedThinkDeps([]*providers.ChatResponse{
				toolCallResp(greeting),
				toolCallResp(tc.second),
			}, &emitted)
			state := defaultState()

			stage := runThinkIterations(t, state, deps, 2)

			if len(emitted) != 1 || emitted[0] != greeting {
				t.Fatalf("block replies = %q, want exactly the first turn's text once", emitted)
			}
			// The duplicate turn's tool call must still execute: the loop continues
			// and the assistant message (with tool calls) is appended for context.
			if stage.Result() != Continue {
				t.Errorf("Result() = %v, want Continue (tool call must still run)", stage.Result())
			}
			pending := state.Messages.Pending()
			if len(pending) != 2 || len(pending[1].ToolCalls) != 1 {
				t.Errorf("pending = %d msgs, want both assistant tool-call turns appended", len(pending))
			}
		})
	}
}

func TestThinkStage_DifferentContinuation_EmitsBlockReply(t *testing.T) {
	t.Parallel()
	var emitted []string
	deps := scriptedThinkDeps([]*providers.ChatResponse{
		toolCallResp("Let me set up the view."),
		toolCallResp("Now saving your preferences."),
	}, &emitted)

	runThinkIterations(t, defaultState(), deps, 2)

	want := []string{"Let me set up the view.", "Now saving your preferences."}
	if len(emitted) != 2 || emitted[0] != want[0] || emitted[1] != want[1] {
		t.Fatalf("block replies = %q, want %q", emitted, want)
	}
}

func TestThinkStage_SilentToolTurnBetween_StillSuppressesRepeat(t *testing.T) {
	t.Parallel()
	// The reference is the last CONTENT-BEARING turn: a silent tool call in
	// between must not reset it.
	const text = "Working on it."
	var emitted []string
	deps := scriptedThinkDeps([]*providers.ChatResponse{
		toolCallResp(text),
		toolCallResp(""), // silent tool call
		toolCallResp(text),
	}, &emitted)

	runThinkIterations(t, defaultState(), deps, 3)

	if len(emitted) != 1 || emitted[0] != text {
		t.Fatalf("block replies = %q, want the text once", emitted)
	}
}

func TestThinkStage_DuplicateAcrossRuns_NotSuppressed(t *testing.T) {
	t.Parallel()
	// The same reply in two SEPARATE runs (fresh RunState each) is legitimate.
	const text = "Here is your Paris itinerary."
	for run := 0; run < 2; run++ {
		var emitted []string
		deps := scriptedThinkDeps([]*providers.ChatResponse{toolCallResp(text)}, &emitted)
		runThinkIterations(t, defaultState(), deps, 1)
		if len(emitted) != 1 || emitted[0] != text {
			t.Fatalf("run %d: block replies = %q, want the text once", run, emitted)
		}
	}
}

func TestThinkStage_DuplicateFinalTurn_StillBreaksLoop(t *testing.T) {
	t.Parallel()
	// A duplicate FINAL turn (no tool calls) must not disturb loop flow:
	// BreakLoop as always, no block reply for the final turn (final delivery
	// dedup happens at the gateway), and LastResponse carries the content.
	const greeting = "Hey! I'm Roman. What would you like to build today?"
	var emitted []string
	deps := scriptedThinkDeps([]*providers.ChatResponse{
		toolCallResp(greeting),
		{Content: greeting, FinishReason: "stop"},
	}, &emitted)
	state := defaultState()

	stage := runThinkIterations(t, state, deps, 2)

	if stage.Result() != BreakLoop {
		t.Errorf("Result() = %v, want BreakLoop", stage.Result())
	}
	if len(emitted) != 1 {
		t.Errorf("block replies = %q, want only the tool-call turn's", emitted)
	}
	if state.Think.LastResponse == nil || state.Think.LastResponse.Content != greeting {
		t.Error("LastResponse must keep the final turn's content")
	}
}
