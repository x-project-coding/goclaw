//go:build e2e

// Package e2e_test exercises the Users CRUD HTTP endpoints
// (internal/http/users.go + internal/http/users_handlers.go): listing,
// creation, get, patch, delete, RBAC guards, and password-hash absence in
// responses.
package e2e_test

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/tests/e2e/helpers"
)

// loginUser is a local helper: POST /v1/auth/login → access token or fatal.
func loginUser(t *testing.T, ctx context.Context, api *helpers.APIClient, email, password string) string {
	t.Helper()
	res, err := api.POST(ctx, "/v1/auth/login", map[string]string{
		"email":    email,
		"password": password,
	})
	mustOKUsers(t, "POST /v1/auth/login", res, err, http.StatusOK)
	var tok struct {
		AccessToken string `json:"access_token"`
	}
	mustJSONUsers(t, res, &tok)
	if tok.AccessToken == "" {
		t.Fatalf("loginUser %s: empty access_token", email)
	}
	return tok.AccessToken
}

// mustOKUsers / mustJSONUsers are file-local helpers (pattern from auth lifecycle test).
func mustOKUsers(t *testing.T, label string, res *helpers.APIResponse, err error, want int) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: transport error: %v", label, err)
	}
	if res.Status != want {
		t.Fatalf("%s: status %d, want %d, body=%s", label, res.Status, want, string(res.Body))
	}
}

func mustJSONUsers(t *testing.T, res *helpers.APIResponse, out any) {
	t.Helper()
	if err := res.JSON(out); err != nil {
		t.Fatalf("decode JSON: %v body=%s", err, string(res.Body))
	}
}

// createMemberViaRoot uses the root token to POST /v1/users with role=member.
// Returns the created user ID.
func createMemberViaRoot(t *testing.T, ctx context.Context, api *helpers.APIClient, rootToken string) string {
	t.Helper()
	api.SetToken(rootToken)
	email := helpers.RandEmail("m")
	pass := "TestPass1!-" + helpers.RandHex8()
	res, err := api.POST(ctx, "/v1/users", map[string]any{
		"email":    email,
		"password": pass,
		"role":     "member",
	})
	mustOKUsers(t, "POST /v1/users (member)", res, err, http.StatusCreated)
	var u struct{ ID string `json:"id"` }
	mustJSONUsers(t, res, &u)
	return u.ID
}

// TestUsersListAdminSeesAll — root login → create 3 members → GET /v1/users → len ≥ 4.
func TestUsersListAdminSeesAll(t *testing.T) {
	helpers.MustMigrateClean(t)
	helpers.ResetDB(t)

	gw := helpers.StartGateway(t)
	api := helpers.NewAPIClient()
	api.BaseURL = gw.BaseURL

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	rootToken := loginUser(t, ctx, api, helpers.RootEmail(), helpers.RootPassword())

	// Create 3 fresh members.
	for i := 0; i < 3; i++ {
		createMemberViaRoot(t, ctx, api, rootToken)
	}

	api.SetToken(rootToken)
	res, err := api.GET(ctx, "/v1/users")
	mustOKUsers(t, "GET /v1/users (admin)", res, err, http.StatusOK)
	var resp struct {
		Users []map[string]any `json:"users"`
	}
	mustJSONUsers(t, res, &resp)
	if len(resp.Users) < 4 {
		t.Fatalf("admin list: got %d users, want ≥ 4 (root + 3 created)", len(resp.Users))
	}
}

// TestUsersListMemberSeesSelfOnly — login as member → GET /v1/users → exactly 1.
func TestUsersListMemberSeesSelfOnly(t *testing.T) {
	helpers.MustMigrateClean(t)
	helpers.ResetDB(t)

	gw := helpers.StartGateway(t)
	api := helpers.NewAPIClient()
	api.BaseURL = gw.BaseURL

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	rootToken := loginUser(t, ctx, api, helpers.RootEmail(), helpers.RootPassword())

	// Seed a member via root, then log in as that member.
	memberEmail := helpers.RandEmail("m")
	memberPass := "TestPass1!-" + helpers.RandHex8()
	api.SetToken(rootToken)
	res, err := api.POST(ctx, "/v1/users", map[string]any{
		"email":    memberEmail,
		"password": memberPass,
		"role":     "member",
	})
	mustOKUsers(t, "POST /v1/users", res, err, http.StatusCreated)

	memberToken := loginUser(t, ctx, api, memberEmail, memberPass)
	api.SetToken(memberToken)
	res, err = api.GET(ctx, "/v1/users")
	mustOKUsers(t, "GET /v1/users (member)", res, err, http.StatusOK)
	var resp struct {
		Users []map[string]any `json:"users"`
	}
	mustJSONUsers(t, res, &resp)
	if len(resp.Users) != 1 {
		t.Fatalf("member list: got %d users, want exactly 1 (self only)", len(resp.Users))
	}
}

// TestUsersCreateRootRejected — POST /v1/users with role=root → 400.
func TestUsersCreateRootRejected(t *testing.T) {
	helpers.MustMigrateClean(t)
	helpers.ResetDB(t)

	gw := helpers.StartGateway(t)
	api := helpers.NewAPIClient()
	api.BaseURL = gw.BaseURL

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	rootToken := loginUser(t, ctx, api, helpers.RootEmail(), helpers.RootPassword())
	api.SetToken(rootToken)

	res, err := api.POST(ctx, "/v1/users", map[string]any{
		"email":    helpers.RandEmail("r"),
		"password": "TestPass1!-" + helpers.RandHex8(),
		"role":     "root",
	})
	if err != nil {
		t.Fatalf("POST /v1/users (root role): %v", err)
	}
	if res.Status != http.StatusBadRequest {
		t.Fatalf("create root role: status %d, want 400, body=%s", res.Status, string(res.Body))
	}
}

// TestUsersCreateAdminCanCreateMember — login as admin → POST /v1/users role=member → 201.
func TestUsersCreateAdminCanCreateMember(t *testing.T) {
	helpers.MustMigrateClean(t)
	helpers.ResetDB(t)

	gw := helpers.StartGateway(t)
	api := helpers.NewAPIClient()
	api.BaseURL = gw.BaseURL

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	rootToken := loginUser(t, ctx, api, helpers.RootEmail(), helpers.RootPassword())

	// Root creates an admin.
	api.SetToken(rootToken)
	adminEmail := helpers.RandEmail("a")
	adminPass := "AdminPass1!-" + helpers.RandHex8()
	res, err := api.POST(ctx, "/v1/users", map[string]any{
		"email":    adminEmail,
		"password": adminPass,
		"role":     "admin",
	})
	mustOKUsers(t, "POST /v1/users (admin by root)", res, err, http.StatusCreated)

	adminToken := loginUser(t, ctx, api, adminEmail, adminPass)

	// Admin creates a member.
	api.SetToken(adminToken)
	res, err = api.POST(ctx, "/v1/users", map[string]any{
		"email":    helpers.RandEmail("m"),
		"password": "TestPass1!-" + helpers.RandHex8(),
		"role":     "member",
	})
	mustOKUsers(t, "POST /v1/users (member by admin)", res, err, http.StatusCreated)
}

// TestUsersGetSelfAllowed — member → GET /v1/users/{ownID} → 200.
func TestUsersGetSelfAllowed(t *testing.T) {
	helpers.MustMigrateClean(t)
	helpers.ResetDB(t)

	gw := helpers.StartGateway(t)
	api := helpers.NewAPIClient()
	api.BaseURL = gw.BaseURL

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	rootToken := loginUser(t, ctx, api, helpers.RootEmail(), helpers.RootPassword())

	memberEmail := helpers.RandEmail("m")
	memberPass := "TestPass1!-" + helpers.RandHex8()
	api.SetToken(rootToken)
	res, err := api.POST(ctx, "/v1/users", map[string]any{
		"email":    memberEmail,
		"password": memberPass,
		"role":     "member",
	})
	mustOKUsers(t, "POST /v1/users", res, err, http.StatusCreated)
	var created struct{ ID string `json:"id"` }
	mustJSONUsers(t, res, &created)

	memberToken := loginUser(t, ctx, api, memberEmail, memberPass)
	api.SetToken(memberToken)
	res, err = api.GET(ctx, fmt.Sprintf("/v1/users/%s", created.ID))
	mustOKUsers(t, "GET /v1/users/{ownID}", res, err, http.StatusOK)
}

// TestUsersGetOtherReturns404ForMember — member → GET other user → 404 (no enumeration).
func TestUsersGetOtherReturns404ForMember(t *testing.T) {
	helpers.MustMigrateClean(t)
	helpers.ResetDB(t)

	gw := helpers.StartGateway(t)
	api := helpers.NewAPIClient()
	api.BaseURL = gw.BaseURL

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	rootToken := loginUser(t, ctx, api, helpers.RootEmail(), helpers.RootPassword())

	// Create two members.
	api.SetToken(rootToken)
	m1Email := helpers.RandEmail("m1")
	m1Pass := "TestPass1!-" + helpers.RandHex8()
	res, err := api.POST(ctx, "/v1/users", map[string]any{
		"email": m1Email, "password": m1Pass, "role": "member",
	})
	mustOKUsers(t, "create m1", res, err, http.StatusCreated)

	m2Email := helpers.RandEmail("m2")
	m2Pass := "TestPass1!-" + helpers.RandHex8()
	res, err = api.POST(ctx, "/v1/users", map[string]any{
		"email": m2Email, "password": m2Pass, "role": "member",
	})
	mustOKUsers(t, "create m2", res, err, http.StatusCreated)
	var m2 struct{ ID string `json:"id"` }
	mustJSONUsers(t, res, &m2)

	m1Token := loginUser(t, ctx, api, m1Email, m1Pass)
	api.SetToken(m1Token)
	res, err = api.GET(ctx, fmt.Sprintf("/v1/users/%s", m2.ID))
	if err != nil {
		t.Fatalf("GET /v1/users/{otherID}: %v", err)
	}
	if res.Status != http.StatusNotFound {
		t.Fatalf("member GET other: status %d, want 404", res.Status)
	}
}

// TestUsersPatchDisplayNameByMember — member can change own display_name; role change rejected.
func TestUsersPatchDisplayNameByMember(t *testing.T) {
	helpers.MustMigrateClean(t)
	helpers.ResetDB(t)

	gw := helpers.StartGateway(t)
	api := helpers.NewAPIClient()
	api.BaseURL = gw.BaseURL

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	rootToken := loginUser(t, ctx, api, helpers.RootEmail(), helpers.RootPassword())

	memberEmail := helpers.RandEmail("m")
	memberPass := "TestPass1!-" + helpers.RandHex8()
	api.SetToken(rootToken)
	res, err := api.POST(ctx, "/v1/users", map[string]any{
		"email": memberEmail, "password": memberPass, "role": "member",
	})
	mustOKUsers(t, "create member", res, err, http.StatusCreated)
	var created struct{ ID string `json:"id"` }
	mustJSONUsers(t, res, &created)

	memberToken := loginUser(t, ctx, api, memberEmail, memberPass)
	api.SetToken(memberToken)

	// Patch display_name → 200.
	newName := "Updated " + helpers.RandHex8()
	res, err = api.PATCH(ctx, fmt.Sprintf("/v1/users/%s", created.ID), map[string]any{
		"display_name": &newName,
	})
	mustOKUsers(t, "PATCH display_name", res, err, http.StatusOK)
	var updated struct{ DisplayName string `json:"display_name"` }
	mustJSONUsers(t, res, &updated)
	if updated.DisplayName != newName {
		t.Fatalf("display_name: got %q want %q", updated.DisplayName, newName)
	}

	// Role change by non-root → 403.
	newRole := "admin"
	res, err = api.PATCH(ctx, fmt.Sprintf("/v1/users/%s", created.ID), map[string]any{
		"role": &newRole,
	})
	if err != nil {
		t.Fatalf("PATCH role: %v", err)
	}
	if res.Status != http.StatusForbidden {
		t.Fatalf("member role change: status %d, want 403", res.Status)
	}
}

// TestUsersDeleteRootRejected — DELETE /v1/users/{rootID} → 403.
func TestUsersDeleteRootRejected(t *testing.T) {
	helpers.MustMigrateClean(t)
	helpers.ResetDB(t)

	gw := helpers.StartGateway(t)
	api := helpers.NewAPIClient()
	api.BaseURL = gw.BaseURL

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	rootToken := loginUser(t, ctx, api, helpers.RootEmail(), helpers.RootPassword())
	api.SetToken(rootToken)

	// Get root's own ID from /v1/auth/me.
	res, err := api.GET(ctx, "/v1/auth/me")
	mustOKUsers(t, "GET /v1/auth/me", res, err, http.StatusOK)
	var me struct{ UserID string `json:"user_id"` }
	mustJSONUsers(t, res, &me)

	res, err = api.DELETE(ctx, fmt.Sprintf("/v1/users/%s", me.UserID))
	if err != nil {
		t.Fatalf("DELETE /v1/users/{rootID}: %v", err)
	}
	if res.Status != http.StatusForbidden {
		t.Fatalf("delete root: status %d, want 403, body=%s", res.Status, string(res.Body))
	}
}

// TestUsersPasswordHashNeverInResponse — GET response bytes must not contain "password_hash".
func TestUsersPasswordHashNeverInResponse(t *testing.T) {
	helpers.MustMigrateClean(t)
	helpers.ResetDB(t)

	gw := helpers.StartGateway(t)
	api := helpers.NewAPIClient()
	api.BaseURL = gw.BaseURL

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	rootToken := loginUser(t, ctx, api, helpers.RootEmail(), helpers.RootPassword())
	memberID := createMemberViaRoot(t, ctx, api, rootToken)

	// GET the new user as root.
	api.SetToken(rootToken)
	res, err := api.GET(ctx, fmt.Sprintf("/v1/users/%s", memberID))
	mustOKUsers(t, "GET /v1/users/{id}", res, err, http.StatusOK)

	if bytes.Contains(res.Body, []byte("password_hash")) {
		t.Fatalf("response body contains 'password_hash' — must never be exposed: %s", string(res.Body))
	}

	// Also check list endpoint.
	res, err = api.GET(ctx, "/v1/users")
	mustOKUsers(t, "GET /v1/users (list)", res, err, http.StatusOK)
	if bytes.Contains(res.Body, []byte("password_hash")) {
		t.Fatalf("list response contains 'password_hash' — must never be exposed: %s", string(res.Body))
	}
}
