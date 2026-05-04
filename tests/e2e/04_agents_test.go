//go:build e2e

// Package e2e_test exercises /v1/agents/* CRUD (internal/http/agents.go):
// create open/predefined, list, get, update via PUT, delete, and per-user
// share grants. Foundational — sessions, memory, and vault tests depend on
// agents existing.
package e2e_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/tests/e2e/helpers"
)

// mustOKAgents / mustJSONAgents are file-local helpers (same pattern as auth test).
func mustOKAgents(t *testing.T, label string, res *helpers.APIResponse, err error, want int) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: transport error: %v", label, err)
	}
	if res.Status != want {
		t.Fatalf("%s: status %d, want %d, body=%s", label, res.Status, want, string(res.Body))
	}
}

func mustJSONAgents(t *testing.T, res *helpers.APIResponse, out any) {
	t.Helper()
	if err := res.JSON(out); err != nil {
		t.Fatalf("decode JSON: %v body=%s", err, string(res.Body))
	}
}

// loginForAgents is a local helper: POST /v1/auth/login → access token.
func loginForAgents(t *testing.T, ctx context.Context, api *helpers.APIClient, email, pass string) string {
	t.Helper()
	res, err := api.POST(ctx, "/v1/auth/login", map[string]string{
		"email": email, "password": pass,
	})
	mustOKAgents(t, "POST /v1/auth/login", res, err, http.StatusOK)
	var tok struct {
		AccessToken string `json:"access_token"`
	}
	mustJSONAgents(t, res, &tok)
	if tok.AccessToken == "" {
		t.Fatalf("loginForAgents %s: empty access_token", email)
	}
	return tok.AccessToken
}

// createAgentBody returns the minimum required body for POST /v1/agents.
// agent_key must be a valid slug (lowercase, hyphens only).
func createAgentBody(agentType string) map[string]any {
	return map[string]any{
		"agent_key":  "test-" + helpers.RandHex8(),
		"agent_type": agentType,
		"model":      "test/test-model",
		"provider":   "openai",
	}
}

// agentResp holds the minimal fields returned by /v1/agents/*.
type agentResp struct {
	ID       string `json:"id"`
	AgentKey string `json:"agent_key"`
	Status   string `json:"status"`
}

// TestAgentCreateOpen — POST /v1/agents with agent_type=open → 201, id + agent_key present.
// Note: the handler normalises "open" → "predefined" (v3 compat), but still returns 201.
func TestAgentCreateOpen(t *testing.T) {
	helpers.MustMigrateClean(t)
	helpers.ResetDB(t)

	gw := helpers.StartGateway(t)
	api := helpers.NewAPIClient()
	api.BaseURL = gw.BaseURL

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	token := loginForAgents(t, ctx, api, helpers.RootEmail(), helpers.RootPassword())
	api.SetToken(token)

	body := createAgentBody("open")
	res, err := api.POST(ctx, "/v1/agents", body)
	mustOKAgents(t, "POST /v1/agents (open)", res, err, http.StatusCreated)

	var agent agentResp
	mustJSONAgents(t, res, &agent)
	if agent.ID == "" {
		t.Fatalf("create open: empty id")
	}
	if agent.AgentKey == "" {
		t.Fatalf("create open: empty agent_key")
	}
}

// TestAgentCreatePredefined — POST /v1/agents with agent_type=predefined → 201.
func TestAgentCreatePredefined(t *testing.T) {
	helpers.MustMigrateClean(t)
	helpers.ResetDB(t)

	gw := helpers.StartGateway(t)
	api := helpers.NewAPIClient()
	api.BaseURL = gw.BaseURL

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	token := loginForAgents(t, ctx, api, helpers.RootEmail(), helpers.RootPassword())
	api.SetToken(token)

	body := createAgentBody("predefined")
	res, err := api.POST(ctx, "/v1/agents", body)
	mustOKAgents(t, "POST /v1/agents (predefined)", res, err, http.StatusCreated)

	var agent agentResp
	mustJSONAgents(t, res, &agent)
	if agent.ID == "" {
		t.Fatalf("create predefined: empty id")
	}
}

// TestAgentList — GET /v1/agents returns at least the agent we just created.
func TestAgentList(t *testing.T) {
	helpers.MustMigrateClean(t)
	helpers.ResetDB(t)

	gw := helpers.StartGateway(t)
	api := helpers.NewAPIClient()
	api.BaseURL = gw.BaseURL

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	token := loginForAgents(t, ctx, api, helpers.RootEmail(), helpers.RootPassword())
	api.SetToken(token)

	// Create a known agent.
	body := createAgentBody("predefined")
	knownKey := body["agent_key"].(string)
	res, err := api.POST(ctx, "/v1/agents", body)
	mustOKAgents(t, "POST /v1/agents", res, err, http.StatusCreated)

	res, err = api.GET(ctx, "/v1/agents")
	mustOKAgents(t, "GET /v1/agents", res, err, http.StatusOK)

	// The response may be a JSON array or {"agents":[...]} object — handle both.
	raw := res.Body
	found := false
	// Try array form first.
	var arrResp []agentResp
	if err2 := res.JSON(&arrResp); err2 == nil {
		for _, a := range arrResp {
			if a.AgentKey == knownKey {
				found = true
				break
			}
		}
	} else {
		// Try object form.
		var objResp struct {
			Agents []agentResp `json:"agents"`
		}
		if err3 := res.JSON(&objResp); err3 == nil {
			for _, a := range objResp.Agents {
				if a.AgentKey == knownKey {
					found = true
					break
				}
			}
		}
	}
	if !found {
		t.Fatalf("agent list: created key %q not found in response: %s", knownKey, string(raw))
	}
}

// TestAgentGet — GET /v1/agents/{id} returns full payload including agent_key.
func TestAgentGet(t *testing.T) {
	helpers.MustMigrateClean(t)
	helpers.ResetDB(t)

	gw := helpers.StartGateway(t)
	api := helpers.NewAPIClient()
	api.BaseURL = gw.BaseURL

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	token := loginForAgents(t, ctx, api, helpers.RootEmail(), helpers.RootPassword())
	api.SetToken(token)

	body := createAgentBody("predefined")
	res, err := api.POST(ctx, "/v1/agents", body)
	mustOKAgents(t, "POST /v1/agents", res, err, http.StatusCreated)
	var created agentResp
	mustJSONAgents(t, res, &created)

	res, err = api.GET(ctx, fmt.Sprintf("/v1/agents/%s", created.ID))
	mustOKAgents(t, "GET /v1/agents/{id}", res, err, http.StatusOK)

	var fetched agentResp
	mustJSONAgents(t, res, &fetched)
	if fetched.ID != created.ID {
		t.Fatalf("get: id mismatch: got %q want %q", fetched.ID, created.ID)
	}
	if fetched.AgentKey != created.AgentKey {
		t.Fatalf("get: agent_key mismatch: got %q want %q", fetched.AgentKey, created.AgentKey)
	}
}

// TestAgentPatch — PUT /v1/agents/{id} to update display_name → 200; GET reflects update.
func TestAgentPatch(t *testing.T) {
	helpers.MustMigrateClean(t)
	helpers.ResetDB(t)

	gw := helpers.StartGateway(t)
	api := helpers.NewAPIClient()
	api.BaseURL = gw.BaseURL

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	token := loginForAgents(t, ctx, api, helpers.RootEmail(), helpers.RootPassword())
	api.SetToken(token)

	body := createAgentBody("predefined")
	res, err := api.POST(ctx, "/v1/agents", body)
	mustOKAgents(t, "POST /v1/agents", res, err, http.StatusCreated)
	var created agentResp
	mustJSONAgents(t, res, &created)

	// PUT (agents handler uses PUT /v1/agents/{id} for update, not PATCH).
	newName := "Updated-" + helpers.RandHex8()
	res, err = api.Do(ctx, "PUT", fmt.Sprintf("/v1/agents/%s", created.ID), map[string]any{
		"display_name": newName,
	})
	mustOKAgents(t, "PUT /v1/agents/{id}", res, err, http.StatusOK)

	// GET reflects update.
	res, err = api.GET(ctx, fmt.Sprintf("/v1/agents/%s", created.ID))
	mustOKAgents(t, "GET /v1/agents/{id} after update", res, err, http.StatusOK)
	var updated struct {
		DisplayName string `json:"display_name"`
	}
	mustJSONAgents(t, res, &updated)
	if updated.DisplayName != newName {
		t.Fatalf("update: display_name=%q want %q", updated.DisplayName, newName)
	}
}

// TestAgentDelete — DELETE /v1/agents/{id} → 200 ("ok": "true"); GET → 404.
func TestAgentDelete(t *testing.T) {
	helpers.MustMigrateClean(t)
	helpers.ResetDB(t)

	gw := helpers.StartGateway(t)
	api := helpers.NewAPIClient()
	api.BaseURL = gw.BaseURL

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	token := loginForAgents(t, ctx, api, helpers.RootEmail(), helpers.RootPassword())
	api.SetToken(token)

	body := createAgentBody("predefined")
	res, err := api.POST(ctx, "/v1/agents", body)
	mustOKAgents(t, "POST /v1/agents", res, err, http.StatusCreated)
	var created agentResp
	mustJSONAgents(t, res, &created)

	res, err = api.DELETE(ctx, fmt.Sprintf("/v1/agents/%s", created.ID))
	mustOKAgents(t, "DELETE /v1/agents/{id}", res, err, http.StatusOK)

	res, err = api.GET(ctx, fmt.Sprintf("/v1/agents/%s", created.ID))
	if err != nil {
		t.Fatalf("GET after delete: %v", err)
	}
	if res.Status != http.StatusNotFound {
		t.Fatalf("GET after delete: status %d, want 404", res.Status)
	}
}

// TestAgentDeleteCascadesContext — DELETE /v1/agents/{id} removes the agent and all
// rows in agent_context_files that reference it (ON DELETE CASCADE).
func TestAgentDeleteCascadesContext(t *testing.T) {
	helpers.MustMigrateClean(t)
	helpers.ResetDB(t)

	gw := helpers.StartGateway(t)
	api := helpers.NewAPIClient()
	api.BaseURL = gw.BaseURL

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	token := loginForAgents(t, ctx, api, helpers.RootEmail(), helpers.RootPassword())
	api.SetToken(token)

	// Create an agent.
	body := createAgentBody("open")
	res, err := api.POST(ctx, "/v1/agents", body)
	mustOKAgents(t, "POST /v1/agents", res, err, http.StatusCreated)
	var created agentResp
	mustJSONAgents(t, res, &created)

	// Insert a context file row directly via DB (no HTTP endpoint for agent_context_files).
	db := helpers.MustDB(t)
	_, dbErr := db.ExecContext(ctx,
		`INSERT INTO agent_context_files (agent_id, file_name, content, created_at, updated_at)
		 VALUES ($1, 'e2e-cascade-test.md', 'hello cascade', now(), now())`,
		created.ID,
	)
	if dbErr != nil {
		t.Skipf("insert agent_context_files: schema may differ: %v", dbErr)
	}

	// Confirm the row exists before delete.
	var countBefore int
	if err := db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM agent_context_files WHERE agent_id = $1", created.ID,
	).Scan(&countBefore); err != nil {
		t.Fatalf("count before delete: %v", err)
	}
	if countBefore == 0 {
		t.Fatal("context file row not found before agent delete")
	}

	// Delete the agent.
	res, err = api.DELETE(ctx, fmt.Sprintf("/v1/agents/%s", created.ID))
	mustOKAgents(t, "DELETE /v1/agents/{id}", res, err, http.StatusOK)

	// Verify cascade: agent_context_files row must be gone.
	var countAfter int
	if err := db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM agent_context_files WHERE agent_id = $1", created.ID,
	).Scan(&countAfter); err != nil {
		t.Fatalf("count after delete: %v", err)
	}
	if countAfter != 0 {
		t.Fatalf("expected 0 agent_context_files rows after agent delete, got %d", countAfter)
	}
}

// TestAgentShareWithUser — POST /v1/agents/{id}/shares with another user → 201;
// the shared member can GET the agent.
func TestAgentShareWithUser(t *testing.T) {
	helpers.MustMigrateClean(t)
	helpers.ResetDB(t)

	gw := helpers.StartGateway(t)
	api := helpers.NewAPIClient()
	api.BaseURL = gw.BaseURL

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	rootToken := loginForAgents(t, ctx, api, helpers.RootEmail(), helpers.RootPassword())

	// Create a member to share with.
	api.SetToken(rootToken)
	memberEmail := helpers.RandEmail("m")
	memberPass := "TestPass1!-" + helpers.RandHex8()
	res, err := api.POST(ctx, "/v1/users", map[string]any{
		"email": memberEmail, "password": memberPass, "role": "member",
	})
	mustOKAgents(t, "POST /v1/users", res, err, http.StatusCreated)
	var memberUser struct{ ID string `json:"id"` }
	mustJSONAgents(t, res, &memberUser)

	// Create an agent as root.
	body := createAgentBody("predefined")
	res, err = api.POST(ctx, "/v1/agents", body)
	mustOKAgents(t, "POST /v1/agents", res, err, http.StatusCreated)
	var agent agentResp
	mustJSONAgents(t, res, &agent)

	// Share the agent with the member.
	res, err = api.POST(ctx, fmt.Sprintf("/v1/agents/%s/shares", agent.ID), map[string]any{
		"user_id": memberUser.ID,
	})
	mustOKAgents(t, "POST /v1/agents/{id}/shares", res, err, http.StatusCreated)

	// Member can now GET the agent.
	memberToken := loginForAgents(t, ctx, api, memberEmail, memberPass)
	api.SetToken(memberToken)
	res, err = api.GET(ctx, fmt.Sprintf("/v1/agents/%s", agent.ID))
	mustOKAgents(t, "GET /v1/agents/{id} as shared member", res, err, http.StatusOK)
}
