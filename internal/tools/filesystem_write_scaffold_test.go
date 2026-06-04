package tools

// Tests for the non-deliverable-scaffolding guard in write_file.
//
// Background: write_file defaults deliver=true, so EVERY file an agent writes is
// auto-attached to the user's chat as a download link. Agents (following skill
// instructions like manage-view / jobs) write internal scaffolding — shell helper
// scripts and skill HTTP payload files (set-pills.sh, manage-view.json, prompt.txt,
// launch-job.sh) purely to drive a curl/skill call. Those leaked to end users as
// download links. End users receive apps, links, and documents — never raw scripts
// or skill payloads. These tests pin that scaffolding is NOT auto-delivered while
// genuine deliverables still are, and that an explicit deliver:true is honoured as
// an escape hatch.

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// shellScripts and skill payloads written with the default deliver (omitted) must
// NOT populate Result.Media nor mark the file delivered.
func TestWriteFileScaffold_NotDeliveredByDefault(t *testing.T) {
	cases := []string{
		"set-pills.sh",
		"clear-pills.sh",
		"launch-job.sh",
		"manage-view.json",
		"prompt.txt",
		"helper.bash",
	}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			workspace := t.TempDir()
			workspaceCanonical, _ := filepath.EvalSymlinks(workspace)
			tool := NewWriteFileTool(workspaceCanonical, true)

			dm := NewDeliveredMedia()
			ctx := WithDeliveredMedia(context.Background(), dm)

			// No "deliver" key — default would be true, but scaffolding must flip to false.
			result := tool.Execute(ctx, map[string]any{
				"path":    name,
				"content": "x",
			})
			if result.IsError {
				t.Fatalf("expected success, got error: %s", result.ForLLM)
			}
			if len(result.Media) != 0 {
				t.Errorf("%s: expected 0 Media entries (scaffolding not delivered), got %d", name, len(result.Media))
			}
			resolved := filepath.Join(workspaceCanonical, name)
			if dm.IsDelivered(resolved) {
				t.Errorf("%s: expected not delivered, but dm.IsDelivered = true", name)
			}
		})
	}
}

// The "automatically delivered to the user" hint must NOT be appended for scaffolding,
// so the model isn't told the file reached the user.
func TestWriteFileScaffold_NoDeliveryHintInMessage(t *testing.T) {
	workspace := t.TempDir()
	workspaceCanonical, _ := filepath.EvalSymlinks(workspace)
	tool := NewWriteFileTool(workspaceCanonical, true)

	result := tool.Execute(context.Background(), map[string]any{
		"path":    "set-pills.sh",
		"content": "curl ...",
	})
	if result.IsError {
		t.Fatalf("expected success, got error: %s", result.ForLLM)
	}
	if got := result.ForLLM; strings.Contains(got, "automatically delivered to the user") {
		t.Errorf("scaffolding write should not claim delivery, got message: %q", got)
	}
}

// Genuine deliverables (documents, data, archives) must still auto-deliver by default.
func TestWriteFileDeliverable_StillDeliveredByDefault(t *testing.T) {
	cases := []string{
		"report.pdf",
		"export.csv",
		"summary.md",
		"data.txt",
		"site.html",
		"bundle.zip",
	}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			workspace := t.TempDir()
			workspaceCanonical, _ := filepath.EvalSymlinks(workspace)
			tool := NewWriteFileTool(workspaceCanonical, true)

			dm := NewDeliveredMedia()
			ctx := WithDeliveredMedia(context.Background(), dm)

			result := tool.Execute(ctx, map[string]any{
				"path":    name,
				"content": "x",
			})
			if result.IsError {
				t.Fatalf("expected success, got error: %s", result.ForLLM)
			}
			if len(result.Media) != 1 {
				t.Errorf("%s: expected 1 Media entry (deliverable), got %d", name, len(result.Media))
			}
		})
	}
}

// An explicit deliver:true is an escape hatch — it overrides the scaffolding default.
func TestWriteFileScaffold_ExplicitDeliverTrueHonoured(t *testing.T) {
	workspace := t.TempDir()
	workspaceCanonical, _ := filepath.EvalSymlinks(workspace)
	tool := NewWriteFileTool(workspaceCanonical, true)

	result := tool.Execute(context.Background(), map[string]any{
		"path":    "install.sh",
		"content": "#!/bin/sh\necho hi\n",
		"deliver": true,
	})
	if result.IsError {
		t.Fatalf("expected success, got error: %s", result.ForLLM)
	}
	if len(result.Media) != 1 {
		t.Errorf("explicit deliver:true should override scaffolding default, got %d Media entries", len(result.Media))
	}
}
