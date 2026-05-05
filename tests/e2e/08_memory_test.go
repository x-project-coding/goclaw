//go:build e2e

// Package e2e_test exercises /v1/agents/{agentID}/memory/* and
// /v1/agents/{agentID}/kg/* endpoints (internal/http/memory.go +
// internal/http/knowledge_graph.go).
package e2e_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/tests/e2e/helpers"
)

// mustOKMem / mustJSONMem are file-local helpers.
func mustOKMem(t *testing.T, label string, res *helpers.APIResponse, err error, want int) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: transport error: %v", label, err)
	}
	if res.Status != want {
		t.Fatalf("%s: status %d, want %d, body=%s", label, res.Status, want, string(res.Body))
	}
}

func mustJSONMem(t *testing.T, res *helpers.APIResponse, out any) {
	t.Helper()
	if err := res.JSON(out); err != nil {
		t.Fatalf("decode JSON: %v body=%s", err, string(res.Body))
	}
}

// loginMem logs in and returns the access token.
func loginMem(t *testing.T, ctx context.Context, api *helpers.APIClient, email, pass string) string {
	t.Helper()
	res, err := api.POST(ctx, "/v1/auth/login", map[string]string{
		"email": email, "password": pass,
	})
	mustOKMem(t, "POST /v1/auth/login", res, err, http.StatusOK)
	var tok struct {
		AccessToken string `json:"access_token"`
	}
	mustJSONMem(t, res, &tok)
	if tok.AccessToken == "" {
		t.Fatalf("loginMem %s: empty access_token", email)
	}
	return tok.AccessToken
}

// createAgentForMemory seeds an agent via /v1/agents, returns its ID.
func createAgentForMemory(t *testing.T, ctx context.Context, api *helpers.APIClient) string {
	t.Helper()
	res, err := api.POST(ctx, "/v1/agents", map[string]any{
		"agent_key":  "mem-" + helpers.RandHex8(),
		"model":      "test/test-model",
		"provider":   "openai",
	})
	mustOKMem(t, "POST /v1/agents (mem)", res, err, http.StatusCreated)
	var ag struct{ ID string `json:"id"` }
	mustJSONMem(t, res, &ag)
	if ag.ID == "" {
		t.Fatalf("createAgentForMemory: no id in response")
	}
	return ag.ID
}

// TestMemoryDocCreateAndList — PUT a memory doc, GET list returns it.
func TestMemoryDocCreateAndList(t *testing.T) {
	helpers.MustMigrateClean(t)
	helpers.ResetDB(t)

	gw := helpers.StartGateway(t)
	api := helpers.NewAPIClient()
	api.BaseURL = gw.BaseURL

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	rootToken := loginMem(t, ctx, api, helpers.RootEmail(), helpers.RootPassword())
	api.SetToken(rootToken)
	agentID := createAgentForMemory(t, ctx, api)

	path := "notes/test-" + helpers.RandHex8() + ".md"
	content := "hello memory " + helpers.RandHex8()

	// PUT the document.
	res, err := api.Do(ctx, http.MethodPut,
		fmt.Sprintf("/v1/agents/%s/memory/documents/%s", agentID, path),
		map[string]string{"content": content})
	mustOKMem(t, "PUT memory doc", res, err, http.StatusOK)

	// GET list — must contain our path.
	res, err = api.GET(ctx, fmt.Sprintf("/v1/agents/%s/memory/documents", agentID))
	mustOKMem(t, "GET memory docs", res, err, http.StatusOK)
	var list []map[string]any
	mustJSONMem(t, res, &list)

	found := false
	for _, d := range list {
		if p, _ := d["path"].(string); p == path {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("created doc path %q not found in list; got %d items", path, len(list))
	}
}

// TestMemoryDocGetByID — GET a doc by path round-trips content.
func TestMemoryDocGetByID(t *testing.T) {
	helpers.MustMigrateClean(t)
	helpers.ResetDB(t)

	gw := helpers.StartGateway(t)
	api := helpers.NewAPIClient()
	api.BaseURL = gw.BaseURL

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	rootToken := loginMem(t, ctx, api, helpers.RootEmail(), helpers.RootPassword())
	api.SetToken(rootToken)
	agentID := createAgentForMemory(t, ctx, api)

	path := "notes/get-" + helpers.RandHex8() + ".md"
	content := "round-trip " + helpers.RandHex8()

	res, err := api.Do(ctx, http.MethodPut,
		fmt.Sprintf("/v1/agents/%s/memory/documents/%s", agentID, path),
		map[string]string{"content": content})
	mustOKMem(t, "PUT memory doc", res, err, http.StatusOK)

	res, err = api.GET(ctx, fmt.Sprintf("/v1/agents/%s/memory/documents/%s", agentID, path))
	mustOKMem(t, "GET memory doc", res, err, http.StatusOK)
	var doc map[string]any
	mustJSONMem(t, res, &doc)
	if c, _ := doc["content"].(string); c != content {
		t.Fatalf("content mismatch: got %q want %q", c, content)
	}
}

// TestMemoryDocDelete — DELETE then GET → 404.
func TestMemoryDocDelete(t *testing.T) {
	helpers.MustMigrateClean(t)
	helpers.ResetDB(t)

	gw := helpers.StartGateway(t)
	api := helpers.NewAPIClient()
	api.BaseURL = gw.BaseURL

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	rootToken := loginMem(t, ctx, api, helpers.RootEmail(), helpers.RootPassword())
	api.SetToken(rootToken)
	agentID := createAgentForMemory(t, ctx, api)

	path := "notes/del-" + helpers.RandHex8() + ".md"

	res, err := api.Do(ctx, http.MethodPut,
		fmt.Sprintf("/v1/agents/%s/memory/documents/%s", agentID, path),
		map[string]string{"content": "to be deleted"})
	mustOKMem(t, "PUT memory doc", res, err, http.StatusOK)

	res, err = api.DELETE(ctx, fmt.Sprintf("/v1/agents/%s/memory/documents/%s", agentID, path))
	mustOKMem(t, "DELETE memory doc", res, err, http.StatusOK)

	res, err = api.GET(ctx, fmt.Sprintf("/v1/agents/%s/memory/documents/%s", agentID, path))
	if err != nil {
		t.Fatalf("GET deleted doc: %v", err)
	}
	if res.Status != http.StatusNotFound {
		t.Fatalf("GET deleted doc: status %d, want 404", res.Status)
	}
}

// TestMemorySearchReturns200 — POST /memory/search with a query → 200 + results key.
// Note: pgvector embeddings require a running embedding provider for true semantic search.
// This test validates the wire format and that the endpoint accepts a valid request.
func TestMemorySearchReturns200(t *testing.T) {
	helpers.MustMigrateClean(t)
	helpers.ResetDB(t)

	gw := helpers.StartGateway(t)
	api := helpers.NewAPIClient()
	api.BaseURL = gw.BaseURL

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	rootToken := loginMem(t, ctx, api, helpers.RootEmail(), helpers.RootPassword())
	api.SetToken(rootToken)
	agentID := createAgentForMemory(t, ctx, api)

	// PUT a doc first so the store isn't empty.
	path := "notes/search-" + helpers.RandHex8() + ".md"
	res, err := api.Do(ctx, http.MethodPut,
		fmt.Sprintf("/v1/agents/%s/memory/documents/%s", agentID, path),
		map[string]string{"content": "unique phrase alpha beta gamma " + helpers.RandHex8()})
	mustOKMem(t, "PUT memory doc for search", res, err, http.StatusOK)

	// Search — just validate the response envelope.
	res, err = api.POST(ctx, fmt.Sprintf("/v1/agents/%s/memory/search", agentID), map[string]any{
		"query":       "alpha beta gamma",
		"max_results": 5,
	})
	mustOKMem(t, "POST memory/search", res, err, http.StatusOK)
	var sr map[string]any
	mustJSONMem(t, res, &sr)
	if _, ok := sr["results"]; !ok {
		t.Fatalf("search response missing 'results' key; body=%s", string(res.Body))
	}
}

// TestKGEntitiesCRUD — POST entity → GET list → DELETE → list shows 0.
func TestKGEntitiesCRUD(t *testing.T) {
	helpers.MustMigrateClean(t)
	helpers.ResetDB(t)

	gw := helpers.StartGateway(t)
	api := helpers.NewAPIClient()
	api.BaseURL = gw.BaseURL

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	rootToken := loginMem(t, ctx, api, helpers.RootEmail(), helpers.RootPassword())
	api.SetToken(rootToken)
	agentID := createAgentForMemory(t, ctx, api)

	externalID := "ent-" + helpers.RandHex8()

	// POST entity.
	res, err := api.POST(ctx, fmt.Sprintf("/v1/agents/%s/kg/entities", agentID), map[string]any{
		"external_id": externalID,
		"name":        "Test Entity " + helpers.RandHex8(),
		"entity_type": "concept",
		"confidence":  1.0,
	})
	mustOKMem(t, "POST kg/entities", res, err, http.StatusOK)

	// GET list — must contain entity.
	res, err = api.GET(ctx, fmt.Sprintf("/v1/agents/%s/kg/entities", agentID))
	mustOKMem(t, "GET kg/entities", res, err, http.StatusOK)
	var entities []map[string]any
	mustJSONMem(t, res, &entities)

	var entityID string
	for _, e := range entities {
		if eid, _ := e["external_id"].(string); eid == externalID {
			entityID, _ = e["id"].(string)
			break
		}
	}
	if entityID == "" {
		t.Fatalf("created entity %q not found in list; got %d items", externalID, len(entities))
	}

	// GET single entity.
	res, err = api.GET(ctx, fmt.Sprintf("/v1/agents/%s/kg/entities/%s", agentID, entityID))
	mustOKMem(t, "GET kg/entities/{id}", res, err, http.StatusOK)

	// DELETE entity.
	res, err = api.DELETE(ctx, fmt.Sprintf("/v1/agents/%s/kg/entities/%s", agentID, entityID))
	mustOKMem(t, "DELETE kg/entities/{id}", res, err, http.StatusOK)

	// GET after delete → 404.
	res, err = api.GET(ctx, fmt.Sprintf("/v1/agents/%s/kg/entities/%s", agentID, entityID))
	if err != nil {
		t.Fatalf("GET deleted entity: %v", err)
	}
	if res.Status != http.StatusNotFound {
		t.Fatalf("GET deleted entity: status %d, want 404", res.Status)
	}
}

// TestKGTraverseReturns200 — POST /kg/traverse with a valid entity returns 200.
func TestKGTraverseReturns200(t *testing.T) {
	helpers.MustMigrateClean(t)
	helpers.ResetDB(t)

	gw := helpers.StartGateway(t)
	api := helpers.NewAPIClient()
	api.BaseURL = gw.BaseURL

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	rootToken := loginMem(t, ctx, api, helpers.RootEmail(), helpers.RootPassword())
	api.SetToken(rootToken)
	agentID := createAgentForMemory(t, ctx, api)

	// Seed an entity.
	externalID := "trav-" + helpers.RandHex8()
	res, err := api.POST(ctx, fmt.Sprintf("/v1/agents/%s/kg/entities", agentID), map[string]any{
		"external_id": externalID,
		"name":        "Traversal Entity",
		"entity_type": "concept",
		"confidence":  1.0,
	})
	mustOKMem(t, "POST kg/entities (traverse seed)", res, err, http.StatusOK)

	// Fetch to get the DB ID.
	res, err = api.GET(ctx, fmt.Sprintf("/v1/agents/%s/kg/entities", agentID))
	mustOKMem(t, "GET kg/entities", res, err, http.StatusOK)
	var entities []map[string]any
	mustJSONMem(t, res, &entities)

	var entityID string
	for _, e := range entities {
		if eid, _ := e["external_id"].(string); eid == externalID {
			entityID, _ = e["id"].(string)
			break
		}
	}
	if entityID == "" {
		t.Fatalf("entity %q not found after creation", externalID)
	}

	// POST /kg/traverse.
	res, err = api.POST(ctx, fmt.Sprintf("/v1/agents/%s/kg/traverse", agentID), map[string]any{
		"entity_id": entityID,
		"max_depth": 1,
	})
	mustOKMem(t, "POST kg/traverse", res, err, http.StatusOK)
}
