package agent

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// assertUserFacingStopMessage checks that a loop-guard kill message is a
// plain-language sentence rather than the raw internal diagnostic that the
// detector produced for the logs.
func assertUserFacingStopMessage(t *testing.T, got, rawDiagnostic string) {
	t.Helper()
	if got == "" {
		t.Fatal("expected a user-facing stop message, got empty finalContent")
	}
	if strings.HasPrefix(got, "CRITICAL:") {
		t.Fatalf("finalContent leaks the raw internal diagnostic: %q", got)
	}
	if rawDiagnostic != "" && strings.Contains(got, rawDiagnostic) {
		t.Fatalf("finalContent contains the raw internal diagnostic %q: %q", rawDiagnostic, got)
	}
	if !strings.Contains(got, "I was unable to complete this task") {
		t.Fatalf("finalContent is not phrased for the user: %q", got)
	}
}

// TestProcessToolResult_SameResultCriticalIsUserFacing covers the case from the
// production report: a tool that keeps returning a byte-identical failure body
// for differently-argued calls trips detectSameResult, and the run stops. The
// user must see a sentence, not "CRITICAL: ... Stopping to prevent runaway loop."
func TestProcessToolResult_SameResultCriticalIsUserFacing(t *testing.T) {
	t.Parallel()
	l := &Loop{id: "test-agent"}
	rs := &runState{}
	req := &RunRequest{RunID: "run-loopguard"}
	ctx := context.Background()
	noopEmit := func(AgentEvent) {}

	const toolName = "call_skill_service"
	// Identical result body, different args each call — the same-result detector
	// goes critical at sameResultCritical.
	var action toolResultAction
	for i := range sameResultCritical {
		tc := providers.ToolCall{
			ID:        "call-" + strconv.Itoa(i),
			Name:      toolName,
			Arguments: map[string]any{"title": "task " + strconv.Itoa(i)},
		}
		result := &tools.Result{ForLLM: "502 Bad Gateway", IsError: true}
		_, _, action = l.processToolResult(ctx, rs, req, noopEmit, tc, toolName, result, false)
	}

	if action != toolResultBreak {
		t.Fatalf("expected toolResultBreak after %d identical results, got %v", sameResultCritical, action)
	}
	if !rs.loopKilled {
		t.Fatal("expected loopKilled to be set")
	}

	// The detector still produces the raw diagnostic — it just must not be what
	// the user is shown.
	level, raw := rs.loopDetector.detectSameResult(toolName, hashResult("502 Bad Gateway"))
	if level != "critical" {
		t.Fatalf("expected detector level critical, got %q", level)
	}
	assertUserFacingStopMessage(t, rs.finalContent, raw)
	if !strings.Contains(rs.finalContent, toolName) {
		t.Fatalf("expected the tool name in the stop message: %q", rs.finalContent)
	}
}

// TestCheckReadOnlyStreak_CriticalIsUserFacing covers the sibling branch: a long
// low-uniqueness read-only streak kills the run, and that kill message is also
// shipped to the user verbatim.
func TestCheckReadOnlyStreak_CriticalIsUserFacing(t *testing.T) {
	t.Parallel()
	l := &Loop{id: "test-agent"}
	rs := &runState{}
	req := &RunRequest{RunID: "run-loopguard-readonly"}

	// Re-read the same two files until the stuck-mode critical threshold trips
	// (uniqueness ratio well below readOnlyUniquenessThreshold).
	for i := range readOnlyStreakCritical {
		rs.loopDetector.recordMutation("read_file", map[string]any{"path": "file-" + strconv.Itoa(i%2)})
	}

	level, raw := rs.loopDetector.detectReadOnlyStreak()
	if level != "critical" {
		t.Fatalf("expected detector level critical after %d read-only calls, got %q", readOnlyStreakCritical, level)
	}

	warnMsg, shouldBreak := l.checkReadOnlyStreak(rs, req)
	if !shouldBreak {
		t.Fatal("expected checkReadOnlyStreak to break the run")
	}
	if warnMsg != nil {
		t.Fatalf("expected no injected warning on the critical path, got %+v", warnMsg)
	}
	if !rs.loopKilled {
		t.Fatal("expected loopKilled to be set")
	}
	assertUserFacingStopMessage(t, rs.finalContent, raw)
}
