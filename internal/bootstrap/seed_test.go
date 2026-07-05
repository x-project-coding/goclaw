package bootstrap

import (
	"strings"
	"testing"
)

// TestAgentsTemplateContainsMemoryLayoutAndFiles verifies the embedded AGENTS.md
// template carries the memory taxonomy and workspace file rules sections.
func TestAgentsTemplateContainsMemoryLayoutAndFiles(t *testing.T) {
	tpl, err := ReadTemplate(AgentsFile)
	if err != nil {
		t.Fatal("ReadTemplate failed:", err)
	}
	for _, marker := range []string{"### Memory layout", "## Files"} {
		if !strings.Contains(tpl, marker) {
			t.Errorf("AGENTS.md template missing %q section", marker)
		}
	}
}

// TestAgentsTemplateMemorySharingSemantics verifies the Memory layout block
// annotates which memory files are shared workspace-wide vs. private to the
// member being talked to, and states the shared-vs-private rule of thumb.
func TestAgentsTemplateMemorySharingSemantics(t *testing.T) {
	tpl, err := ReadTemplate(AgentsFile)
	if err != nil {
		t.Fatal("ReadTemplate failed:", err)
	}

	sharedFiles := []string{
		"memory/company.md",
		"memory/use-cases.md",
		"memory/projects/<slug>.md",
		"memory/decisions.md",
	}
	for _, f := range sharedFiles {
		line := layoutBulletFor(tpl, f)
		if line == "" {
			t.Errorf("AGENTS.md template missing a Memory layout bullet for %q", f)
			continue
		}
		if !strings.Contains(strings.ToLower(line), "shared with all workspace members") {
			t.Errorf("expected %q bullet to be marked shared with all workspace members, got: %s", f, line)
		}
	}

	privateFiles := []string{
		"MEMORY.md",
		"memory/people/<name>.md",
		"memory/YYYY-MM-DD.md",
	}
	for _, f := range privateFiles {
		line := layoutBulletFor(tpl, f)
		if line == "" {
			t.Errorf("AGENTS.md template missing a Memory layout bullet for %q", f)
			continue
		}
		if !strings.Contains(strings.ToLower(line), "private to the member you're talking to") {
			t.Errorf("expected %q bullet to be marked private to the member you're talking to, got: %s", f, line)
		}
	}

	if !strings.Contains(tpl, "Workspace facts (launches, decisions, projects, company info) go in shared files") {
		t.Error("AGENTS.md template missing the shared-vs-private memory rule line")
	}
}

// TestAgentsTemplateFilesSection verifies the ## Files section carries the
// workspace-as-file-browser conventions: tmp/ for scratch, no duplicate
// copies, build machinery stays put, and the deliver-vs-publish distinction.
func TestAgentsTemplateFilesSection(t *testing.T) {
	tpl, err := ReadTemplate(AgentsFile)
	if err != nil {
		t.Fatal("ReadTemplate failed:", err)
	}

	required := []string{
		"file browser the user sees",
		"`tmp/`",
		"never create `-v2`/`-final`/`(1)` copies",
		"Build machinery",
		"publish it (deploy) or save the FACTS to shared memory",
	}
	for _, want := range required {
		if !strings.Contains(tpl, want) {
			t.Errorf("AGENTS.md ## Files section missing expected content: %q", want)
		}
	}
}

// layoutBulletFor returns the Memory layout bullet line for the given file,
// i.e. the first line starting with "- `<file>`" (a definition bullet), or ""
// if none. This skips prose mentions of the same file elsewhere in the doc.
func layoutBulletFor(tpl, file string) string {
	prefix := "- `" + file + "`"
	for _, line := range strings.Split(tpl, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), prefix) {
			return line
		}
	}
	return ""
}
