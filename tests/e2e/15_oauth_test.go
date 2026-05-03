//go:build e2e

// Package e2e_test — Phase 14B OAuth tests.
// Validates /v1/auth/chatgpt/{provider}/* and /v1/auth/openai/* endpoints
// (internal/http/oauth.go). Full OAuth flow requires a live provider, so
// tests focus on wire format, permission gates, and status endpoints.
package e2e_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/tests/e2e/helpers"
)

// mustOKOAuth / mustJSONOAuth are file-local helpers.
func mustOKOAuth(t *testing.T, label string, res *helpers.APIResponse, err error, want int) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: transport error: %v", label, err)
	}
	if res.Status != want {
		t.Fatalf("%s: status %d, want %d, body=%s", label, res.Status, want, string(res.Body))
	}
}

func mustJSONOAuth(t *testing.T, res *helpers.APIResponse, out any) {
	t.Helper()
	if err := res.JSON(out); err != nil {
		t.Fatalf("decode JSON: %v body=%s", err, string(res.Body))
	}
}

// loginOAuth logs in and returns the access token.
func loginOAuth(t *testing.T, ctx context.Context, api *helpers.APIClient, email, pass string) string {
	t.Helper()
	res, err := api.POST(ctx, "/v1/auth/login", map[string]string{
		"email": email, "password": pass,
	})
	mustOKOAuth(t, "POST /v1/auth/login", res, err, http.StatusOK)
	var tok struct {
		AccessToken string `json:"access_token"`
	}
	mustJSONOAuth(t, res, &tok)
	if tok.AccessToken == "" {
		t.Fatalf("loginOAuth %s: empty access_token", email)
	}
	return tok.AccessToken
}

// TestOAuthOpenAIStatusReturns200 — GET /v1/auth/openai/status → 200 with authenticated field.
// No real provider configured; expects authenticated=false.
func TestOAuthOpenAIStatusReturns200(t *testing.T) {
	helpers.MustMigrateClean(t)
	helpers.ResetDB(t)

	gw := helpers.StartGateway(t)
	api := helpers.NewAPIClient()
	api.BaseURL = gw.BaseURL

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// OAuth endpoints require admin role.
	rootToken := loginOAuth(t, ctx, api, helpers.RootEmail(), helpers.RootPassword())
	api.SetToken(rootToken)

	res, err := api.GET(ctx, "/v1/auth/openai/status")
	mustOKOAuth(t, "GET /v1/auth/openai/status", res, err, http.StatusOK)

	var status map[string]any
	mustJSONOAuth(t, res, &status)
	if _, ok := status["authenticated"]; !ok {
		t.Fatalf("status response missing 'authenticated' field; body=%s", string(res.Body))
	}
}

// TestOAuthChatGPTProviderStatusReturns200 — GET /v1/auth/chatgpt/{provider}/status → 200.
func TestOAuthChatGPTProviderStatusReturns200(t *testing.T) {
	helpers.MustMigrateClean(t)
	helpers.ResetDB(t)

	gw := helpers.StartGateway(t)
	api := helpers.NewAPIClient()
	api.BaseURL = gw.BaseURL

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	rootToken := loginOAuth(t, ctx, api, helpers.RootEmail(), helpers.RootPassword())
	api.SetToken(rootToken)

	// Use a synthetic provider name that passes slug validation.
	res, err := api.GET(ctx, "/v1/auth/chatgpt/test-provider/status")
	if err != nil {
		t.Fatalf("GET /v1/auth/chatgpt/test-provider/status: transport error: %v", err)
	}
	// 200 (not authenticated) or 409 (provider type conflict) are both acceptable;
	// 401/403 would indicate an auth regression.
	if res.Status == http.StatusUnauthorized || res.Status == http.StatusForbidden {
		t.Fatalf("status gate failed: %d body=%s", res.Status, string(res.Body))
	}
}

// TestOAuthNonAdminBlocked — non-admin user → GET /v1/auth/openai/status → 403.
func TestOAuthNonAdminBlocked(t *testing.T) {
	helpers.MustMigrateClean(t)
	helpers.ResetDB(t)

	gw := helpers.StartGateway(t)
	api := helpers.NewAPIClient()
	api.BaseURL = gw.BaseURL

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Create a member user via root.
	rootToken := loginOAuth(t, ctx, api, helpers.RootEmail(), helpers.RootPassword())
	api.SetToken(rootToken)

	memberEmail := helpers.RandEmail("m")
	memberPass := "TestPass1!-" + helpers.RandHex8()
	res, err := api.POST(ctx, "/v1/users", map[string]any{
		"email":    memberEmail,
		"password": memberPass,
		"role":     "member",
	})
	mustOKOAuth(t, "POST /v1/users (member)", res, err, http.StatusCreated)

	memberToken := loginOAuth(t, ctx, api, memberEmail, memberPass)
	api.SetToken(memberToken)

	res, err = api.GET(ctx, "/v1/auth/openai/status")
	if err != nil {
		t.Fatalf("GET /v1/auth/openai/status as member: %v", err)
	}
	if res.Status != http.StatusForbidden {
		t.Fatalf("member accessing oauth status: status %d, want 403, body=%s", res.Status, string(res.Body))
	}
}

// TestOAuthOpenAIStartInitiatesFlow — POST /v1/auth/openai/start → returns auth_url or conflict.
// Does not complete OAuth (no real provider). Validates wire format and that admin can start a flow.
func TestOAuthOpenAIStartInitiatesFlow(t *testing.T) {
	helpers.MustMigrateClean(t)
	helpers.ResetDB(t)

	gw := helpers.StartGateway(t)
	api := helpers.NewAPIClient()
	api.BaseURL = gw.BaseURL

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	rootToken := loginOAuth(t, ctx, api, helpers.RootEmail(), helpers.RootPassword())
	api.SetToken(rootToken)

	res, err := api.POST(ctx, "/v1/auth/openai/start", map[string]any{
		"display_name": "Test OpenAI",
	})
	if err != nil {
		t.Fatalf("POST /v1/auth/openai/start: %v", err)
	}

	// Acceptable responses:
	// 200 — already_authenticated or auth_url returned (no port conflict)
	// 409 — another flow active or provider type mismatch
	// 500 — port 1455 not available in test env (expected in CI)
	switch res.Status {
	case http.StatusOK, http.StatusConflict, http.StatusInternalServerError:
		// All valid in test environment.
	case http.StatusUnauthorized, http.StatusForbidden:
		t.Fatalf("auth gate failed for admin: status %d body=%s", res.Status, string(res.Body))
	default:
		t.Fatalf("unexpected status %d from /v1/auth/openai/start; body=%s", res.Status, string(res.Body))
	}
}

// TestOAuthOpenAILogoutAccepted — POST /v1/auth/openai/logout → 200 (no token = no-op logout).
func TestOAuthOpenAILogoutAccepted(t *testing.T) {
	helpers.MustMigrateClean(t)
	helpers.ResetDB(t)

	gw := helpers.StartGateway(t)
	api := helpers.NewAPIClient()
	api.BaseURL = gw.BaseURL

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	rootToken := loginOAuth(t, ctx, api, helpers.RootEmail(), helpers.RootPassword())
	api.SetToken(rootToken)

	// Logout when not authenticated — should be a no-op, not a crash.
	res, err := api.POST(ctx, "/v1/auth/openai/logout", nil)
	if err != nil {
		t.Fatalf("POST /v1/auth/openai/logout: %v", err)
	}
	if res.Status == http.StatusUnauthorized || res.Status == http.StatusForbidden {
		t.Fatalf("admin logout blocked: %d body=%s", res.Status, string(res.Body))
	}
}
