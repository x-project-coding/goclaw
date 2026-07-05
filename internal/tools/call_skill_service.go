package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// maxSkillServiceResponseBytes caps the relayed body (mirrors jobs.go's guard).
const maxSkillServiceResponseBytes = 8 << 20 // 8 MiB

// CallSkillServiceTool lets an AI Employee invoke a named 42bucks skill-service
// operation via structured tool-calling instead of hand-writing curl/python.
//
// Everything fragile about the curl path is handled internally: the workspace
// token is minted server-side (mirroring internal/http/jobs.go) and never passes
// through the model's output, the identity headers are set from the run context,
// the base URL and path are assembled from the catalog, and the endpoint is
// reached from the goclaw process (no sandbox, so the curl-blocked job sandbox is
// irrelevant). The operation is an enum, so a route that does not exist cannot be
// named.
type CallSkillServiceTool struct {
	client *http.Client
}

// NewCallSkillServiceTool builds the tool with its own HTTP client.
func NewCallSkillServiceTool() *CallSkillServiceTool {
	return &CallSkillServiceTool{client: &http.Client{Timeout: 60 * time.Second}}
}

func (t *CallSkillServiceTool) Name() string { return "call_skill_service" }

func (t *CallSkillServiceTool) Description() string {
	return "Call a 42bucks skill-service operation. Prefer this over writing curl or python — " +
		"authentication, identity headers, and the base URL are handled for you, and you cannot " +
		"call an operation that does not exist. Pick an `operation` and pass its arguments as " +
		"`input`. Async operations return an id; poll it with the paired status operation.\n\n" +
		"Operations:\n" + catalogDescription()
}

func (t *CallSkillServiceTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"operation": map[string]any{
				"type":        "string",
				"description": "The skill-service operation to call (service.operation).",
				"enum":        catalogOperationIDs(),
			},
			"input": map[string]any{
				"type":                 "object",
				"description":          "Arguments for the operation; see the per-operation input fields in the description. Omit for operations that take no input.",
				"additionalProperties": true,
			},
		},
		"required": []string{"operation"},
	}
}

func (t *CallSkillServiceTool) Execute(ctx context.Context, args map[string]any) *Result {
	rc := store.RunContextFromCtx(ctx)
	if rc == nil {
		return ErrorResult("call_skill_service: no run context (cannot resolve the workspace).")
	}

	opID, _ := args["operation"].(string)
	op, ok := catalogLookup(opID)
	if !ok {
		return ErrorResult(fmt.Sprintf(
			"call_skill_service: unknown operation %q. Valid operations:\n%s",
			opID, catalogDescription()))
	}

	// `input` is optional (GET/no-arg ops). Accept a missing or null value.
	input := map[string]any{}
	if raw, present := args["input"]; present && raw != nil {
		m, isMap := raw.(map[string]any)
		if !isMap {
			return ErrorResult("call_skill_service: `input` must be an object.")
		}
		// Copy so path-param extraction does not mutate the caller's map.
		for k, v := range m {
			input[k] = v
		}
	}

	// Fill {placeholders} in the path from `input`, removing them from the body.
	path := op.Path
	for _, name := range op.PathParams {
		val, ok := input[name]
		s, isStr := val.(string)
		if !ok || !isStr || s == "" {
			return ErrorResult(fmt.Sprintf(
				"call_skill_service: operation %q requires a string `input.%s`.", op.ID, name))
		}
		path = strings.ReplaceAll(path, "{"+name+"}", url.PathEscape(s))
		delete(input, name)
	}

	fullURL := skillServiceBaseURL() + "/api/skill-services" + path

	var bodyReader io.Reader
	if op.Method == http.MethodGet || op.Method == http.MethodHead || op.Method == http.MethodDelete {
		// Remaining input becomes query params (none of the Phase 1 GET ops need
		// this today, but it keeps the tool honest for future catalog entries).
		if len(input) > 0 {
			q := url.Values{}
			for k, v := range input {
				q.Set(k, fmt.Sprintf("%v", v))
			}
			fullURL += "?" + q.Encode()
		}
	} else {
		payload, err := json.Marshal(input)
		if err != nil {
			return ErrorResult("call_skill_service: could not encode `input` as JSON.")
		}
		bodyReader = bytes.NewReader(payload)
	}

	req, err := http.NewRequestWithContext(ctx, op.Method, fullURL, bodyReader)
	if err != nil {
		return ErrorResult("call_skill_service: could not build the request.")
	}
	applySkillServiceHeaders(ctx, req, rc, op.Skill, bodyReader != nil)

	resp, err := t.client.Do(req)
	if err != nil {
		return ErrorResult(fmt.Sprintf("call_skill_service: request to %s failed: %v", op.ID, err))
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxSkillServiceResponseBytes))
	trimmed := strings.TrimSpace(string(body))

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if trimmed == "" {
			return NewResult(fmt.Sprintf(`{"data":{"ok":true},"http_status":%d}`, resp.StatusCode))
		}
		return NewResult(trimmed)
	}

	// Surface x-api's own {code,message} error verbatim so the model can correct.
	return ErrorResult(fmt.Sprintf("call_skill_service: %s returned HTTP %d: %s",
		op.ID, resp.StatusCode, trimmed))
}

// applySkillServiceHeaders mints the workspace token and sets auth + identity
// headers on a skill-service request. The token is read from the mint cache, never
// from anything the model supplied, so it cannot be spoofed or leaked into model
// output. Workspace id is the tenant UUID (never rc.Workspace — that path contains
// '/' and fails x-api's ^[A-Za-z0-9_-]+$ workspace-id regex).
func applySkillServiceHeaders(ctx context.Context, req *http.Request, rc *store.RunContext, skillSlug string, hasBody bool) {
	if key := WorkspaceSkillToken(ctx, rc.TenantID); key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	if rc.TenantID != uuid.Nil {
		req.Header.Set("X-Workspace-Id", rc.TenantID.String())
	}
	if rc.UserID != "" {
		req.Header.Set("X-User-Id", rc.UserID)
	}
	if rc.AgentKey != "" {
		req.Header.Set("X-Agent-Id", rc.AgentKey)
	}
	if skillSlug != "" {
		req.Header.Set("X-Skill-Slug", skillSlug)
	}
	if hasBody {
		req.Header.Set("Content-Type", "application/json")
	}
}
