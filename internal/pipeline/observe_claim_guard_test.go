package pipeline

import (
	"context"
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// stubClaimScan simulates the agent-layer OutputClaimGuard: matches when the
// content contains "built and tested". Pattern-level accuracy is covered by
// the guard's own unit tests in internal/agent (pipeline cannot import agent).
func stubClaimScan(content string) ([]string, string) {
	if strings.Contains(content, "built and tested") {
		return []string{"first_person_completed"}, "built and tested"
	}
	return nil, ""
}

func claimGuardDeps(action string) (*PipelineDeps, *[]string) {
	var triggers []string
	deps := &PipelineDeps{
		Config:                 PipelineConfig{MaxIterations: 10},
		ScanOutputClaims:       stubClaimScan,
		OutputClaimGuardAction: action,
		EmitClaimGuardTrigger: func(act string, names []string, phrase string) {
			triggers = append(triggers, act+"|"+strings.Join(names, ",")+"|"+phrase)
		},
	}
	return deps, &triggers
}

func TestObserveStage_ClaimGuard_WarnForcesOneCorrectiveRetry(t *testing.T) {
	t.Parallel()
	deps, triggers := claimGuardDeps("warn")
	stage := NewObserveStage(deps)
	state := defaultState()
	state.Think.LastResponse = &providers.ChatResponse{
		Content:      "Your quiz is built and tested, works great.",
		Thinking:     "reasoning",
		FinishReason: "stop",
	}

	if err := stage.Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if state.Observe.FinalContent != "" {
		t.Fatalf("FinalContent = %q, want empty (retry forced)", state.Observe.FinalContent)
	}
	if !state.Observe.ContinueAfterFinal {
		t.Fatal("ContinueAfterFinal = false, want true")
	}
	if !state.Observe.ClaimGuardRetried {
		t.Fatal("ClaimGuardRetried = false, want true")
	}
	pending := state.Messages.Pending()
	if len(pending) != 2 {
		t.Fatalf("pending len = %d, want 2 (transient draft + [System] note)", len(pending))
	}
	if pending[0].Role != "assistant" || !pending[0].Transient {
		t.Fatalf("pending[0] = %#v, want transient assistant draft", pending[0])
	}
	if pending[1].Role != "user" || !strings.HasPrefix(pending[1].Content, "[System]") {
		t.Fatalf("pending[1] = %#v, want [System] user note", pending[1])
	}
	if !strings.Contains(pending[1].Content, "built and tested") {
		t.Fatalf("[System] note %q should quote the matched phrase", pending[1].Content)
	}
	if len(*triggers) != 1 || !strings.HasPrefix((*triggers)[0], "warn|") {
		t.Fatalf("triggers = %v, want one warn trigger", *triggers)
	}
}

func TestObserveStage_ClaimGuard_SecondTriggerDegradesToLog(t *testing.T) {
	t.Parallel()
	deps, triggers := claimGuardDeps("warn")
	stage := NewObserveStage(deps)
	state := defaultState()
	state.Observe.ClaimGuardRetried = true // retry already spent this run
	state.Think.LastResponse = &providers.ChatResponse{
		Content:      "Still built and tested, I promise.",
		FinishReason: "stop",
	}

	if err := stage.Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	// Delivered as-is (no loop), but the repeat trigger is still recorded.
	if state.Observe.FinalContent != "Still built and tested, I promise." {
		t.Fatalf("FinalContent = %q, want delivered content", state.Observe.FinalContent)
	}
	if state.Observe.ContinueAfterFinal {
		t.Fatal("ContinueAfterFinal = true, want false (no second retry)")
	}
	if len(*triggers) != 1 || !strings.HasPrefix((*triggers)[0], "log|") {
		t.Fatalf("triggers = %v, want one log trigger", *triggers)
	}
}

func TestObserveStage_ClaimGuard_LastIterationDegradesToLog(t *testing.T) {
	t.Parallel()
	deps, triggers := claimGuardDeps("warn")
	stage := NewObserveStage(deps)
	state := defaultState()
	state.Iteration = deps.Config.MaxIterations - 1 // no iteration left for a retry
	state.Think.LastResponse = &providers.ChatResponse{
		Content:      "All built and tested.",
		FinishReason: "stop",
	}

	if err := stage.Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if state.Observe.FinalContent != "All built and tested." {
		t.Fatalf("FinalContent = %q, want delivered content", state.Observe.FinalContent)
	}
	if state.Observe.ContinueAfterFinal {
		t.Fatal("ContinueAfterFinal = true, want false")
	}
	if len(*triggers) != 1 || !strings.HasPrefix((*triggers)[0], "log|") {
		t.Fatalf("triggers = %v, want one log trigger", *triggers)
	}
}

func TestObserveStage_ClaimGuard_NotTriggeredWhenRunHasToolCalls(t *testing.T) {
	t.Parallel()
	for name, mutate := range map[string]func(*RunState){
		"sync_tool_calls":  func(s *RunState) { s.Tool.TotalToolCalls = 2 },
		"async_tool_calls": func(s *RunState) { s.Tool.AsyncToolCalls = []string{"spawn"} },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			deps, triggers := claimGuardDeps("warn")
			stage := NewObserveStage(deps)
			state := defaultState()
			mutate(state)
			state.Think.LastResponse = &providers.ChatResponse{
				Content:      "It is built and tested via the job above.",
				FinishReason: "stop",
			}

			if err := stage.Execute(context.Background(), state); err != nil {
				t.Fatalf("Execute() error: %v", err)
			}
			if state.Observe.FinalContent == "" {
				t.Fatal("FinalContent empty, want delivered content (claim is tool-backed)")
			}
			if len(*triggers) != 0 {
				t.Fatalf("triggers = %v, want none", *triggers)
			}
		})
	}
}

func TestObserveStage_ClaimGuard_OrdinaryReplyNeverFlagged(t *testing.T) {
	t.Parallel()
	deps, triggers := claimGuardDeps("warn")
	stage := NewObserveStage(deps)
	state := defaultState()
	state.Think.LastResponse = &providers.ChatResponse{
		Content:      "No, I can't do that without repository access.",
		FinishReason: "stop",
	}

	if err := stage.Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if state.Observe.FinalContent != "No, I can't do that without repository access." {
		t.Fatalf("FinalContent = %q, want unchanged reply", state.Observe.FinalContent)
	}
	if state.Observe.ContinueAfterFinal || state.Observe.ClaimGuardRetried {
		t.Fatal("guard state mutated on a non-matching reply")
	}
	if len(*triggers) != 0 {
		t.Fatalf("triggers = %v, want none", *triggers)
	}
}

func TestObserveStage_ClaimGuard_BlockReplacesWithHoldingMessage(t *testing.T) {
	t.Parallel()
	deps, triggers := claimGuardDeps("block")
	stage := NewObserveStage(deps)
	state := defaultState()
	state.Think.LastResponse = &providers.ChatResponse{
		Content:      "Everything is built and tested.",
		Thinking:     "reasoning",
		FinishReason: "stop",
	}

	if err := stage.Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if state.Observe.FinalContent != claimGuardHoldingMessage {
		t.Fatalf("FinalContent = %q, want holding message", state.Observe.FinalContent)
	}
	if state.Observe.ContinueAfterFinal {
		t.Fatal("ContinueAfterFinal = true, want false (block does not retry)")
	}
	if len(*triggers) != 1 || !strings.HasPrefix((*triggers)[0], "block|") {
		t.Fatalf("triggers = %v, want one block trigger", *triggers)
	}
}

func TestObserveStage_ClaimGuard_LogDeliversUnchanged(t *testing.T) {
	t.Parallel()
	deps, triggers := claimGuardDeps("log")
	stage := NewObserveStage(deps)
	state := defaultState()
	state.Think.LastResponse = &providers.ChatResponse{
		Content:      "Everything is built and tested.",
		FinishReason: "stop",
	}

	if err := stage.Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if state.Observe.FinalContent != "Everything is built and tested." {
		t.Fatalf("FinalContent = %q, want unchanged reply", state.Observe.FinalContent)
	}
	if len(*triggers) != 1 || !strings.HasPrefix((*triggers)[0], "log|") {
		t.Fatalf("triggers = %v, want one log trigger", *triggers)
	}
}

func TestObserveStage_ClaimGuard_OffOrNilScanDisabled(t *testing.T) {
	t.Parallel()
	cases := map[string]*PipelineDeps{
		"action_off": {
			Config:                 PipelineConfig{MaxIterations: 10},
			ScanOutputClaims:       stubClaimScan,
			OutputClaimGuardAction: "off",
		},
		"nil_scan": {
			Config:                 PipelineConfig{MaxIterations: 10},
			OutputClaimGuardAction: "warn",
		},
	}
	for name, deps := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			stage := NewObserveStage(deps)
			state := defaultState()
			state.Think.LastResponse = &providers.ChatResponse{
				Content:      "Everything is built and tested.",
				FinishReason: "stop",
			}
			if err := stage.Execute(context.Background(), state); err != nil {
				t.Fatalf("Execute() error: %v", err)
			}
			if state.Observe.FinalContent != "Everything is built and tested." {
				t.Fatalf("FinalContent = %q, want unchanged reply", state.Observe.FinalContent)
			}
		})
	}
}
