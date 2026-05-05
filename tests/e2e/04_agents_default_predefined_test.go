//go:build e2e

// Wire-contract tests for the predefined-only agent kind in v4. After the
// purge, POST /v1/agents must:
//   - succeed without an agent_type field (defaults to predefined),
//   - reject any payload that supplies agent_type (no silent mapping),
//   - never emit agent_type in GET responses.
//
// RED until Phase 03 lands the new handler behavior.
package e2e_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/tests/e2e/helpers"
)

// minimalAgentBody is the post-purge canonical create request: no agent_type.
func minimalAgentBody() map[string]any {
	return map[string]any{
		"agent_key": "test-" + helpers.RandHex8(),
		"model":     "test/test-model",
		"provider":  "openai",
	}
}

// TestAgentCreateDefaultPredefined: omitting agent_type yields 201.
func TestAgentCreateDefaultPredefined(t *testing.T) {
	helpers.MustMigrateClean(t)
	helpers.ResetDB(t)

	gw := helpers.StartGateway(t)
	api := helpers.NewAPIClient()
	api.BaseURL = gw.BaseURL

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	api.SetToken(loginForAgents(t, ctx, api, helpers.RootEmail(), helpers.RootPassword()))

	res, err := api.POST(ctx, "/v1/agents", minimalAgentBody())
	mustOKAgents(t, "POST /v1/agents (no agent_type)", res, err, http.StatusCreated)

	var got agentResp
	mustJSONAgents(t, res, &got)
	if got.ID == "" || got.AgentKey == "" {
		t.Fatalf("expected id+agent_key in response, got %+v", got)
	}
}

// TestAgentCreateRejectsAgentType: any agent_type value returns 400.
func TestAgentCreateRejectsAgentType(t *testing.T) {
	helpers.MustMigrateClean(t)
	helpers.ResetDB(t)

	gw := helpers.StartGateway(t)
	api := helpers.NewAPIClient()
	api.BaseURL = gw.BaseURL

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	api.SetToken(loginForAgents(t, ctx, api, helpers.RootEmail(), helpers.RootPassword()))

	for _, value := range []string{"open", "predefined", "shared"} {
		value := value
		t.Run(value, func(t *testing.T) {
			body := minimalAgentBody()
			body["agent_type"] = value
			res, err := api.POST(ctx, "/v1/agents", body)
			if err != nil {
				t.Fatalf("transport: %v", err)
			}
			if res.Status != http.StatusBadRequest {
				t.Fatalf("expected 400 for agent_type=%q, got %d body=%s",
					value, res.Status, string(res.Body))
			}
		})
	}
}

// TestAgentResponseHasNoAgentType: GET response JSON must not include the field.
func TestAgentResponseHasNoAgentType(t *testing.T) {
	helpers.MustMigrateClean(t)
	helpers.ResetDB(t)

	gw := helpers.StartGateway(t)
	api := helpers.NewAPIClient()
	api.BaseURL = gw.BaseURL

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	api.SetToken(loginForAgents(t, ctx, api, helpers.RootEmail(), helpers.RootPassword()))

	body := minimalAgentBody()
	knownKey := body["agent_key"].(string)
	res, err := api.POST(ctx, "/v1/agents", body)
	mustOKAgents(t, "POST /v1/agents", res, err, http.StatusCreated)

	res, err = api.GET(ctx, "/v1/agents")
	mustOKAgents(t, "GET /v1/agents", res, err, http.StatusOK)

	// Decode permissively (array or {"agents":[...]} envelope) and assert no
	// agent in the payload exposes the agent_type key.
	var raw []map[string]any
	if err := json.Unmarshal(res.Body, &raw); err != nil {
		var obj struct {
			Agents []map[string]any `json:"agents"`
		}
		if err2 := json.Unmarshal(res.Body, &obj); err2 != nil {
			t.Fatalf("decode list: %v / %v body=%s", err, err2, string(res.Body))
		}
		raw = obj.Agents
	}

	matched := false
	for _, a := range raw {
		if a["agent_key"] == knownKey {
			matched = true
		}
		if _, ok := a["agent_type"]; ok {
			t.Fatalf("agent payload must not include agent_type; got %v", a)
		}
	}
	if !matched {
		t.Fatalf("did not find created agent %q in list response: %s",
			knownKey, strings.TrimSpace(string(res.Body)))
	}
}
