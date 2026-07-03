package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bootstrap"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// importSeedAgentStore is a minimal AgentStore stub for archive-import baseline
// seeding tests. Only Create/GetByKey/GetAgentContextFiles/SetAgentContextFile/
// SetUserContextFile are exercised — everything else panics via the nil embedded
// interface, matching the idiom used by other stubs in this package.
type importSeedAgentStore struct {
	store.AgentStore
	agents map[uuid.UUID]*store.AgentData
	byKey  map[string]uuid.UUID
	files  map[uuid.UUID]map[string]string
}

func newImportSeedAgentStore() *importSeedAgentStore {
	return &importSeedAgentStore{
		agents: map[uuid.UUID]*store.AgentData{},
		byKey:  map[string]uuid.UUID{},
		files:  map[uuid.UUID]map[string]string{},
	}
}

func (s *importSeedAgentStore) Create(_ context.Context, agent *store.AgentData) error {
	if agent.ID == uuid.Nil {
		agent.ID = uuid.New()
	}
	cp := *agent
	s.agents[agent.ID] = &cp
	s.byKey[agent.AgentKey] = agent.ID
	return nil
}

func (s *importSeedAgentStore) GetByKey(_ context.Context, key string) (*store.AgentData, error) {
	id, ok := s.byKey[key]
	if !ok {
		return nil, nil
	}
	cp := *s.agents[id]
	return &cp, nil
}

func (s *importSeedAgentStore) GetAgentContextFiles(_ context.Context, agentID uuid.UUID) ([]store.AgentContextFileData, error) {
	var out []store.AgentContextFileData
	for name, content := range s.files[agentID] {
		out = append(out, store.AgentContextFileData{AgentID: agentID, FileName: name, Content: content})
	}
	return out, nil
}

func (s *importSeedAgentStore) SetAgentContextFile(_ context.Context, agentID uuid.UUID, fileName, content string) error {
	if s.files[agentID] == nil {
		s.files[agentID] = map[string]string{}
	}
	s.files[agentID][fileName] = content
	return nil
}

func (s *importSeedAgentStore) SetUserContextFile(_ context.Context, _ uuid.UUID, _, _, _ string) error {
	return nil
}

func importSeedTestContext() context.Context {
	ctx := context.Background()
	ctx = store.WithTenantID(ctx, uuid.New())
	ctx = store.WithUserID(ctx, "owner-1")
	return ctx
}

func newImportSeedRequest(agentKey, displayName string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/v1/agents/import", nil)
	req.Form = url.Values{"agent_key": {agentKey}, "display_name": {displayName}}
	return req
}

func predefinedAgentConfig() map[string]json.RawMessage {
	return map[string]json.RawMessage{
		"provider":   json.RawMessage(`"anthropic"`),
		"model":      json.RawMessage(`"claude-3-5-sonnet"`),
		"agent_type": json.RawMessage(`"predefined"`),
	}
}

// TestDoImportNewAgent_SeedsMissingBaselineFiles verifies that a new agent
// imported from an archive containing only SOUL/CAPABILITIES/IDENTITY ends up
// with AGENTS.md, AGENTS_CORE.md and AGENTS_TASK.md seeded from the embedded
// templates, and that the archive-provided SOUL.md content is left untouched.
func TestDoImportNewAgent_SeedsMissingBaselineFiles(t *testing.T) {
	agents := newImportSeedAgentStore()
	handler := &AgentsHandler{agents: agents, defaultWorkspace: "/tmp/workspace"}

	ctx := importSeedTestContext()
	req := newImportSeedRequest("brand-agent", "Brand Agent").WithContext(ctx)

	archiveSoul := "# Archive Soul\nCustom personality from brand archive."
	arc := &importArchive{
		agentConfig: predefinedAgentConfig(),
		contextFiles: []importContextFile{
			{fileName: bootstrap.SoulFile, content: archiveSoul},
			{fileName: bootstrap.CapabilitiesFile, content: "# Archive Capabilities"},
			{fileName: bootstrap.IdentityFile, content: "# Archive Identity"},
		},
	}

	summary, err := handler.doImportNewAgent(ctx, req, arc, nil)
	if err != nil {
		t.Fatalf("doImportNewAgent returned error: %v", err)
	}

	agentID := uuid.MustParse(summary.AgentID)
	files := agents.files[agentID]

	if got := files[bootstrap.SoulFile]; got != archiveSoul {
		t.Fatalf("archive SOUL.md was modified by seeding: got %q, want %q", got, archiveSoul)
	}

	for _, name := range []string{bootstrap.AgentsFile, bootstrap.AgentsCoreFile, bootstrap.AgentsTaskFile} {
		want, err := bootstrap.ReadTemplate(name)
		if err != nil {
			t.Fatalf("ReadTemplate(%s): %v", name, err)
		}
		got, ok := files[name]
		if !ok || got == "" {
			t.Fatalf("expected %s to be seeded after import, but it is missing", name)
		}
		if got != want {
			t.Fatalf("%s content mismatch: got %q, want embedded template %q", name, got, want)
		}
	}
}

// TestDoImportNewAgent_KeepsArchiveProvidedAgentsMd verifies that when the
// archive already ships its own AGENTS.md, baseline seeding must NOT overwrite
// it with the embedded template.
func TestDoImportNewAgent_KeepsArchiveProvidedAgentsMd(t *testing.T) {
	agents := newImportSeedAgentStore()
	handler := &AgentsHandler{agents: agents, defaultWorkspace: "/tmp/workspace"}

	ctx := importSeedTestContext()
	req := newImportSeedRequest("brand-agent-2", "Brand Agent Two").WithContext(ctx)

	archiveAgentsMd := "# Custom AGENTS.md\nArchive-provided rules that must survive import."
	arc := &importArchive{
		agentConfig: predefinedAgentConfig(),
		contextFiles: []importContextFile{
			{fileName: bootstrap.SoulFile, content: "# Soul"},
			{fileName: bootstrap.AgentsFile, content: archiveAgentsMd},
		},
	}

	summary, err := handler.doImportNewAgent(ctx, req, arc, nil)
	if err != nil {
		t.Fatalf("doImportNewAgent returned error: %v", err)
	}

	agentID := uuid.MustParse(summary.AgentID)
	got := agents.files[agentID][bootstrap.AgentsFile]
	if got != archiveAgentsMd {
		t.Fatalf("archive-provided AGENTS.md was overwritten by seeding: got %q, want %q", got, archiveAgentsMd)
	}
}

// TestDoImportNewAgent_SeedsBaselineWithZeroContextFiles verifies that baseline
// seeding still runs when the archive contains no context_files section entries
// at all (e.g. a minimal/legacy export).
func TestDoImportNewAgent_SeedsBaselineWithZeroContextFiles(t *testing.T) {
	agents := newImportSeedAgentStore()
	handler := &AgentsHandler{agents: agents, defaultWorkspace: "/tmp/workspace"}

	ctx := importSeedTestContext()
	req := newImportSeedRequest("brand-agent-3", "Brand Agent Three").WithContext(ctx)

	arc := &importArchive{
		agentConfig: predefinedAgentConfig(),
	}

	summary, err := handler.doImportNewAgent(ctx, req, arc, nil)
	if err != nil {
		t.Fatalf("doImportNewAgent returned error: %v", err)
	}

	agentID := uuid.MustParse(summary.AgentID)
	files := agents.files[agentID]
	for _, name := range []string{bootstrap.AgentsFile, bootstrap.AgentsCoreFile, bootstrap.AgentsTaskFile} {
		if files[name] == "" {
			t.Fatalf("expected %s to be seeded when archive has zero context files", name)
		}
	}
}

// TestDoMergeImport_SeedsMissingBaselineOnExistingAgent verifies that merging
// an archive into an existing agent that is missing AGENTS.md causes it to be
// seeded, while leaving the agent's existing SOUL.md untouched.
func TestDoMergeImport_SeedsMissingBaselineOnExistingAgent(t *testing.T) {
	agents := newImportSeedAgentStore()
	ctx := importSeedTestContext()

	existing := &store.AgentData{
		AgentKey:  "existing-brand-agent",
		AgentType: store.AgentTypePredefined,
		Provider:  "anthropic",
		Model:     "claude-3-5-sonnet",
	}
	if err := agents.Create(ctx, existing); err != nil {
		t.Fatalf("seed existing agent: %v", err)
	}
	agents.files[existing.ID] = map[string]string{
		bootstrap.SoulFile: "# Existing Soul\nDo not touch.",
	}

	handler := &AgentsHandler{agents: agents, defaultWorkspace: "/tmp/workspace"}

	arc := &importArchive{}
	sections := map[string]bool{"context_files": true}

	if _, err := handler.doMergeImport(ctx, existing, arc, sections, nil); err != nil {
		t.Fatalf("doMergeImport returned error: %v", err)
	}

	files := agents.files[existing.ID]
	if files[bootstrap.SoulFile] != "# Existing Soul\nDo not touch." {
		t.Fatalf("existing SOUL.md was modified by seeding: %q", files[bootstrap.SoulFile])
	}
	if files[bootstrap.AgentsFile] == "" {
		t.Fatal("expected AGENTS.md to be seeded on merge-import for an existing agent missing it")
	}
}
