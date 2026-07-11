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
	if len(enum) != len(skillServiceCatalog()) {
		t.Fatalf("enum size %d != catalog size %d", len(enum), len(skillServiceCatalog()))
	}
	// The enum must be exactly the catalog ids (no invented routes reachable).
	got := map[string]bool{}
	for _, id := range enum {
		got[id] = true
	}
	for _, op := range skillServiceCatalog() {
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

// ─── FilterCallSkillServiceDef (per-skill operation gating) ────────────────

func TestFilterCallSkillServiceDef_NarrowsEnumAndDescription(t *testing.T) {
	td := ToProviderDef(NewCallSkillServiceTool())
	allowed := map[string]bool{"manage-skills": true}

	if !FilterCallSkillServiceDef(&td, allowed) {
		t.Fatal("expected the def to survive with manage-skills allowed")
	}
	enum := td.Function.Parameters["properties"].(map[string]any)["operation"].(map[string]any)["enum"].([]string)
	for _, id := range enum {
		if !strings.HasPrefix(id, "manage-skills.") {
			t.Fatalf("gated enum leaked %q", id)
		}
	}
	if len(enum) == 0 {
		t.Fatal("enum unexpectedly empty")
	}
	if !strings.Contains(td.Function.Description, "manage-skills.catalog") {
		t.Fatal("description missing an allowed operation")
	}
	if strings.Contains(td.Function.Description, "research.search") {
		t.Fatal("description leaked a gated operation")
	}
	if !strings.HasPrefix(td.Function.Description, callSkillServicePreamble[:40]) {
		t.Fatal("description lost the static preamble")
	}
}

func TestFilterCallSkillServiceDef_NoOpsLeft(t *testing.T) {
	td := ToProviderDef(NewCallSkillServiceTool())
	if FilterCallSkillServiceDef(&td, map[string]bool{"brainstorming": true}) {
		t.Fatal("expected false when the agent has none of the catalog's skills")
	}
}

func TestFilterCallSkillServiceDef_DoesNotMutateToolSchema(t *testing.T) {
	tool := NewCallSkillServiceTool()
	td := ToProviderDef(tool)
	if !FilterCallSkillServiceDef(&td, map[string]bool{"deploy": true}) {
		t.Fatal("filter unexpectedly dropped the def")
	}
	// A fresh def from the tool must still carry the FULL enum — gating one
	// agent's definition must never poison the shared tool.
	fresh := tool.Parameters()
	enum := fresh["properties"].(map[string]any)["operation"].(map[string]any)["enum"].([]string)
	if len(enum) != len(skillServiceCatalog()) {
		t.Fatalf("tool schema mutated: enum now %d ops, want %d", len(enum), len(skillServiceCatalog()))
	}
}

// ─── Session-key auto-fill + origin headers ────────────────────────────────

func TestCallSkillService_AutoFillsSessionKey(t *testing.T) {
	var gotBody map[string]any
	var gotOriginKind, gotOriginID string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotOriginKind = r.Header.Get("X-Origin-Kind")
		gotOriginID = r.Header.Get("X-Origin-Id")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_, _ = w.Write([]byte(`{"data":{"ok":true}}`))
	}))
	defer srv.Close()
	t.Setenv("X_API_BASE_URL", srv.URL)

	ctx := WithToolSessionKey(runCtx(), "agent:test:ws:direct:sk-123")
	res := NewCallSkillServiceTool().Execute(ctx, map[string]any{
		"operation": "manage-view.set",
		"input":     map[string]any{"hints": map[string]any{}}, // sessionKey omitted
	})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.ForLLM)
	}
	if gotBody["sessionKey"] != "agent:test:ws:direct:sk-123" {
		t.Fatalf("sessionKey not auto-filled: %v", gotBody["sessionKey"])
	}
	if gotOriginKind != "chat_session" {
		t.Fatalf("X-Origin-Kind = %q", gotOriginKind)
	}
	if gotOriginID != "agent:test:ws:direct:sk-123" {
		t.Fatalf("X-Origin-Id = %q", gotOriginID)
	}
}

func TestCallSkillService_ModelProvidedSessionKeyWins(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_, _ = w.Write([]byte(`{"data":{"ok":true}}`))
	}))
	defer srv.Close()
	t.Setenv("X_API_BASE_URL", srv.URL)

	ctx := WithToolSessionKey(runCtx(), "auto-key")
	res := NewCallSkillServiceTool().Execute(ctx, map[string]any{
		"operation": "manage-view.set",
		"input":     map[string]any{"sessionKey": "explicit-key", "hints": map[string]any{}},
	})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.ForLLM)
	}
	if gotBody["sessionKey"] != "explicit-key" {
		t.Fatalf("explicit sessionKey overwritten: %v", gotBody["sessionKey"])
	}
}

func TestCallSkillService_NoSessionKeyFillForOpsWithoutIt(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_, _ = w.Write([]byte(`{"data":{"ok":true}}`))
	}))
	defer srv.Close()
	t.Setenv("X_API_BASE_URL", srv.URL)

	ctx := WithToolSessionKey(runCtx(), "auto-key")
	res := NewCallSkillServiceTool().Execute(ctx, map[string]any{
		"operation": "research.search",
		"input":     map[string]any{"query": "x"},
	})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.ForLLM)
	}
	if _, present := gotBody["sessionKey"]; present {
		t.Fatal("sessionKey injected into an operation that does not take it")
	}
}
