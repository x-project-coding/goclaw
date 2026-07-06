package agent

// Tests for the per-skill operation gating of call_skill_service in
// buildFilteredTools: the operation enum/description are pruned to the agent's
// accessible skills (skillAllowList ∪ pinnedSkills); nil allow-list keeps the
// full catalog; an agent with none of the catalog's skills loses the tool.

import (
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/skillcatalog"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// skillGateExecutor serves the real call_skill_service def plus a bystander.
type skillGateExecutor struct {
	stubExecutor
}

func (s *skillGateExecutor) ProviderDefs() []providers.ToolDefinition {
	return []providers.ToolDefinition{
		tools.ToProviderDef(metadataTestTool{name: "bystander"}),
		tools.ToProviderDef(tools.NewCallSkillServiceTool()),
	}
}

func buildSkillGateLoop(allowList, pinned []string) *Loop {
	return &Loop{
		provider:       &stubProvider{},
		tools:          &skillGateExecutor{},
		skillEvolve:    true,
		skillAllowList: allowList,
		pinnedSkills:   pinned,
	}
}

func findDef(defs []providers.ToolDefinition, name string) *providers.ToolDefinition {
	for i := range defs {
		if defs[i].Function != nil && defs[i].Function.Name == name {
			return &defs[i]
		}
	}
	return nil
}

func enumOf(t *testing.T, td *providers.ToolDefinition) []string {
	t.Helper()
	props, ok := td.Function.Parameters["properties"].(map[string]any)
	if !ok {
		t.Fatal("parameters missing properties")
	}
	op, ok := props["operation"].(map[string]any)
	if !ok {
		t.Fatal("parameters missing operation")
	}
	enum, ok := op["enum"].([]string)
	if !ok {
		t.Fatalf("enum has unexpected type %T", op["enum"])
	}
	return enum
}

func TestSkillOpGate_NilAllowList_FullCatalog(t *testing.T) {
	l := buildSkillGateLoop(nil, nil)
	defs, _, _ := l.buildFilteredTools(&RunRequest{}, false, 1, 10, nil)

	td := findDef(defs, "call_skill_service")
	if td == nil {
		t.Fatal("call_skill_service missing with nil allow-list")
	}
	if got := enumOf(t, td); len(got) != len(skillcatalog.Catalog) {
		t.Fatalf("nil allow-list should keep the full catalog: got %d ops, want %d", len(got), len(skillcatalog.Catalog))
	}
}

func TestSkillOpGate_PrunesToGrantedSkills(t *testing.T) {
	l := buildSkillGateLoop([]string{"manage-view", "research"}, nil)
	defs, _, _ := l.buildFilteredTools(&RunRequest{}, false, 1, 10, nil)

	td := findDef(defs, "call_skill_service")
	if td == nil {
		t.Fatal("call_skill_service missing")
	}
	for _, id := range enumOf(t, td) {
		if !strings.HasPrefix(id, "manage-view.") && !strings.HasPrefix(id, "research.") {
			t.Fatalf("enum leaked gated operation %q", id)
		}
	}
	if strings.Contains(td.Function.Description, "manage-skills.publish") {
		t.Fatal("description leaked a gated operation")
	}
	if bys := findDef(defs, "bystander"); bys == nil {
		t.Fatal("gating removed an unrelated tool")
	}
}

func TestSkillOpGate_PinnedSkillsCount(t *testing.T) {
	// Allow-list empty but a catalog skill pinned → its ops stay visible.
	l := buildSkillGateLoop([]string{}, []string{"deploy"})
	defs, _, _ := l.buildFilteredTools(&RunRequest{}, false, 1, 10, nil)

	td := findDef(defs, "call_skill_service")
	if td == nil {
		t.Fatal("call_skill_service missing with a pinned catalog skill")
	}
	enum := enumOf(t, td)
	if len(enum) == 0 {
		t.Fatal("pinned catalog skill produced an empty enum")
	}
	for _, id := range enum {
		if !strings.HasPrefix(id, "deploy.") {
			t.Fatalf("enum leaked non-deploy operation %q", id)
		}
	}
}

func TestSkillOpGate_NoCatalogSkills_DropsTool(t *testing.T) {
	l := buildSkillGateLoop([]string{"brainstorming", "writing-plans"}, nil)
	defs, _, _ := l.buildFilteredTools(&RunRequest{}, false, 1, 10, nil)

	if td := findDef(defs, "call_skill_service"); td != nil {
		t.Fatal("tool should be dropped when the agent has none of the catalog's skills")
	}
	if bys := findDef(defs, "bystander"); bys == nil {
		t.Fatal("unrelated tool must survive")
	}
}
