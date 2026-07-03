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
