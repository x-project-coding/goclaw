package http

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bootstrap"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// updateSeedAgentStore is a minimal AgentStore stub for PUT /v1/agents/{id}
// predefined-transition seeding tests. Only GetByID/Update/GetAgentContextFiles/
// SetAgentContextFile are exercised — everything else panics via the nil
// embedded interface, matching the idiom used by importSeedAgentStore in
// agents_import_seed_test.go.
type updateSeedAgentStore struct {
	store.AgentStore
	agents map[uuid.UUID]*store.AgentData
	files  map[uuid.UUID]map[string]string
}

func newUpdateSeedAgentStore() *updateSeedAgentStore {
	return &updateSeedAgentStore{
		agents: map[uuid.UUID]*store.AgentData{},
		files:  map[uuid.UUID]map[string]string{},
	}
}

func (s *updateSeedAgentStore) GetByID(_ context.Context, id uuid.UUID) (*store.AgentData, error) {
	ag, ok := s.agents[id]
	if !ok {
		return nil, fmt.Errorf("agent %s not found", id)
	}
	cp := *ag
	return &cp, nil
}

func (s *updateSeedAgentStore) Update(_ context.Context, id uuid.UUID, updates map[string]any) error {
	ag, ok := s.agents[id]
	if !ok {
		return fmt.Errorf("agent %s not found", id)
	}
	if v, ok := updates["agent_type"].(string); ok {
		ag.AgentType = v
	}
	if v, ok := updates["display_name"].(string); ok {
		ag.DisplayName = v
	}
	return nil
}

func (s *updateSeedAgentStore) GetAgentContextFiles(_ context.Context, agentID uuid.UUID) ([]store.AgentContextFileData, error) {
	var out []store.AgentContextFileData
	for name, content := range s.files[agentID] {
		out = append(out, store.AgentContextFileData{AgentID: agentID, FileName: name, Content: content})
	}
	return out, nil
}

func (s *updateSeedAgentStore) SetAgentContextFile(_ context.Context, agentID uuid.UUID, fileName, content string) error {
	if s.files[agentID] == nil {
		s.files[agentID] = map[string]string{}
	}
	s.files[agentID][fileName] = content
	return nil
}

// newAgentUpdateRequest builds a PUT /v1/agents/{id} request with the given
// JSON body, a tenant-scoped context matching agentTenant, and the {id} path
// value wired via SetPathValue (no mux needed — handleUpdate is called directly).
func newAgentUpdateRequest(id uuid.UUID, agentTenant uuid.UUID, body string) *http.Request {
	req := httptest.NewRequest(http.MethodPut, "/v1/agents/"+id.String(), strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", id.String())
	ctx := store.WithTenantID(req.Context(), agentTenant)
	ctx = store.WithUserID(ctx, "admin-1")
	return req.WithContext(ctx)
}

// TestHandleUpdate_OpenToPredefinedTransition_SeedsMissingBaselineFiles verifies
// the production bug fix: PUT /v1/agents/{id} flipping agent_type from 'open'
// (the state 42bucks provisioning archives import brand agents as) to
// 'predefined' must seed the missing AGENTS.md/AGENTS_CORE.md/AGENTS_TASK.md
// baseline files, mirroring the seed-on-import behavior in doMergeImport.
func TestHandleUpdate_OpenToPredefinedTransition_SeedsMissingBaselineFiles(t *testing.T) {
	agents := newUpdateSeedAgentStore()
	tenantID := uuid.New()
	agentID := uuid.New()
	agents.agents[agentID] = &store.AgentData{
		BaseModel: store.BaseModel{ID: agentID},
		TenantID:  tenantID,
		AgentKey:  "brand-agent",
		AgentType: store.AgentTypeOpen,
		Provider:  "anthropic",
		Model:     "claude-3-5-sonnet",
	}

	handler := &AgentsHandler{agents: agents}
	req := newAgentUpdateRequest(agentID, tenantID, `{"agent_type":"predefined"}`)
	w := httptest.NewRecorder()

	handler.handleUpdate(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	files := agents.files[agentID]
	for _, name := range []string{bootstrap.AgentsFile, bootstrap.AgentsCoreFile, bootstrap.AgentsTaskFile} {
		want, err := bootstrap.ReadTemplate(name)
		if err != nil {
			t.Fatalf("ReadTemplate(%s): %v", name, err)
		}
		got, ok := files[name]
		if !ok || got == "" {
			t.Fatalf("expected %s to be seeded after open->predefined transition, but it is missing", name)
		}
		if got != want {
			t.Fatalf("%s content mismatch: got %q, want embedded template %q", name, got, want)
		}
	}

	if agents.agents[agentID].AgentType != store.AgentTypePredefined {
		t.Fatalf("expected agent_type to be updated to predefined, got %q", agents.agents[agentID].AgentType)
	}
}

// TestHandleUpdate_AlreadyPredefined_DoesNotDuplicateOrOverwriteFiles verifies
// idempotency: PUT-ing agent_type=predefined on an agent that is already
// predefined (a no-op transition) must not touch existing baseline files —
// SeedToStore's only-if-missing semantics plus the "previous type != predefined"
// guard in handleUpdate together make this a no-op re-seed.
func TestHandleUpdate_AlreadyPredefined_DoesNotDuplicateOrOverwriteFiles(t *testing.T) {
	agents := newUpdateSeedAgentStore()
	tenantID := uuid.New()
	agentID := uuid.New()
	agents.agents[agentID] = &store.AgentData{
		BaseModel: store.BaseModel{ID: agentID},
		TenantID:  tenantID,
		AgentKey:  "already-predefined-agent",
		AgentType: store.AgentTypePredefined,
		Provider:  "anthropic",
		Model:     "claude-3-5-sonnet",
	}
	customAgentsMd := "# Custom AGENTS.md\nHand-edited rules that must survive the update."
	agents.files[agentID] = map[string]string{
		bootstrap.AgentsFile: customAgentsMd,
	}

	handler := &AgentsHandler{agents: agents}
	req := newAgentUpdateRequest(agentID, tenantID, `{"agent_type":"predefined"}`)
	w := httptest.NewRecorder()

	handler.handleUpdate(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	if got := agents.files[agentID][bootstrap.AgentsFile]; got != customAgentsMd {
		t.Fatalf("AGENTS.md was overwritten by a redundant predefined->predefined update: got %q, want %q", got, customAgentsMd)
	}
	// AGENTS_CORE.md/AGENTS_TASK.md were never seeded for this agent and must
	// stay absent — no seeding call should have fired at all for this transition.
	for _, name := range []string{bootstrap.AgentsCoreFile, bootstrap.AgentsTaskFile} {
		if _, ok := agents.files[agentID][name]; ok {
			t.Fatalf("expected %s to remain unseeded on a predefined->predefined update, but it exists", name)
		}
	}
}

// TestHandleUpdate_StaysOpen_SeedsNothing verifies that an update which leaves
// (or sets) agent_type as 'open' never triggers baseline seeding — SeedToStore
// intentionally no-ops for open agents, which is the root cause this fix works
// around only for the open->predefined transition, not for open agents generally.
func TestHandleUpdate_StaysOpen_SeedsNothing(t *testing.T) {
	agents := newUpdateSeedAgentStore()
	tenantID := uuid.New()
	agentID := uuid.New()
	agents.agents[agentID] = &store.AgentData{
		BaseModel: store.BaseModel{ID: agentID},
		TenantID:  tenantID,
		AgentKey:  "open-agent",
		AgentType: store.AgentTypeOpen,
		Provider:  "anthropic",
		Model:     "claude-3-5-sonnet",
	}

	handler := &AgentsHandler{agents: agents}
	req := newAgentUpdateRequest(agentID, tenantID, `{"agent_type":"open"}`)
	w := httptest.NewRecorder()

	handler.handleUpdate(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	if len(agents.files[agentID]) != 0 {
		t.Fatalf("expected no baseline files to be seeded for an agent staying 'open', got %v", agents.files[agentID])
	}
}
