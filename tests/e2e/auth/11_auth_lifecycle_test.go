//go:build e2e

// Package auth_e2e_test exercises the v4 password-auth HTTP lifecycle that the
// React frontend depends on: login → me → refresh → patch users/me →
// change-password → relogin → logout. Catches FE-API contract drift.
//
// Browser-level interaction (route guards, redirects, form submission) is left
// to Phase 14 because the gateway does not yet serve the FE bundle statically.
package auth_e2e_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/tests/e2e/helpers"
)

func TestAuthLifecycle(t *testing.T) {
	helpers.MustMigrateClean(t)
	helpers.ResetDB(t)

	gw := helpers.StartGateway(t)
	api := helpers.NewAPIClient()
	api.BaseURL = gw.BaseURL

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// 1. /v1/bootstrap/status — already bootstrapped (root seeded by ResetDB).
	res, err := api.GET(ctx, "/v1/bootstrap/status")
	mustOK(t, "GET /v1/bootstrap/status", res, err, http.StatusOK)
	var status struct {
		Bootstrapped bool `json:"bootstrapped"`
	}
	mustJSON(t, res, &status)
	if !status.Bootstrapped {
		t.Fatalf("bootstrap_status: expected bootstrapped=true after seedRootUser, got false")
	}

	// 2. /v1/auth/login with seeded credentials → tokens.
	res, err = api.POST(ctx, "/v1/auth/login", map[string]string{
		"email":    helpers.RootEmail(),
		"password": helpers.RootPassword(),
	})
	mustOK(t, "POST /v1/auth/login", res, err, http.StatusOK)
	var login loginResp
	mustJSON(t, res, &login)
	if login.AccessToken == "" || login.RefreshToken == "" {
		t.Fatalf("login: empty tokens: %+v", login)
	}
	if login.UserID == "" {
		t.Fatalf("login: empty user_id")
	}

	// 3. /v1/auth/me with access token → user info.
	api.SetToken(login.AccessToken)
	res, err = api.GET(ctx, "/v1/auth/me")
	mustOK(t, "GET /v1/auth/me", res, err, http.StatusOK)
	var me meResp
	mustJSON(t, res, &me)
	if me.Email != helpers.RootEmail() {
		t.Fatalf("me.email = %q, want %q", me.Email, helpers.RootEmail())
	}
	if me.Role != "root" {
		t.Fatalf("me.role = %q, want root", me.Role)
	}

	// 4. /v1/auth/refresh → new tokens, old refresh now revoked.
	api.SetToken("") // refresh doesn't need access auth
	res, err = api.POST(ctx, "/v1/auth/refresh", map[string]string{
		"refresh_token": login.RefreshToken,
	})
	mustOK(t, "POST /v1/auth/refresh", res, err, http.StatusOK)
	var rotated loginResp
	mustJSON(t, res, &rotated)
	if rotated.AccessToken == "" || rotated.RefreshToken == "" {
		t.Fatalf("refresh: empty rotated tokens")
	}
	if rotated.RefreshToken == login.RefreshToken {
		t.Fatalf("refresh: refresh_token did not rotate")
	}

	// 4b. Old refresh token must now be rejected (rotate-on-use).
	res, err = api.POST(ctx, "/v1/auth/refresh", map[string]string{
		"refresh_token": login.RefreshToken,
	})
	if err != nil {
		t.Fatalf("POST /v1/auth/refresh (old): %v", err)
	}
	if res.Status != http.StatusUnauthorized {
		t.Fatalf("re-using old refresh: status %d, want 401", res.Status)
	}

	// 5. /v1/users/me PATCH with new access token → display_name updated.
	api.SetToken(rotated.AccessToken)
	res, err = api.PATCH(ctx, "/v1/users/me", map[string]string{
		"display_name": "Lifecycle Tester",
	})
	mustOK(t, "PATCH /v1/users/me", res, err, http.StatusOK)
	mustJSON(t, res, &me)
	if me.DisplayName != "Lifecycle Tester" {
		t.Fatalf("after PATCH: display_name = %q, want %q", me.DisplayName, "Lifecycle Tester")
	}

	// 5b. PATCH with too-short display_name → 400.
	res, err = api.PATCH(ctx, "/v1/users/me", map[string]string{"display_name": "x"})
	if err != nil {
		t.Fatalf("PATCH /v1/users/me short: %v", err)
	}
	if res.Status != http.StatusBadRequest {
		t.Fatalf("display_name=x: status %d, want 400", res.Status)
	}

	// 6. /v1/auth/change-password with WRONG current → 401.
	const newPass = "NewLifecyclePass!2026"
	res, err = api.POST(ctx, "/v1/auth/change-password", map[string]string{
		"current_password": "totally-wrong",
		"new_password":     newPass,
	})
	if err != nil {
		t.Fatalf("change-password (wrong current): %v", err)
	}
	if res.Status != http.StatusUnauthorized {
		t.Fatalf("change-password (wrong current): status %d, want 401", res.Status)
	}

	// 6b. /v1/auth/change-password with correct current → 204 + revoke all sessions.
	res, err = api.POST(ctx, "/v1/auth/change-password", map[string]string{
		"current_password": helpers.RootPassword(),
		"new_password":     newPass,
	})
	mustOK(t, "POST /v1/auth/change-password", res, err, http.StatusNoContent)

	// 6c. JWT access tokens are stateless — they remain valid until exp even
	// after RevokeAllForUser. The refresh-token revocation is what actually
	// kills future sessions; the short access TTL (15min default) bounds the
	// blast radius. Verify the refresh side is locked down:
	api.SetToken("")
	res, err = api.POST(ctx, "/v1/auth/refresh", map[string]string{
		"refresh_token": rotated.RefreshToken,
	})
	if err != nil {
		t.Fatalf("POST /v1/auth/refresh post-change: %v", err)
	}
	if res.Status != http.StatusUnauthorized {
		t.Fatalf("post-change refresh: status %d, want 401 (refresh should be revoked)", res.Status)
	}

	// 7. Login with NEW password → fresh tokens.
	res, err = api.POST(ctx, "/v1/auth/login", map[string]string{
		"email":    helpers.RootEmail(),
		"password": newPass,
	})
	mustOK(t, "POST /v1/auth/login (new pass)", res, err, http.StatusOK)
	var fresh loginResp
	mustJSON(t, res, &fresh)
	if fresh.AccessToken == "" {
		t.Fatalf("relogin: empty access token")
	}

	// 7b. Login with OLD password → 401.
	res, err = api.POST(ctx, "/v1/auth/login", map[string]string{
		"email":    helpers.RootEmail(),
		"password": helpers.RootPassword(),
	})
	if err != nil {
		t.Fatalf("login (old pass): %v", err)
	}
	if res.Status != http.StatusUnauthorized {
		t.Fatalf("login (old pass): status %d, want 401", res.Status)
	}

	// 8. /v1/auth/logout → 204 + refresh sessions revoked.
	api.SetToken(fresh.AccessToken)
	res, err = api.POST(ctx, "/v1/auth/logout", nil)
	mustOK(t, "POST /v1/auth/logout", res, err, http.StatusNoContent)

	// 8b. Refresh tokens issued during the previous login must now be revoked.
	api.SetToken("")
	res, err = api.POST(ctx, "/v1/auth/refresh", map[string]string{
		"refresh_token": fresh.RefreshToken,
	})
	if err != nil {
		t.Fatalf("POST /v1/auth/refresh post-logout: %v", err)
	}
	if res.Status != http.StatusUnauthorized {
		t.Fatalf("post-logout refresh: status %d, want 401", res.Status)
	}
}

// --- helpers ---

type loginResp struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	UserID       string `json:"user_id"`
	Role         string `json:"role"`
}

type meResp struct {
	UserID      string `json:"user_id"`
	Email       string `json:"email"`
	Role        string `json:"role"`
	Status      string `json:"status"`
	DisplayName string `json:"display_name"`
}

func mustOK(t *testing.T, label string, res *helpers.APIResponse, err error, want int) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: transport error: %v", label, err)
	}
	if res.Status != want {
		t.Fatalf("%s: status %d, want %d, body=%s", label, res.Status, want, string(res.Body))
	}
}

func mustJSON(t *testing.T, res *helpers.APIResponse, out any) {
	t.Helper()
	if err := res.JSON(out); err != nil {
		t.Fatalf("decode JSON: %v body=%s", err, string(res.Body))
	}
}
