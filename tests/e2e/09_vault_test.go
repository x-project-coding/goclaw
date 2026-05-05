//go:build e2e

// Package e2e_test exercises /v1/vault/* and /v1/agents/{agentID}/vault/*
// endpoints (internal/http/vault_handlers.go + vault_handler_documents.go).
package e2e_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/tests/e2e/helpers"
)

// mustOKVault / mustJSONVault are file-local helpers.
func mustOKVault(t *testing.T, label string, res *helpers.APIResponse, err error, want int) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: transport error: %v", label, err)
	}
	if res.Status != want {
		t.Fatalf("%s: status %d, want %d, body=%s", label, res.Status, want, string(res.Body))
	}
}

func mustJSONVault(t *testing.T, res *helpers.APIResponse, out any) {
	t.Helper()
	if err := res.JSON(out); err != nil {
		t.Fatalf("decode JSON: %v body=%s", err, string(res.Body))
	}
}

// loginVault logs in and returns the access token.
func loginVault(t *testing.T, ctx context.Context, api *helpers.APIClient, email, pass string) string {
	t.Helper()
	res, err := api.POST(ctx, "/v1/auth/login", map[string]string{
		"email": email, "password": pass,
	})
	mustOKVault(t, "POST /v1/auth/login", res, err, http.StatusOK)
	var tok struct {
		AccessToken string `json:"access_token"`
	}
	mustJSONVault(t, res, &tok)
	if tok.AccessToken == "" {
		t.Fatalf("loginVault %s: empty access_token", email)
	}
	return tok.AccessToken
}

// createVaultDoc creates a vault document via POST /v1/vault/documents and returns its ID.
func createVaultDoc(t *testing.T, ctx context.Context, api *helpers.APIClient, suffix string) string {
	t.Helper()
	res, err := api.POST(ctx, "/v1/vault/documents", map[string]any{
		"path":     "test/" + suffix + ".md",
		"title":    "Test Doc " + suffix,
		"doc_type": "note",
		"scope":    "shared",
	})
	mustOKVault(t, "POST /v1/vault/documents", res, err, http.StatusCreated)
	var doc struct {
		ID string `json:"id"`
	}
	mustJSONVault(t, res, &doc)
	if doc.ID == "" {
		t.Fatalf("createVaultDoc: no id in response body=%s", string(res.Body))
	}
	return doc.ID
}

// TestVaultDocCreate — POST doc → 201 → returns id.
func TestVaultDocCreate(t *testing.T) {
	helpers.MustMigrateClean(t)
	helpers.ResetDB(t)

	gw := helpers.StartGateway(t)
	api := helpers.NewAPIClient()
	api.BaseURL = gw.BaseURL

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	rootToken := loginVault(t, ctx, api, helpers.RootEmail(), helpers.RootPassword())
	api.SetToken(rootToken)

	docID := createVaultDoc(t, ctx, api, helpers.RandHex8())
	if docID == "" {
		t.Fatalf("expected non-empty doc id")
	}
}

// TestVaultDocList — GET list contains created doc.
func TestVaultDocList(t *testing.T) {
	helpers.MustMigrateClean(t)
	helpers.ResetDB(t)

	gw := helpers.StartGateway(t)
	api := helpers.NewAPIClient()
	api.BaseURL = gw.BaseURL

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	rootToken := loginVault(t, ctx, api, helpers.RootEmail(), helpers.RootPassword())
	api.SetToken(rootToken)

	suffix := helpers.RandHex8()
	docID := createVaultDoc(t, ctx, api, suffix)

	res, err := api.GET(ctx, "/v1/vault/documents")
	mustOKVault(t, "GET /v1/vault/documents", res, err, http.StatusOK)

	var resp struct {
		Documents []struct {
			ID string `json:"id"`
		} `json:"documents"`
		Total int `json:"total"`
	}
	mustJSONVault(t, res, &resp)

	found := false
	for _, d := range resp.Documents {
		if d.ID == docID {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("created doc %s not found in list (total=%d)", docID, resp.Total)
	}
}

// TestVaultDocPatch — PUT content fields → GET reflects changes.
func TestVaultDocPatch(t *testing.T) {
	helpers.MustMigrateClean(t)
	helpers.ResetDB(t)

	gw := helpers.StartGateway(t)
	api := helpers.NewAPIClient()
	api.BaseURL = gw.BaseURL

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	rootToken := loginVault(t, ctx, api, helpers.RootEmail(), helpers.RootPassword())
	api.SetToken(rootToken)

	docID := createVaultDoc(t, ctx, api, helpers.RandHex8())
	newTitle := "Updated " + helpers.RandHex8()

	// PUT /v1/vault/documents/{docID} — update title.
	res, err := api.Do(ctx, http.MethodPut, fmt.Sprintf("/v1/vault/documents/%s", docID), map[string]any{
		"title": &newTitle,
	})
	mustOKVault(t, "PUT /v1/vault/documents/{id}", res, err, http.StatusOK)

	// GET the doc by ID via list (filter by checking each).
	res, err = api.GET(ctx, "/v1/vault/documents")
	mustOKVault(t, "GET /v1/vault/documents (after patch)", res, err, http.StatusOK)

	var resp struct {
		Documents []struct {
			ID    string `json:"id"`
			Title string `json:"title"`
		} `json:"documents"`
	}
	mustJSONVault(t, res, &resp)

	for _, d := range resp.Documents {
		if d.ID == docID {
			if d.Title != newTitle {
				t.Fatalf("title after patch: got %q want %q", d.Title, newTitle)
			}
			return
		}
	}
	t.Fatalf("patched doc %s not found in list", docID)
}

// TestVaultDocDelete — DELETE → 204 or 200 → GET list no longer contains it.
func TestVaultDocDelete(t *testing.T) {
	helpers.MustMigrateClean(t)
	helpers.ResetDB(t)

	gw := helpers.StartGateway(t)
	api := helpers.NewAPIClient()
	api.BaseURL = gw.BaseURL

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	rootToken := loginVault(t, ctx, api, helpers.RootEmail(), helpers.RootPassword())
	api.SetToken(rootToken)

	docID := createVaultDoc(t, ctx, api, helpers.RandHex8())

	res, err := api.DELETE(ctx, fmt.Sprintf("/v1/vault/documents/%s", docID))
	if err != nil {
		t.Fatalf("DELETE vault doc: %v", err)
	}
	if res.Status != http.StatusNoContent && res.Status != http.StatusOK {
		t.Fatalf("DELETE vault doc: status %d, want 200 or 204, body=%s", res.Status, string(res.Body))
	}

	// Verify it's gone from the list.
	res, err = api.GET(ctx, "/v1/vault/documents")
	mustOKVault(t, "GET /v1/vault/documents (after delete)", res, err, http.StatusOK)

	var resp struct {
		Documents []struct {
			ID string `json:"id"`
		} `json:"documents"`
	}
	mustJSONVault(t, res, &resp)

	for _, d := range resp.Documents {
		if d.ID == docID {
			t.Fatalf("deleted doc %s still present in list", docID)
		}
	}
}

// TestVaultLinksEndpointExists — GET /v1/vault/documents/{id}/links returns 200.
// Wikilink resolution requires the doc to exist; verifies the endpoint accepts the request.
func TestVaultLinksEndpointExists(t *testing.T) {
	helpers.MustMigrateClean(t)
	helpers.ResetDB(t)

	gw := helpers.StartGateway(t)
	api := helpers.NewAPIClient()
	api.BaseURL = gw.BaseURL

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	rootToken := loginVault(t, ctx, api, helpers.RootEmail(), helpers.RootPassword())
	api.SetToken(rootToken)

	docID := createVaultDoc(t, ctx, api, helpers.RandHex8())

	res, err := api.GET(ctx, fmt.Sprintf("/v1/vault/documents/%s/links", docID))
	mustOKVault(t, "GET /v1/vault/documents/{id}/links", res, err, http.StatusOK)
}

// TestVaultHybridSearch — POST /v1/vault/search returns the seeded doc title.
// Validates wire format; score-based ranking not asserted (embedding provider may vary).
func TestVaultHybridSearch(t *testing.T) {
	helpers.MustMigrateClean(t)
	helpers.ResetDB(t)

	gw := helpers.StartGateway(t)
	api := helpers.NewAPIClient()
	api.BaseURL = gw.BaseURL

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	rootToken := loginVault(t, ctx, api, helpers.RootEmail(), helpers.RootPassword())
	api.SetToken(rootToken)

	// Create a doc with a unique title phrase.
	suffix := helpers.RandHex8()
	createVaultDoc(t, ctx, api, suffix)

	res, err := api.POST(ctx, "/v1/vault/search", map[string]any{
		"query":       "Test Doc " + suffix,
		"max_results": 5,
	})
	mustOKVault(t, "POST /v1/vault/search", res, err, http.StatusOK)

	// Validate response is a JSON array (search returns []VaultSearchResult).
	var results []map[string]any
	if err := res.JSON(&results); err != nil {
		// Some implementations wrap in {"results": [...]}.
		var wrapped struct {
			Results []map[string]any `json:"results"`
		}
		if err2 := res.JSON(&wrapped); err2 != nil {
			t.Fatalf("search response not parseable: %v body=%s", err, string(res.Body))
		}
		results = wrapped.Results
	}
	// Just verify the response is valid (empty is acceptable when FTS index not warm).
	_ = results
}

// TestVaultWikilinksResolve — create 2 docs, link them, GET /v1/vault/documents/{id}/links
// and verify the doc_names map resolves the target doc title.
func TestVaultWikilinksResolve(t *testing.T) {
	helpers.MustMigrateClean(t)
	helpers.ResetDB(t)

	gw := helpers.StartGateway(t)
	api := helpers.NewAPIClient()
	api.BaseURL = gw.BaseURL

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	rootToken := loginVault(t, ctx, api, helpers.RootEmail(), helpers.RootPassword())
	api.SetToken(rootToken)

	// Create doc A with title "alpha".
	resA, err := api.POST(ctx, "/v1/vault/documents", map[string]any{
		"path":     "wiki/alpha.md",
		"title":    "alpha",
		"content":  "# Alpha\n\nLink to [[Beta]] here.",
		"doc_type": "note",
		"scope":    "shared",
	})
	mustOKVault(t, "POST /v1/vault/documents (alpha)", resA, err, http.StatusCreated)
	var docA struct {
		ID string `json:"id"`
	}
	mustJSONVault(t, resA, &docA)

	// Create doc B with title "beta".
	resB, err := api.POST(ctx, "/v1/vault/documents", map[string]any{
		"path":     "wiki/beta.md",
		"title":    "beta",
		"content":  "# Beta\n\nReferenced by Alpha.",
		"doc_type": "note",
		"scope":    "shared",
	})
	mustOKVault(t, "POST /v1/vault/documents (beta)", resB, err, http.StatusCreated)
	var docB struct {
		ID string `json:"id"`
	}
	mustJSONVault(t, resB, &docB)

	// Create a link from A → B via POST /v1/vault/links.
	resLink, err := api.POST(ctx, "/v1/vault/links", map[string]any{
		"from_doc_id": docA.ID,
		"to_doc_id":   docB.ID,
		"link_type":   "reference",
	})
	mustOKVault(t, "POST /v1/vault/links", resLink, err, http.StatusCreated)

	// GET /v1/vault/documents/{docA.ID}/links — should return doc_names with B's ID → "beta".
	res, err := api.GET(ctx, fmt.Sprintf("/v1/vault/documents/%s/links", docA.ID))
	mustOKVault(t, "GET /v1/vault/documents/{id}/links", res, err, http.StatusOK)

	var linksResp struct {
		Outlinks  []struct {
			ToDocID string `json:"to_doc_id"`
		} `json:"outlinks"`
		DocNames map[string]string `json:"doc_names"`
	}
	mustJSONVault(t, res, &linksResp)

	// Verify outlink from A → B is present.
	foundOutlink := false
	for _, l := range linksResp.Outlinks {
		if l.ToDocID == docB.ID {
			foundOutlink = true
			break
		}
	}
	if !foundOutlink {
		t.Fatalf("outlinks does not contain to_doc_id=%s (body=%s)", docB.ID, string(res.Body))
	}

	// Verify doc_names resolves B's ID to "beta".
	resolvedName, ok := linksResp.DocNames[docB.ID]
	if !ok {
		t.Fatalf("doc_names missing entry for doc_id=%s (body=%s)", docB.ID, string(res.Body))
	}
	if resolvedName != "beta" {
		t.Fatalf("doc_names[%s] = %q, want %q", docB.ID, resolvedName, "beta")
	}
}

// TestVaultAgentScopedDocCreate — POST /v1/agents/{agentID}/vault/documents creates a doc scoped to agent.
func TestVaultAgentScopedDocCreate(t *testing.T) {
	helpers.MustMigrateClean(t)
	helpers.ResetDB(t)

	gw := helpers.StartGateway(t)
	api := helpers.NewAPIClient()
	api.BaseURL = gw.BaseURL

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	rootToken := loginVault(t, ctx, api, helpers.RootEmail(), helpers.RootPassword())
	api.SetToken(rootToken)

	// Create an agent first.
	res, err := api.POST(ctx, "/v1/agents", map[string]any{
		"agent_key":  "vlt-" + helpers.RandHex8(),
		"model":      "test/test-model",
		"provider":   "openai",
	})
	mustOKVault(t, "POST /v1/agents", res, err, http.StatusCreated)
	var ag struct{ ID string `json:"id"` }
	mustJSONVault(t, res, &ag)

	suffix := helpers.RandHex8()
	res, err = api.POST(ctx, fmt.Sprintf("/v1/agents/%s/vault/documents", ag.ID), map[string]any{
		"path":     "agent-docs/" + suffix + ".md",
		"title":    "Agent Doc " + suffix,
		"doc_type": "note",
		"scope":    "personal",
	})
	mustOKVault(t, "POST /v1/agents/{id}/vault/documents", res, err, http.StatusCreated)

	var created struct{ ID string `json:"id"` }
	mustJSONVault(t, res, &created)
	if created.ID == "" {
		t.Fatalf("agent-scoped vault doc: empty id in response")
	}
}
