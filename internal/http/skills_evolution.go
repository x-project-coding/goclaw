package http

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/skills"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

type skillEvolutionPatchRequest struct {
	Enabled *bool  `json:"enabled"`
	Mode    string `json:"mode"`
}

type skillSuggestionCreateRequest struct {
	SuggestionType string          `json:"suggestion_type"`
	Reason         string          `json:"reason"`
	Evidence       json.RawMessage `json:"evidence"`
	DraftPatch     json.RawMessage `json:"draft_patch"`
	TargetFile     string          `json:"target_file"`
}

type skillSuggestionApplyRequest struct {
	Approve bool `json:"approve"`
}

type skillDraftPatch struct {
	Find    string  `json:"find"`
	Replace string  `json:"replace"`
	Content *string `json:"content"`
}

func (h *SkillsHandler) evolutionConfigured(w http.ResponseWriter) bool {
	if h.evolutionStore == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "skill evolution store not configured"})
		return false
	}
	return true
}

func (h *SkillsHandler) skillIDFromRequest(w http.ResponseWriter, r *http.Request) (uuid.UUID, store.SkillInfo, bool) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid skill id"})
		return uuid.Nil, store.SkillInfo{}, false
	}
	info, ok := h.skills.GetSkillByID(r.Context(), id)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "skill not found"})
		return uuid.Nil, store.SkillInfo{}, false
	}
	return id, info, true
}

func (h *SkillsHandler) handleGetEvolution(w http.ResponseWriter, r *http.Request) {
	if !h.evolutionConfigured(w) {
		return
	}
	skillID, _, ok := h.skillIDFromRequest(w, r)
	if !ok {
		return
	}
	settings, err := h.evolutionStore.GetSettings(r.Context(), skillID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, settings)
}

func (h *SkillsHandler) handlePatchEvolution(w http.ResponseWriter, r *http.Request) {
	if !h.evolutionConfigured(w) {
		return
	}
	skillID, _, ok := h.skillIDFromRequest(w, r)
	if !ok {
		return
	}
	var req skillEvolutionPatchRequest
	if !bindJSON(w, r, store.LocaleFromContext(r.Context()), &req) {
		return
	}
	current, err := h.evolutionStore.GetSettings(r.Context(), skillID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	enabled := current.Enabled
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	mode := strings.TrimSpace(req.Mode)
	if mode == "" {
		mode = current.Mode
	}
	if mode == "" {
		mode = store.SkillEvolutionModeSuggestOnly
	}
	if mode != store.SkillEvolutionModeSuggestOnly && mode != store.SkillEvolutionModeAutoAnalyze {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid evolution mode"})
		return
	}
	updated, err := h.evolutionStore.UpsertSettings(r.Context(), store.SkillEvolutionSettings{
		SkillID: skillID,
		Enabled: enabled,
		Mode:    mode,
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	action := "skill.evolve.mode_updated"
	if req.Enabled != nil {
		if enabled {
			action = "skill.evolve.enabled"
		} else {
			action = "skill.evolve.disabled"
		}
	}
	h.logSkillActivity(r, action, skillID, map[string]any{"enabled": enabled, "mode": mode})
	writeJSON(w, http.StatusOK, updated)
}

func (h *SkillsHandler) handleGetSkillMetrics(w http.ResponseWriter, r *http.Request) {
	if !h.evolutionConfigured(w) {
		return
	}
	skillID, _, ok := h.skillIDFromRequest(w, r)
	if !ok {
		return
	}
	var since *time.Time
	if raw := strings.TrimSpace(r.URL.Query().Get("since")); raw != "" {
		if t, err := time.Parse(time.RFC3339, raw); err == nil {
			since = &t
		}
	}
	stats, err := h.evolutionStore.AggregateUsage(r.Context(), skillID, since)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if !permissions.HasMinRole(resolveAuth(r).Role, permissions.RoleAdmin) {
		stats.TopFailureReasons = nil
	}
	writeJSON(w, http.StatusOK, stats)
}

func (h *SkillsHandler) handleGetSkillActivity(w http.ResponseWriter, r *http.Request) {
	if h.activityStore == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "activity store not configured"})
		return
	}
	skillID, _, ok := h.skillIDFromRequest(w, r)
	if !ok {
		return
	}
	items, err := h.activityStore.List(r.Context(), store.ActivityListOpts{
		EntityType: "skill",
		EntityID:   skillID.String(),
		Limit:      clampLimit(r.URL.Query().Get("limit"), 50, 200),
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"activity": items})
}

func (h *SkillsHandler) handleListSkillSuggestions(w http.ResponseWriter, r *http.Request) {
	if !h.evolutionConfigured(w) {
		return
	}
	skillID, _, ok := h.skillIDFromRequest(w, r)
	if !ok {
		return
	}
	items, err := h.evolutionStore.ListSuggestions(r.Context(), skillID, r.URL.Query().Get("status"), clampLimit(r.URL.Query().Get("limit"), 50, 200))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if !permissions.HasMinRole(resolveAuth(r).Role, permissions.RoleAdmin) {
		for i := range items {
			items[i].Evidence = nil
			items[i].DraftPatch = nil
			items[i].CreatedByActorID = ""
			items[i].ReviewedByActorID = ""
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"suggestions": items})
}

func (h *SkillsHandler) handleCreateSkillSuggestion(w http.ResponseWriter, r *http.Request) {
	if !h.evolutionConfigured(w) {
		return
	}
	skillID, _, ok := h.skillIDFromRequest(w, r)
	if !ok {
		return
	}
	var req skillSuggestionCreateRequest
	if !bindJSON(w, r, store.LocaleFromContext(r.Context()), &req) {
		return
	}
	if strings.TrimSpace(req.SuggestionType) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "suggestion_type is required"})
		return
	}
	target := strings.TrimSpace(req.TargetFile)
	if target != "" {
		clean, err := skills.ValidateSkillTargetPath(target, true)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		target = clean
	}
	created, err := h.evolutionStore.CreateSuggestion(r.Context(), store.SkillImprovementSuggestion{
		SkillID:            skillID,
		SuggestionType:     req.SuggestionType,
		Reason:             req.Reason,
		Evidence:           req.Evidence,
		DraftPatch:         req.DraftPatch,
		TargetFile:         target,
		CreatedByActorType: "user",
		CreatedByActorID:   store.ActorIDFromContext(r.Context()),
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	h.logSkillActivity(r, "skill.suggestion.created", skillID, map[string]any{"suggestion_id": created.ID.String(), "type": created.SuggestionType})
	writeJSON(w, http.StatusCreated, created)
}

func (h *SkillsHandler) handleApproveSkillSuggestion(w http.ResponseWriter, r *http.Request) {
	h.handleSuggestionStatus(w, r, store.SkillSuggestionStatusApproved, "skill.suggestion.approved")
}

func (h *SkillsHandler) handleRejectSkillSuggestion(w http.ResponseWriter, r *http.Request) {
	h.handleSuggestionStatus(w, r, store.SkillSuggestionStatusRejected, "skill.suggestion.rejected")
}

func (h *SkillsHandler) handleSuggestionStatus(w http.ResponseWriter, r *http.Request, status, action string) {
	if !h.evolutionConfigured(w) {
		return
	}
	skillID, _, ok := h.skillIDFromRequest(w, r)
	if !ok {
		return
	}
	suggestionID, err := uuid.Parse(r.PathValue("suggestionID"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid suggestion id"})
		return
	}
	sg, err := h.evolutionStore.GetSuggestion(r.Context(), suggestionID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if sg == nil || sg.SkillID != skillID {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "suggestion not found"})
		return
	}
	updated, err := h.evolutionStore.UpdateSuggestionStatus(r.Context(), suggestionID, status, "user", store.ActorIDFromContext(r.Context()))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	h.logSkillActivity(r, action, skillID, map[string]any{"suggestion_id": suggestionID.String()})
	writeJSON(w, http.StatusOK, updated)
}

func (h *SkillsHandler) handleApplySkillSuggestion(w http.ResponseWriter, r *http.Request) {
	if !h.evolutionConfigured(w) {
		return
	}
	skillID, info, ok := h.skillIDFromRequest(w, r)
	if !ok {
		return
	}
	if info.IsSystem {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "system skill mutation is blocked"})
		return
	}
	suggestionID, err := uuid.Parse(r.PathValue("suggestionID"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid suggestion id"})
		return
	}
	var req skillSuggestionApplyRequest
	_ = json.NewDecoder(r.Body).Decode(&req)
	sg, err := h.evolutionStore.GetSuggestion(r.Context(), suggestionID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if sg == nil || sg.SkillID != skillID {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "suggestion not found"})
		return
	}
	if sg.Status == store.SkillSuggestionStatusApplied && sg.AppliedVersion != nil {
		writeJSON(w, http.StatusOK, sg)
		return
	}
	if sg.Status != store.SkillSuggestionStatusApproved {
		if !req.Approve {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "suggestion must be approved before apply"})
			return
		}
		sg, err = h.evolutionStore.UpdateSuggestionStatus(r.Context(), suggestionID, store.SkillSuggestionStatusApproved, "user", store.ActorIDFromContext(r.Context()))
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
	}
	applied, err := h.applySkillSuggestionPatch(r, skillID, sg)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, applied)
}

func (h *SkillsHandler) applySkillSuggestionPatch(r *http.Request, skillID uuid.UUID, sg *store.SkillImprovementSuggestion) (*store.SkillImprovementSuggestion, error) {
	currentDir, slug, oldVersion, isSystem, ok := h.skills.GetSkillFilePath(r.Context(), skillID)
	if !ok {
		return nil, fmt.Errorf("skill not found")
	}
	if isSystem {
		return nil, fmt.Errorf("system skill mutation is blocked")
	}
	target := sg.TargetFile
	if strings.TrimSpace(target) == "" {
		target = "SKILL.md"
	}
	cleanTarget, err := skills.ValidateSkillTargetPath(target, true)
	if err != nil {
		return nil, err
	}
	var patch skillDraftPatch
	if len(sg.DraftPatch) > 0 {
		if err := json.Unmarshal(sg.DraftPatch, &patch); err != nil {
			return nil, fmt.Errorf("invalid draft_patch: %w", err)
		}
	}
	if patch.Content == nil && patch.Find == "" {
		return nil, fmt.Errorf("draft_patch requires content or find/replace")
	}
	newVersion, commitLock, err := h.skills.GetNextVersionLocked(r.Context(), slug)
	if err != nil {
		return nil, err
	}
	defer commitLock() //nolint:errcheck

	destDir := filepath.Join(h.tenantSkillsDir(r), slug, fmt.Sprintf("%d", newVersion))
	tmpDir := destDir + ".tmp-" + uuid.NewString()
	if err := copyDir(currentDir, tmpDir); err != nil {
		return nil, fmt.Errorf("stage version: %w", err)
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.RemoveAll(tmpDir)
			_ = os.RemoveAll(destDir)
		}
	}()

	targetPath := filepath.Join(tmpDir, filepath.FromSlash(cleanTarget))
	var nextContent string
	if patch.Content != nil {
		nextContent = *patch.Content
	} else {
		currentBytes, err := os.ReadFile(targetPath)
		if err != nil {
			return nil, fmt.Errorf("read target file: %w", err)
		}
		nextContent = string(currentBytes)
		replaced := strings.Replace(nextContent, patch.Find, patch.Replace, 1)
		if replaced == nextContent {
			return nil, fmt.Errorf("find text not found in target file")
		}
		nextContent = replaced
	}
	if cleanTarget == "SKILL.md" {
		violations, safe := skills.GuardSkillContent(nextContent)
		if !safe {
			return nil, errors.New(skills.FormatGuardViolations(violations))
		}
	}
	if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
		return nil, fmt.Errorf("create target directory: %w", err)
	}
	if err := os.WriteFile(targetPath, []byte(nextContent), 0644); err != nil {
		return nil, fmt.Errorf("write target file: %w", err)
	}
	if err := os.Rename(tmpDir, destDir); err != nil {
		return nil, fmt.Errorf("commit version files: %w", err)
	}

	hash, size, err := hashSkillDir(destDir)
	if err != nil {
		return nil, err
	}
	if err := h.skills.UpdateSkill(r.Context(), skillID, map[string]any{
		"version":    newVersion,
		"file_path":  destDir,
		"file_size":  size,
		"file_hash":  &hash,
		"updated_at": time.Now(),
	}); err != nil {
		return nil, fmt.Errorf("update skill: %w", err)
	}
	changedFiles, _ := json.Marshal([]string{cleanTarget})
	if _, err := h.evolutionStore.CreateSkillVersion(r.Context(), store.SkillVersion{
		SkillID:                 skillID,
		Version:                 newVersion,
		ContentHash:             hash,
		ChangedFiles:            changedFiles,
		CreatedByActorType:      "user",
		CreatedByActorID:        store.ActorIDFromContext(r.Context()),
		CreatedFromSuggestionID: &sg.ID,
	}); err != nil {
		return nil, fmt.Errorf("record skill version: %w", err)
	}
	applied, err := h.evolutionStore.MarkSuggestionApplied(r.Context(), sg.ID, newVersion, "user", store.ActorIDFromContext(r.Context()))
	if err != nil {
		return nil, err
	}
	h.logSkillActivity(r, "skill.suggestion.applied", skillID, map[string]any{
		"suggestion_id": sg.ID.String(),
		"changed_files": []string{cleanTarget},
		"old_version":   oldVersion,
		"new_version":   newVersion,
		"content_hash":  hash,
	})
	cleanup = false
	return applied, nil
}

func (h *SkillsHandler) logSkillActivity(r *http.Request, action string, skillID uuid.UUID, details map[string]any) {
	if h.activityStore == nil {
		return
	}
	raw, _ := json.Marshal(details)
	_ = h.activityStore.Log(r.Context(), &store.ActivityLog{
		ActorType:  "user",
		ActorID:    store.ActorIDFromContext(r.Context()),
		Action:     action,
		EntityType: "skill",
		EntityID:   skillID.String(),
		Details:    raw,
		IPAddress:  r.RemoteAddr,
	})
}

func clampLimit(raw string, fallback, max int) int {
	if raw == "" {
		return fallback
	}
	var n int
	if _, err := fmt.Sscanf(raw, "%d", &n); err != nil || n <= 0 {
		return fallback
	}
	if n > max {
		return max
	}
	return n
}

func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
		if err != nil {
			return err
		}
		defer out.Close()
		_, err = io.Copy(out, in)
		return err
	})
}

func hashSkillDir(dir string) (string, int64, error) {
	var size int64
	h := sha256.New()
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		size += info.Size()
		rel, _ := filepath.Rel(dir, path)
		h.Write([]byte(filepath.ToSlash(rel)))
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		h.Write(data)
		return nil
	})
	if err != nil {
		return "", 0, err
	}
	return fmt.Sprintf("%x", h.Sum(nil)), size, nil
}
