package http

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func TestApplySkillSuggestionPatchCreatesNewReferenceFile(t *testing.T) {
	handler, skillStore, ctx, root := newTestUploadHandler(t)
	evolution := &skillEvolutionStoreStub{}
	handler.SetEvolutionStore(evolution, nil)

	currentDir := filepath.Join(root, "skills-store", "reference-skill", "1")
	if err := os.MkdirAll(currentDir, 0755); err != nil {
		t.Fatalf("mkdir current skill: %v", err)
	}
	if err := os.WriteFile(filepath.Join(currentDir, "SKILL.md"), []byte(skillMarkdown("Reference Skill", "reference-skill")), 0644); err != nil {
		t.Fatalf("write skill: %v", err)
	}
	skillID := skillStore.seedCustomSkill("reference-skill", currentDir, "active", nil)
	content := "Use the documented query syntax.\n"
	patch, err := json.Marshal(skillDraftPatch{Content: &content})
	if err != nil {
		t.Fatalf("marshal draft patch: %v", err)
	}
	suggestion := &store.SkillImprovementSuggestion{
		ID:             uuid.New(),
		SkillID:        skillID,
		TargetFile:     "references/troubleshooting.md",
		DraftPatch:     patch,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
		SkillSlug:      "reference-skill",
		Status:         store.SkillSuggestionStatusApproved,
		SuggestionType: "skill_reference_add",
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/skills/"+skillID.String()+"/evolution/suggestions/"+suggestion.ID.String()+"/apply", nil).WithContext(ctx)

	applied, err := handler.applySkillSuggestionPatch(req, skillID, suggestion)
	if err != nil {
		t.Fatalf("apply suggestion: %v", err)
	}
	if applied.Status != store.SkillSuggestionStatusApplied {
		t.Fatalf("status = %q, want applied", applied.Status)
	}

	newReference := filepath.Join(root, "skills-store", "reference-skill", "2", "references", "troubleshooting.md")
	got, err := os.ReadFile(newReference)
	if err != nil {
		t.Fatalf("read created reference: %v", err)
	}
	if string(got) != content {
		t.Fatalf("created reference = %q, want %q", got, content)
	}
	if len(evolution.versions) != 1 || evolution.versions[0].Version != 2 {
		t.Fatalf("versions = %+v, want one version 2", evolution.versions)
	}
}

func TestHandleSuggestionStatusDoesNotUpdateDifferentSkillSuggestion(t *testing.T) {
	handler, skillStore, ctx, root := newTestUploadHandler(t)
	requestSkillID := skillStore.seedCustomSkill("request-skill", filepath.Join(root, "skills-store", "request-skill", "1"), "active", nil)
	ownerSkillID := skillStore.seedCustomSkill("owner-skill", filepath.Join(root, "skills-store", "owner-skill", "1"), "active", nil)
	suggestionID := uuid.New()
	evolution := &skillEvolutionStoreStub{
		suggestions: map[uuid.UUID]*store.SkillImprovementSuggestion{
			suggestionID: {
				ID:             suggestionID,
				SkillID:        ownerSkillID,
				Status:         store.SkillSuggestionStatusPending,
				SuggestionType: "skill_reference_add",
			},
		},
	}
	handler.SetEvolutionStore(evolution, nil)

	req := httptest.NewRequest(http.MethodPost, "/v1/skills/"+requestSkillID.String()+"/evolution/suggestions/"+suggestionID.String()+"/approve", nil).WithContext(ctx)
	req.SetPathValue("id", requestSkillID.String())
	req.SetPathValue("suggestionID", suggestionID.String())
	w := httptest.NewRecorder()

	handler.handleApproveSkillSuggestion(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, body = %s, want 404", w.Code, w.Body.String())
	}
	if evolution.statusUpdates != 0 {
		t.Fatalf("status updates = %d, want 0", evolution.statusUpdates)
	}
	if got := evolution.suggestions[suggestionID].Status; got != store.SkillSuggestionStatusPending {
		t.Fatalf("suggestion status = %q, want pending", got)
	}
}

type skillEvolutionStoreStub struct {
	versions      []store.SkillVersion
	suggestions   map[uuid.UUID]*store.SkillImprovementSuggestion
	statusUpdates int
}

func (s *skillEvolutionStoreStub) GetSettings(context.Context, uuid.UUID) (*store.SkillEvolutionSettings, error) {
	return nil, nil
}

func (s *skillEvolutionStoreStub) UpsertSettings(context.Context, store.SkillEvolutionSettings) (*store.SkillEvolutionSettings, error) {
	return nil, nil
}

func (s *skillEvolutionStoreStub) RecordUsage(context.Context, store.SkillUsageMetric) error {
	return nil
}

func (s *skillEvolutionStoreStub) AggregateUsage(context.Context, uuid.UUID, *time.Time) (*store.SkillUsageStats, error) {
	return nil, nil
}

func (s *skillEvolutionStoreStub) ListUsage(context.Context, uuid.UUID, int) ([]store.SkillUsageMetric, error) {
	return nil, nil
}

func (s *skillEvolutionStoreStub) CreateSuggestion(context.Context, store.SkillImprovementSuggestion) (*store.SkillImprovementSuggestion, error) {
	return nil, nil
}

func (s *skillEvolutionStoreStub) ListSuggestions(context.Context, uuid.UUID, string, int) ([]store.SkillImprovementSuggestion, error) {
	return nil, nil
}

func (s *skillEvolutionStoreStub) GetSuggestion(_ context.Context, id uuid.UUID) (*store.SkillImprovementSuggestion, error) {
	if s.suggestions == nil {
		return nil, nil
	}
	sg, ok := s.suggestions[id]
	if !ok {
		return nil, nil
	}
	copied := *sg
	return &copied, nil
}

func (s *skillEvolutionStoreStub) UpdateSuggestionStatus(_ context.Context, id uuid.UUID, status, actorType, actorID string) (*store.SkillImprovementSuggestion, error) {
	s.statusUpdates++
	if s.suggestions == nil {
		return nil, fmt.Errorf("suggestion not found")
	}
	sg, ok := s.suggestions[id]
	if !ok {
		return nil, fmt.Errorf("suggestion not found")
	}
	sg.Status = status
	sg.ReviewedByActorType = actorType
	sg.ReviewedByActorID = actorID
	now := time.Now()
	sg.ReviewedAt = &now
	return sg, nil
}

func (s *skillEvolutionStoreStub) MarkSuggestionApplied(_ context.Context, id uuid.UUID, version int, actorType, actorID string) (*store.SkillImprovementSuggestion, error) {
	return &store.SkillImprovementSuggestion{
		ID:                  id,
		Status:              store.SkillSuggestionStatusApplied,
		ReviewedByActorType: actorType,
		ReviewedByActorID:   actorID,
		AppliedVersion:      &version,
	}, nil
}

func (s *skillEvolutionStoreStub) CreateSkillVersion(_ context.Context, version store.SkillVersion) (*store.SkillVersion, error) {
	s.versions = append(s.versions, version)
	return &version, nil
}

func (s *skillEvolutionStoreStub) ListSkillVersions(context.Context, uuid.UUID, int) ([]store.SkillVersion, error) {
	return s.versions, nil
}

func (s *skillEvolutionStoreStub) GetSkillVersion(context.Context, uuid.UUID, int) (*store.SkillVersion, error) {
	return nil, nil
}
