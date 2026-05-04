//go:build e2e

// Package e2e_test exercises /v1/cli-credentials/* endpoints
// (internal/http/secure_cli.go + secure_cli_user_credentials.go).
// Implementation uses the cli-credentials model — see
// docs/adr/2026-05-v4-secure-cli-credentials-model.md.
package e2e_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/tests/e2e/helpers"
)

// mustOKCLI / mustJSONCLI are file-local helpers.
func mustOKCLI(t *testing.T, label string, res *helpers.APIResponse, err error, want int) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: transport error: %v", label, err)
	}
	if res.Status != want {
		t.Fatalf("%s: status %d, want %d, body=%s", label, res.Status, want, string(res.Body))
	}
}

func mustJSONCLI(t *testing.T, res *helpers.APIResponse, out any) {
	t.Helper()
	if err := res.JSON(out); err != nil {
		t.Fatalf("decode JSON: %v body=%s", err, string(res.Body))
	}
}

// loginCLI logs in and returns the access token.
func loginCLI(t *testing.T, ctx context.Context, api *helpers.APIClient, email, pass string) string {
	t.Helper()
	res, err := api.POST(ctx, "/v1/auth/login", map[string]string{
		"email": email, "password": pass,
	})
	mustOKCLI(t, "POST /v1/auth/login", res, err, http.StatusOK)
	var tok struct {
		AccessToken string `json:"access_token"`
	}
	mustJSONCLI(t, res, &tok)
	if tok.AccessToken == "" {
		t.Fatalf("loginCLI %s: empty access_token", email)
	}
	return tok.AccessToken
}

// createCLICredential creates a cli-credential entry and returns its ID.
// Uses the "echo" binary (universally available) to avoid LookPath failures.
func createCLICredential(t *testing.T, ctx context.Context, api *helpers.APIClient) string {
	t.Helper()
	res, err := api.POST(ctx, "/v1/cli-credentials", map[string]any{
		"binary_name": "echo",
		"description": "e2e test credential " + helpers.RandHex8(),
		"env": map[string]string{
			"E2E_TEST_KEY": "test-value-" + helpers.RandHex8(),
		},
		"enabled": true,
	})
	mustOKCLI(t, "POST /v1/cli-credentials", res, err, http.StatusCreated)
	var cred struct {
		ID string `json:"id"`
	}
	mustJSONCLI(t, res, &cred)
	if cred.ID == "" {
		t.Fatalf("createCLICredential: no id in response body=%s", string(res.Body))
	}
	return cred.ID
}

// TestSecureCLIPresets — GET /v1/cli-credentials/presets → 200 + presets map non-empty.
func TestSecureCLIPresets(t *testing.T) {
	helpers.MustMigrateClean(t)
	helpers.ResetDB(t)

	gw := helpers.StartGateway(t)
	api := helpers.NewAPIClient()
	api.BaseURL = gw.BaseURL

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// cli-credentials endpoints require admin role.
	rootToken := loginCLI(t, ctx, api, helpers.RootEmail(), helpers.RootPassword())
	api.SetToken(rootToken)

	res, err := api.GET(ctx, "/v1/cli-credentials/presets")
	mustOKCLI(t, "GET /v1/cli-credentials/presets", res, err, http.StatusOK)

	var resp struct {
		Presets map[string]any `json:"presets"`
	}
	mustJSONCLI(t, res, &resp)
	if len(resp.Presets) == 0 {
		t.Fatalf("presets map is empty; want at least one preset (gh, gcloud, aws, kubectl, terraform)")
	}

	// Verify known presets exist (from tools/credential_presets.go).
	for _, expected := range []string{"gh", "gcloud", "aws"} {
		if _, ok := resp.Presets[expected]; !ok {
			t.Errorf("expected preset %q missing from response", expected)
		}
	}
}

// TestSecureCLICredentialsCRUD — POST → GET → GET list → PUT → DELETE.
func TestSecureCLICredentialsCRUD(t *testing.T) {
	helpers.MustMigrateClean(t)
	helpers.ResetDB(t)

	gw := helpers.StartGateway(t)
	api := helpers.NewAPIClient()
	api.BaseURL = gw.BaseURL

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	rootToken := loginCLI(t, ctx, api, helpers.RootEmail(), helpers.RootPassword())
	api.SetToken(rootToken)

	credID := createCLICredential(t, ctx, api)

	// GET single.
	res, err := api.GET(ctx, fmt.Sprintf("/v1/cli-credentials/%s", credID))
	mustOKCLI(t, "GET /v1/cli-credentials/{id}", res, err, http.StatusOK)
	var got struct {
		ID          string `json:"id"`
		BinaryName  string `json:"binary_name"`
		EncryptedEnv []byte `json:"encrypted_env"`
	}
	mustJSONCLI(t, res, &got)
	if got.BinaryName != "echo" {
		t.Fatalf("GET credential binary_name: got %q want %q", got.BinaryName, "echo")
	}
	// Encrypted env must NOT be returned.
	if len(got.EncryptedEnv) > 0 {
		t.Fatalf("encrypted_env exposed in GET response — must be omitted")
	}

	// GET list — must contain entry.
	res, err = api.GET(ctx, "/v1/cli-credentials")
	mustOKCLI(t, "GET /v1/cli-credentials", res, err, http.StatusOK)
	var listResp struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	mustJSONCLI(t, res, &listResp)
	found := false
	for _, item := range listResp.Items {
		if item.ID == credID {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("credential %s not in list (%d items)", credID, len(listResp.Items))
	}

	// PUT — update description.
	newDesc := "updated " + helpers.RandHex8()
	res, err = api.Do(ctx, http.MethodPut, fmt.Sprintf("/v1/cli-credentials/%s", credID), map[string]any{
		"description": newDesc,
		"env":         map[string]string{},
	})
	mustOKCLI(t, "PUT /v1/cli-credentials/{id}", res, err, http.StatusOK)

	// DELETE.
	res, err = api.DELETE(ctx, fmt.Sprintf("/v1/cli-credentials/%s", credID))
	mustOKCLI(t, "DELETE /v1/cli-credentials/{id}", res, err, http.StatusOK)

	// GET after delete → 404.
	res, err = api.GET(ctx, fmt.Sprintf("/v1/cli-credentials/%s", credID))
	if err != nil {
		t.Fatalf("GET deleted credential: %v", err)
	}
	if res.Status != http.StatusNotFound {
		t.Fatalf("GET deleted credential: status %d, want 404", res.Status)
	}
}

// TestSecureCLIPerUserCredentials — PUT user-creds for user A → GET as admin sees it;
// a different user's credential slot is independent.
func TestSecureCLIPerUserCredentials(t *testing.T) {
	helpers.MustMigrateClean(t)
	helpers.ResetDB(t)

	gw := helpers.StartGateway(t)
	api := helpers.NewAPIClient()
	api.BaseURL = gw.BaseURL

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	rootToken := loginCLI(t, ctx, api, helpers.RootEmail(), helpers.RootPassword())
	api.SetToken(rootToken)

	// Create two member users.
	userAEmail := helpers.RandEmail("a")
	userAPass := "TestPass1!-" + helpers.RandHex8()
	res, err := api.POST(ctx, "/v1/users", map[string]any{
		"email":    userAEmail,
		"password": userAPass,
		"role":     "member",
	})
	mustOKCLI(t, "create userA", res, err, http.StatusCreated)
	var userA struct{ ID string `json:"id"` }
	mustJSONCLI(t, res, &userA)

	userBEmail := helpers.RandEmail("b")
	userBPass := "TestPass1!-" + helpers.RandHex8()
	res, err = api.POST(ctx, "/v1/users", map[string]any{
		"email":    userBEmail,
		"password": userBPass,
		"role":     "member",
	})
	mustOKCLI(t, "create userB", res, err, http.StatusCreated)
	var userB struct{ ID string `json:"id"` }
	mustJSONCLI(t, res, &userB)

	credID := createCLICredential(t, ctx, api)

	// PUT user credentials for userA (admin action).
	res, err = api.Do(ctx, http.MethodPut,
		fmt.Sprintf("/v1/cli-credentials/%s/user-credentials/%s", credID, userA.ID),
		map[string]any{
			"env": map[string]string{
				"E2E_USER_KEY": "user-a-value-" + helpers.RandHex8(),
			},
		})
	mustOKCLI(t, "PUT user-credentials for userA", res, err, http.StatusOK)

	// GET user credentials for userA → 200.
	res, err = api.GET(ctx, fmt.Sprintf("/v1/cli-credentials/%s/user-credentials/%s", credID, userA.ID))
	mustOKCLI(t, "GET user-credentials for userA", res, err, http.StatusOK)

	// GET user credentials for userB (never set) → 404 (no entry).
	res, err = api.GET(ctx, fmt.Sprintf("/v1/cli-credentials/%s/user-credentials/%s", credID, userB.ID))
	if err != nil {
		t.Fatalf("GET user-credentials for userB: %v", err)
	}
	if res.Status != http.StatusNotFound {
		t.Fatalf("userB credentials status %d, want 404 (never set)", res.Status)
	}
}

// TestSecureCLIAdminGate — non-admin POST → 403.
func TestSecureCLIAdminGate(t *testing.T) {
	helpers.MustMigrateClean(t)
	helpers.ResetDB(t)

	gw := helpers.StartGateway(t)
	api := helpers.NewAPIClient()
	api.BaseURL = gw.BaseURL

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	rootToken := loginCLI(t, ctx, api, helpers.RootEmail(), helpers.RootPassword())
	api.SetToken(rootToken)

	memberEmail := helpers.RandEmail("m")
	memberPass := "TestPass1!-" + helpers.RandHex8()
	res, err := api.POST(ctx, "/v1/users", map[string]any{
		"email":    memberEmail,
		"password": memberPass,
		"role":     "member",
	})
	mustOKCLI(t, "POST /v1/users (member)", res, err, http.StatusCreated)

	memberToken := loginCLI(t, ctx, api, memberEmail, memberPass)
	api.SetToken(memberToken)

	res, err = api.POST(ctx, "/v1/cli-credentials", map[string]any{
		"binary_name": "echo",
		"description": "unauthorized",
		"env":         map[string]string{"KEY": "val"},
		"enabled":     true,
	})
	if err != nil {
		t.Fatalf("POST /v1/cli-credentials as member: %v", err)
	}
	if res.Status != http.StatusForbidden {
		t.Fatalf("member create credential: status %d, want 403, body=%s", res.Status, string(res.Body))
	}
}

// TestSecureCLICheckBinaryEndpoint — POST /v1/cli-credentials/check-binary → 200 + found field.
func TestSecureCLICheckBinaryEndpoint(t *testing.T) {
	helpers.MustMigrateClean(t)
	helpers.ResetDB(t)

	gw := helpers.StartGateway(t)
	api := helpers.NewAPIClient()
	api.BaseURL = gw.BaseURL

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	rootToken := loginCLI(t, ctx, api, helpers.RootEmail(), helpers.RootPassword())
	api.SetToken(rootToken)

	// "echo" should be found on every test host.
	res, err := api.POST(ctx, "/v1/cli-credentials/check-binary", map[string]any{
		"binary_name": "echo",
	})
	mustOKCLI(t, "POST /v1/cli-credentials/check-binary", res, err, http.StatusOK)

	var result struct {
		Found bool   `json:"found"`
		Path  string `json:"path"`
	}
	mustJSONCLI(t, res, &result)
	if !result.Found {
		t.Logf("check-binary: echo not found (PATH may differ in test container) — body=%s", string(res.Body))
	}
}

// TestSecureCLIDryRunAccepted — POST /v1/cli-credentials/{id}/test → 200 + results slice.
func TestSecureCLIDryRunAccepted(t *testing.T) {
	helpers.MustMigrateClean(t)
	helpers.ResetDB(t)

	gw := helpers.StartGateway(t)
	api := helpers.NewAPIClient()
	api.BaseURL = gw.BaseURL

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	rootToken := loginCLI(t, ctx, api, helpers.RootEmail(), helpers.RootPassword())
	api.SetToken(rootToken)

	credID := createCLICredential(t, ctx, api)

	res, err := api.POST(ctx, fmt.Sprintf("/v1/cli-credentials/%s/test", credID), map[string]any{
		"test_commands": []string{
			"echo hello",
			"echo world",
		},
	})
	mustOKCLI(t, "POST /v1/cli-credentials/{id}/test", res, err, http.StatusOK)

	var result struct {
		Results []struct {
			Command string `json:"command"`
			Allowed bool   `json:"allowed"`
		} `json:"results"`
	}
	mustJSONCLI(t, res, &result)
	if len(result.Results) != 2 {
		t.Fatalf("dry-run: got %d results, want 2; body=%s", len(result.Results), string(res.Body))
	}
}
