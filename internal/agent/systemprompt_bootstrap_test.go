package agent

import (
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/bootstrap"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// TestBuildSystemPrompt_BootstrapStates verifies the 4 bootstrap states
// produce the correct system prompt sections.
func TestBuildSystemPrompt_BootstrapStates(t *testing.T) {
	blankUserMD := "# USER.md\n\n- **Name:**\n- **Language:**\n- **Timezone:**\n"
	populatedUserMD := "# USER.md\n\n- **Name:** Alice\n- **Language:** English\n- **Timezone:** UTC+7\n"

	tests := []struct {
		name       string
		cfg        SystemPromptConfig
		wantIn     string // substring that MUST appear
		wantNotIn  string // substring that MUST NOT appear (empty = skip check)
	}{
		{
			name: "open agent with BOOTSTRAP.md → FIRST RUN slim mode",
			cfg: SystemPromptConfig{
				IsBootstrap: true,
				AgentType:   store.AgentTypeOpen,
				ContextFiles: []bootstrap.ContextFile{
					{Path: bootstrap.BootstrapFile, Content: "# BOOTSTRAP"},
					{Path: bootstrap.UserFile, Content: blankUserMD},
				},
				ToolNames: []string{"write_file", "Write"},
			},
			wantIn:    "## FIRST RUN",
			wantNotIn: "USER PROFILE INCOMPLETE",
		},
		{
			name: "predefined agent with BOOTSTRAP.md → NO onboarding section",
			cfg: SystemPromptConfig{
				IsBootstrap: false,
				AgentType:   store.AgentTypePredefined,
				ContextFiles: []bootstrap.ContextFile{
					{Path: bootstrap.BootstrapFile, Content: "# BOOTSTRAP"},
					{Path: bootstrap.UserFile, Content: blankUserMD},
				},
				ToolNames: []string{"write_file", "Write", "skill_search"},
			},
			wantNotIn: "FIRST RUN",
		},
		{
			name: "predefined agent + blank USER.md → NO USER PROFILE INCOMPLETE",
			cfg: SystemPromptConfig{
				IsBootstrap: false,
				AgentType:   store.AgentTypePredefined,
				ContextFiles: []bootstrap.ContextFile{
					{Path: bootstrap.UserFile, Content: blankUserMD},
				},
				ToolNames: []string{"write_file"},
			},
			wantNotIn: "USER PROFILE INCOMPLETE",
		},
		{
			name: "predefined agent + populated USER.md → no nudge at all",
			cfg: SystemPromptConfig{
				IsBootstrap: false,
				AgentType:   store.AgentTypePredefined,
				ContextFiles: []bootstrap.ContextFile{
					{Path: bootstrap.UserFile, Content: populatedUserMD},
				},
				ToolNames: []string{"write_file"},
			},
			wantNotIn: "FIRST RUN",
		},
		{
			name: "open agent slim mode has write_file note",
			cfg: SystemPromptConfig{
				IsBootstrap: true,
				AgentType:   store.AgentTypeOpen,
				ContextFiles: []bootstrap.ContextFile{
					{Path: bootstrap.BootstrapFile, Content: "# BOOTSTRAP"},
				},
				ToolNames: []string{"write_file"},
			},
			wantIn: "only have write_file available",
		},
		{
			name: "predefined agent first run → NO get-to-know onboarding copy",
			cfg: SystemPromptConfig{
				IsBootstrap: false,
				AgentType:   store.AgentTypePredefined,
				ContextFiles: []bootstrap.ContextFile{
					{Path: bootstrap.BootstrapFile, Content: "# BOOTSTRAP"},
				},
				ToolNames: []string{"write_file", "web_search"},
			},
			wantNotIn: "GET TO KNOW THE USER",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prompt := BuildSystemPrompt(tt.cfg)

			if tt.wantIn != "" && !strings.Contains(prompt, tt.wantIn) {
				t.Errorf("expected %q in system prompt, got:\n%s", tt.wantIn, prompt[:min(len(prompt), 500)])
			}
			if tt.wantNotIn != "" && strings.Contains(prompt, tt.wantNotIn) {
				t.Errorf("unexpected %q in system prompt", tt.wantNotIn)
			}

			// Always verify: populated USER.md must never trigger INCOMPLETE
			if tt.name == "predefined agent + populated USER.md → no nudge at all" {
				if strings.Contains(prompt, "USER PROFILE INCOMPLETE") {
					t.Error("populated USER.md should not trigger USER PROFILE INCOMPLETE")
				}
			}
		})
	}
}

// TestBuildSystemPrompt_PredefinedNoOnboarding is the guard test for the predefined
// onboarding removal. Predefined (42bucks brand) agents no longer ask name/timezone/
// language on first run — user identity comes from the external user-info skill — so the
// prompt must NEVER contain the old onboarding sections, regardless of whether a
// BOOTSTRAP.md / blank USER.md is present in context.
func TestBuildSystemPrompt_PredefinedNoOnboarding(t *testing.T) {
	blankUserMD := "# USER.md\n\n- **Name:**\n- **Language:**\n- **Timezone:**\n"
	forbidden := []string{
		"FIRST RUN",
		"GET TO KNOW THE USER",
		"USER PROFILE INCOMPLETE",
	}

	cases := []struct {
		name string
		cfg  SystemPromptConfig
	}{
		{
			name: "full mode, no context files",
			cfg: SystemPromptConfig{
				AgentType: store.AgentTypePredefined,
				ToolNames: []string{"write_file", "web_search"},
			},
		},
		{
			name: "with BOOTSTRAP.md present",
			cfg: SystemPromptConfig{
				AgentType: store.AgentTypePredefined,
				ContextFiles: []bootstrap.ContextFile{
					{Path: bootstrap.BootstrapFile, Content: "# BOOTSTRAP"},
				},
				ToolNames: []string{"write_file", "web_search"},
			},
		},
		{
			name: "with blank USER.md present",
			cfg: SystemPromptConfig{
				AgentType: store.AgentTypePredefined,
				ContextFiles: []bootstrap.ContextFile{
					{Path: bootstrap.UserFile, Content: blankUserMD},
				},
				ToolNames: []string{"write_file"},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			prompt := BuildSystemPrompt(tc.cfg)
			for _, f := range forbidden {
				if strings.Contains(prompt, f) {
					t.Errorf("predefined prompt must not contain %q", f)
				}
			}
		})
	}
}

// TestBuildSystemPrompt_PredefinedBootstrapTrueNoOnboarding proves the real
// removal: even with IsBootstrap=true AND a BOOTSTRAP.md / blank USER.md present
// (the exact state that used to trigger predefined onboarding), the prompt must
// NOT contain the removed predefined-onboarding copy. Unlike the IsBootstrap=false
// cases, this does not pass trivially via the "## FIRST RUN" gate — the OPEN
// "## FIRST RUN — MANDATORY" block may still render here, which is expected, so we
// only assert the two removed predefined-onboarding phrases are absent.
func TestBuildSystemPrompt_PredefinedBootstrapTrueNoOnboarding(t *testing.T) {
	blankUserMD := "# USER.md\n\n- **Name:**\n- **Language:**\n- **Timezone:**\n"
	cfg := SystemPromptConfig{
		IsBootstrap: true,
		AgentType:   store.AgentTypePredefined,
		ContextFiles: []bootstrap.ContextFile{
			{Path: bootstrap.BootstrapFile, Content: "# BOOTSTRAP"},
			{Path: bootstrap.UserFile, Content: blankUserMD},
		},
		ToolNames: []string{"write_file", "web_search"},
	}
	prompt := BuildSystemPrompt(cfg)
	for _, f := range []string{"GET TO KNOW THE USER", "USER PROFILE INCOMPLETE"} {
		if strings.Contains(prompt, f) {
			t.Errorf("predefined prompt (IsBootstrap=true) must not contain %q", f)
		}
	}
}

// TestBuildSystemPrompt_OpenBootstrapUnchanged verifies Phase 04 did NOT
// touch the open-agent slim branch — its existing mandate copy stays.
func TestBuildSystemPrompt_OpenBootstrapUnchanged(t *testing.T) {
	cfg := SystemPromptConfig{
		IsBootstrap: true,
		AgentType:   store.AgentTypeOpen,
		ContextFiles: []bootstrap.ContextFile{
			{Path: bootstrap.BootstrapFile, Content: "# BOOTSTRAP"},
		},
		ToolNames: []string{"write_file"},
	}
	prompt := BuildSystemPrompt(cfg)
	if !strings.Contains(prompt, "Do NOT give a generic greeting") {
		t.Error("open-bootstrap branch must keep its existing mandate copy")
	}
	if !strings.Contains(prompt, "only have write_file available") {
		t.Error("open-bootstrap branch must keep its tool-limit note")
	}
}

// TestBuildSystemPrompt_NoBootstrapNoUser verifies that when there are no
// bootstrap-related files at all, no nudge sections appear.
func TestBuildSystemPrompt_NoBootstrapNoUser(t *testing.T) {
	prompt := BuildSystemPrompt(SystemPromptConfig{
		AgentType: store.AgentTypePredefined,
		ToolNames: []string{"write_file"},
	})

	if strings.Contains(prompt, "FIRST RUN") {
		t.Error("unexpected FIRST RUN section with no context files")
	}
	if strings.Contains(prompt, "USER PROFILE INCOMPLETE") {
		t.Error("unexpected USER PROFILE INCOMPLETE section with no context files")
	}
}
