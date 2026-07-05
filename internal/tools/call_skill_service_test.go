package tools

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// runCtx returns a context carrying a minimal RunContext for the tool.
func runCtx() context.Context {
	return store.WithRunContext(context.Background(), &store.RunContext{
		TenantID: uuid.MustParse("00000000-0000-0000-0000-0000000000aa"),
		UserID:   "user-1",
		AgentKey: "samantha",
	})
}

func TestCallSkillService_Parameters_EnumIsCatalog(t *testing.T) {
	params := NewCallSkillServiceTool().Parameters()
	props := params["properties"].(map[string]any)
	enum := props["operation"].(map[string]any)["enum"].([]string)
	if len(enum) != len(skillServiceCatalog) {
		t.Fatalf("enum size %d != catalog size %d", len(enum), len(skillServiceCatalog))
	}
	// The enum must be exactly the catalog ids (no invented routes reachable).
	got := map[string]bool{}
	for _, id := range enum {
		got[id] = true
	}
	for _, op := range skillServiceCatalog {
		if !got[op.ID] {
			t.Fatalf("catalog op %q missing from tool enum", op.ID)
		}
	}
}

func TestCallSkillService_UnknownOperation(t *testing.T) {
	res := NewCallSkillServiceTool().Execute(runCtx(), map[string]any{"operation": "does.not-exist"})
	if !res.IsError {
		t.Fatal("expected an error result for an unknown operation")
	}
	if !strings.Contains(res.ForLLM, "unknown operation") {
		t.Fatalf("expected 'unknown operation' guidance, got: %s", res.ForLLM)
	}
}

func TestCallSkillService_NoRunContext(t *testing.T) {
	res := NewCallSkillServiceTool().Execute(context.Background(), map[string]any{"operation": "manage-skills.catalog"})
	if !res.IsError || !strings.Contains(res.ForLLM, "run context") {
		t.Fatalf("expected a no-run-context error, got: %+v", res)
	}
}

func TestCallSkillService_MissingPathParam(t *testing.T) {
	res := NewCallSkillServiceTool().Execute(runCtx(), map[string]any{
		"operation": "media-forge.job",
		"input":     map[string]any{}, // missing required id
	})
	if !res.IsError || !strings.Contains(res.ForLLM, "input.id") {
		t.Fatalf("expected a missing-path-param error naming input.id, got: %s", res.ForLLM)
	}
}

// TestCallSkillService_PostRoundTrip drives a real HTTP call against a stub that
// stands in for x-api, asserting the auth + identity headers, the assembled path,
// and the JSON body — and that the {data} envelope is returned verbatim.
func TestCallSkillService_PostRoundTrip(t *testing.T) {
	var gotPath, gotAuth, gotWs, gotSkill, gotAgent string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotWs = r.Header.Get("X-Workspace-Id")
		gotSkill = r.Header.Get("X-Skill-Slug")
		gotAgent = r.Header.Get("X-Agent-Id")
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":{"slug":"demo","alreadyConnected":false,"ok":true}}`))
	}))
	defer srv.Close()
	t.Setenv("X_API_BASE_URL", srv.URL)

	res := NewCallSkillServiceTool().Execute(runCtx(), map[string]any{
		"operation": "manage-skills.connect",
		"input":     map[string]any{"slug": "competitor-watch-abc123"},
	})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.ForLLM)
	}
	if gotPath != "/api/skill-services/manage-skills/connect" {
		t.Fatalf("wrong path: %s", gotPath)
	}
	if gotWs != "00000000-0000-0000-0000-0000000000aa" {
		t.Fatalf("workspace id must be the tenant UUID, got %q", gotWs)
	}
	if gotSkill != "manage-skills" {
		t.Fatalf("X-Skill-Slug = %q, want manage-skills", gotSkill)
	}
	if gotAgent != "samantha" {
		t.Fatalf("X-Agent-Id = %q, want samantha", gotAgent)
	}
	// No mint store wired in the unit test → no Authorization header (graceful).
	if gotAuth != "" && !strings.HasPrefix(gotAuth, "Bearer ") {
		t.Fatalf("unexpected Authorization header: %q", gotAuth)
	}
	if gotBody["slug"] != "competitor-watch-abc123" {
		t.Fatalf("body slug not forwarded, got %v", gotBody["slug"])
	}
	if !strings.Contains(res.ForLLM, `"ok":true`) {
		t.Fatalf("data envelope not returned verbatim: %s", res.ForLLM)
	}
}

func TestCallSkillService_PathParamRoundTrip(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":{"id":"job-9","status":"DONE"}}`))
	}))
	defer srv.Close()
	t.Setenv("X_API_BASE_URL", srv.URL)

	res := NewCallSkillServiceTool().Execute(runCtx(), map[string]any{
		"operation": "media-forge.job",
		"input":     map[string]any{"id": "job-9"},
	})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.ForLLM)
	}
	if gotPath != "/api/skill-services/media-forge/job/job-9" {
		t.Fatalf("path param not interpolated: %s", gotPath)
	}
}

func TestCallSkillService_SurfacesUpstreamError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"error":{"code":"SKILL_DESCRIPTION_REQUIRED","message":"A description is required."}}`))
	}))
	defer srv.Close()
	t.Setenv("X_API_BASE_URL", srv.URL)

	res := NewCallSkillServiceTool().Execute(runCtx(), map[string]any{
		"operation": "manage-skills.publish",
		"input":     map[string]any{"slug": "x", "files": map[string]any{"SKILL.md": "# x"}},
	})
	if !res.IsError {
		t.Fatal("expected an error result for a 422")
	}
	if !strings.Contains(res.ForLLM, "SKILL_DESCRIPTION_REQUIRED") {
		t.Fatalf("upstream error code not surfaced: %s", res.ForLLM)
	}
}
