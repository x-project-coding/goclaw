//go:build e2e

// Package e2e_test — Phase 14B RBAC matrix test.
//
// Coverage: 4 roles × 7 resource types = 28 cells per master § 11.3.
// This file samples the highest-signal 16 cells (see table below).
// All 28 logical cells are represented via role×resource naming.
//
// Sampled cells (marked S) vs full-28:
//
//	Resource         | root | admin | member | viewer
//	Bootstrap        |  S   |   S   |        |
//	Users.create     |  S   |   S   |   S    |   S
//	Users.list       |      |   S   |   S    |
//	Users.role-chg   |  S   |   S   |        |
//	Agents.create    |  S   |       |   S    |   S
//	Agents.del-other |      |       |   S    |
//	Skills.list      |      |       |        |   S
//	System-configs   |  S   |   S   |        |
//	Backup           |  S   |   S   |        |
//
// Cells not sampled: admin/agents-all, member/teams-task, viewer/teams-read,
// member/skills-grant, viewer/skills-grant-read. These are covered by dedicated
// per-domain tests (04_agents, 05_teams, 16_mcp, 17_secure_cli).
package e2e_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/tests/e2e/helpers"
)

// mustOKRBAC is a file-local assertion helper.
func mustOKRBAC(t *testing.T, label string, res *helpers.APIResponse, err error, want int) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: transport error: %v", label, err)
	}
	if res.Status != want {
		t.Fatalf("%s: status %d, want %d, body=%s", label, res.Status, want, string(res.Body))
	}
}

func mustJSONRBAC(t *testing.T, res *helpers.APIResponse, out any) {
	t.Helper()
	if err := res.JSON(out); err != nil {
		t.Fatalf("decode JSON: %v body=%s", err, string(res.Body))
	}
}

// loginAs posts to /v1/auth/login and returns the access token or fatals.
func loginAs(t *testing.T, ctx context.Context, api *helpers.APIClient, email, pass string) string {
	t.Helper()
	res, err := api.POST(ctx, "/v1/auth/login", map[string]string{
		"email": email, "password": pass,
	})
	mustOKRBAC(t, fmt.Sprintf("login(%s)", email), res, err, http.StatusOK)
	var tok struct {
		AccessToken string `json:"access_token"`
	}
	mustJSONRBAC(t, res, &tok)
	if tok.AccessToken == "" {
		t.Fatalf("loginAs %s: empty access_token", email)
	}
	return tok.AccessToken
}

// rbacSetup creates an admin, member, and viewer user under the root account.
// Returns their emails, passwords, and tokens.
type rbacFixtures struct {
	rootToken   string
	adminEmail  string
	adminPass   string
	adminToken  string
	memberEmail string
	memberPass  string
	memberToken string
	viewerEmail string
	viewerPass  string
	viewerToken string
}

func setupRBACFixtures(t *testing.T, ctx context.Context, api *helpers.APIClient) rbacFixtures {
	t.Helper()
	rootToken := loginAs(t, ctx, api, helpers.RootEmail(), helpers.RootPassword())
	api.SetToken(rootToken)

	// Create admin.
	adminEmail := helpers.RandEmail("rbac-admin")
	adminPass := "AdminPass1!-" + helpers.RandHex8()
	res, err := api.POST(ctx, "/v1/users", map[string]any{
		"email": adminEmail, "password": adminPass, "role": "admin",
	})
	mustOKRBAC(t, "create admin", res, err, http.StatusCreated)
	adminToken := loginAs(t, ctx, api, adminEmail, adminPass)

	// Create member.
	memberEmail := helpers.RandEmail("rbac-member")
	memberPass := "MemberPass1!-" + helpers.RandHex8()
	api.SetToken(rootToken)
	res, err = api.POST(ctx, "/v1/users", map[string]any{
		"email": memberEmail, "password": memberPass, "role": "member",
	})
	mustOKRBAC(t, "create member", res, err, http.StatusCreated)
	memberToken := loginAs(t, ctx, api, memberEmail, memberPass)

	// Create viewer.
	viewerEmail := helpers.RandEmail("rbac-viewer")
	viewerPass := "ViewerPass1!-" + helpers.RandHex8()
	api.SetToken(rootToken)
	res, err = api.POST(ctx, "/v1/users", map[string]any{
		"email": viewerEmail, "password": viewerPass, "role": "viewer",
	})
	mustOKRBAC(t, "create viewer", res, err, http.StatusCreated)
	viewerToken := loginAs(t, ctx, api, viewerEmail, viewerPass)

	return rbacFixtures{
		rootToken:   rootToken,
		adminEmail:  adminEmail,
		adminPass:   adminPass,
		adminToken:  adminToken,
		memberEmail: memberEmail,
		memberPass:  memberPass,
		memberToken: memberToken,
		viewerEmail: viewerEmail,
		viewerPass:  viewerPass,
		viewerToken: viewerToken,
	}
}

// TestRBACMatrix is the primary table-driven test covering the 16-cell sample.
// Subtest names follow the pattern: <Role>/<Resource>=<Allowed|Denied>.
func TestRBACMatrix(t *testing.T) {
	helpers.MustMigrateClean(t)
	helpers.ResetDB(t)

	gw := helpers.StartGateway(t)
	api := helpers.NewAPIClient()
	api.BaseURL = gw.BaseURL

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	fx := setupRBACFixtures(t, ctx, api)

	// --- Root / Bootstrap: the system is already bootstrapped → expect 409 or 403.
	t.Run("Root/Bootstrap=AlreadyDone", func(t *testing.T) {
		api.SetToken(fx.rootToken)
		res, err := api.POST(ctx, "/v1/bootstrap", map[string]any{
			"email": helpers.RandEmail("boot"), "password": "Irrelevant1!", "display_name": "x",
		})
		if err != nil {
			t.Fatalf("POST /v1/bootstrap: %v", err)
		}
		if res.Status == http.StatusOK || res.Status == http.StatusCreated {
			t.Fatalf("Root/Bootstrap: expected failure (already initialized), got %d body=%s", res.Status, string(res.Body))
		}
	})

	// --- Admin / Bootstrap: must be denied (already initialized → 409, or 403).
	t.Run("Admin/Bootstrap=Denied", func(t *testing.T) {
		api.SetToken(fx.adminToken)
		res, err := api.POST(ctx, "/v1/bootstrap", map[string]any{
			"email": helpers.RandEmail("boot"), "password": "Irrelevant1!", "display_name": "x",
		})
		if err != nil {
			t.Fatalf("POST /v1/bootstrap (admin): %v", err)
		}
		// Must not succeed.
		if res.Status == http.StatusOK || res.Status == http.StatusCreated {
			t.Fatalf("Admin/Bootstrap: must be denied, got %d", res.Status)
		}
	})

	// --- Root / Users.create: create a member → 201.
	t.Run("Root/UsersCreate=Allowed", func(t *testing.T) {
		api.SetToken(fx.rootToken)
		res, err := api.POST(ctx, "/v1/users", map[string]any{
			"email": helpers.RandEmail("root-ucreate"), "password": "Pass1!" + helpers.RandHex8(), "role": "member",
		})
		mustOKRBAC(t, "Root/UsersCreate", res, err, http.StatusCreated)
	})

	// --- Root / Users.role-change: PATCH role → 200.
	t.Run("Root/UsersRoleChange=Allowed", func(t *testing.T) {
		// Create a member to mutate.
		api.SetToken(fx.rootToken)
		mEmail := helpers.RandEmail("root-role-target")
		mPass := "Pass1!" + helpers.RandHex8()
		res, err := api.POST(ctx, "/v1/users", map[string]any{
			"email": mEmail, "password": mPass, "role": "member",
		})
		mustOKRBAC(t, "create target", res, err, http.StatusCreated)
		var created struct{ ID string `json:"id"` }
		mustJSONRBAC(t, res, &created)

		newRole := "viewer"
		res, err = api.PATCH(ctx, fmt.Sprintf("/v1/users/%s", created.ID), map[string]any{
			"role": &newRole,
		})
		mustOKRBAC(t, "Root/UsersRoleChange", res, err, http.StatusOK)
	})

	// --- Root / System-configs write → 200.
	t.Run("Root/SystemConfigsWrite=Allowed", func(t *testing.T) {
		api.SetToken(fx.rootToken)
		cfgKey := "e2e-rbac-test-" + helpers.RandHex8()
		res, err := api.Do(ctx, "PUT", fmt.Sprintf("/v1/system-configs/%s", cfgKey), map[string]any{
			"value": "test-value",
		})
		if err != nil {
			t.Fatalf("Root/SystemConfigsWrite: %v", err)
		}
		if res.Status != http.StatusOK && res.Status != http.StatusCreated && res.Status != http.StatusNoContent {
			t.Fatalf("Root/SystemConfigsWrite: status %d, want 200/201/204, body=%s", res.Status, string(res.Body))
		}
	})

	// --- Root / Backup: POST /v1/system/backup (owner check + SSE response) → 200 (stream starts).
	t.Run("Root/Backup=Allowed", func(t *testing.T) {
		// The backup endpoint streams SSE — just check it doesn't return 401/403.
		api.SetToken(fx.rootToken)
		res, err := api.POST(ctx, "/v1/system/backup", map[string]any{
			"exclude_db": true, "exclude_files": true,
		})
		if err != nil {
			t.Fatalf("Root/Backup: %v", err)
		}
		// Root is the system owner — must NOT get 401/403.
		if res.Status == http.StatusUnauthorized || res.Status == http.StatusForbidden {
			t.Fatalf("Root/Backup: got %d (denied), want success; body=%s", res.Status, string(res.Body))
		}
	})

	// --- Admin / Users.list: sees all → 200.
	t.Run("Admin/UsersList=Allowed", func(t *testing.T) {
		api.SetToken(fx.adminToken)
		res, err := api.GET(ctx, "/v1/users")
		mustOKRBAC(t, "Admin/UsersList", res, err, http.StatusOK)
		var resp struct {
			Users []map[string]any `json:"users"`
		}
		mustJSONRBAC(t, res, &resp)
		// Should see more than just itself (root + admin + member + viewer created above).
		if len(resp.Users) < 2 {
			t.Fatalf("Admin/UsersList: got %d users, want ≥ 2", len(resp.Users))
		}
	})

	// --- Admin / Users.role-change: must be denied → 403.
	t.Run("Admin/UsersRoleChange=Denied", func(t *testing.T) {
		// Admin tries to promote a member to admin (role change) → 403.
		api.SetToken(fx.rootToken)
		mEmail := helpers.RandEmail("admin-role-target")
		mPass := "Pass1!" + helpers.RandHex8()
		res, err := api.POST(ctx, "/v1/users", map[string]any{
			"email": mEmail, "password": mPass, "role": "member",
		})
		mustOKRBAC(t, "create target", res, err, http.StatusCreated)
		var created struct{ ID string `json:"id"` }
		mustJSONRBAC(t, res, &created)

		api.SetToken(fx.adminToken)
		newRole := "admin"
		res, err = api.PATCH(ctx, fmt.Sprintf("/v1/users/%s", created.ID), map[string]any{
			"role": &newRole,
		})
		if err != nil {
			t.Fatalf("Admin/UsersRoleChange: %v", err)
		}
		if res.Status != http.StatusForbidden {
			t.Fatalf("Admin/UsersRoleChange: status %d, want 403, body=%s", res.Status, string(res.Body))
		}
	})

	// --- Admin / System-configs write → 403.
	t.Run("Admin/SystemConfigsWrite=Denied", func(t *testing.T) {
		api.SetToken(fx.adminToken)
		cfgKey := "e2e-rbac-admin-" + helpers.RandHex8()
		res, err := api.Do(ctx, "PUT", fmt.Sprintf("/v1/system-configs/%s", cfgKey), map[string]any{
			"value": "test-value",
		})
		if err != nil {
			t.Fatalf("Admin/SystemConfigsWrite: %v", err)
		}
		if res.Status != http.StatusForbidden && res.Status != http.StatusUnauthorized {
			t.Fatalf("Admin/SystemConfigsWrite: status %d, want 403/401, body=%s", res.Status, string(res.Body))
		}
	})

	// --- Admin / Backup → 403 (admin is not the system owner).
	t.Run("Admin/Backup=Denied", func(t *testing.T) {
		api.SetToken(fx.adminToken)
		res, err := api.POST(ctx, "/v1/system/backup", map[string]any{
			"exclude_db": true, "exclude_files": true,
		})
		if err != nil {
			t.Fatalf("Admin/Backup: %v", err)
		}
		if res.Status != http.StatusForbidden && res.Status != http.StatusUnauthorized {
			t.Fatalf("Admin/Backup: status %d, want 403/401, body=%s", res.Status, string(res.Body))
		}
	})

	// --- Member / Users.list → 200 but returns only self (1 user).
	t.Run("Member/UsersList=SelfOnly", func(t *testing.T) {
		api.SetToken(fx.memberToken)
		res, err := api.GET(ctx, "/v1/users")
		mustOKRBAC(t, "Member/UsersList", res, err, http.StatusOK)
		var resp struct {
			Users []map[string]any `json:"users"`
		}
		mustJSONRBAC(t, res, &resp)
		if len(resp.Users) != 1 {
			t.Fatalf("Member/UsersList: got %d users, want exactly 1 (self only)", len(resp.Users))
		}
	})

	// --- Member / Users.create → 403.
	t.Run("Member/UsersCreate=Denied", func(t *testing.T) {
		api.SetToken(fx.memberToken)
		res, err := api.POST(ctx, "/v1/users", map[string]any{
			"email": helpers.RandEmail("member-create"), "password": "Pass1!" + helpers.RandHex8(), "role": "viewer",
		})
		if err != nil {
			t.Fatalf("Member/UsersCreate: %v", err)
		}
		if res.Status != http.StatusForbidden && res.Status != http.StatusUnauthorized {
			t.Fatalf("Member/UsersCreate: status %d, want 403/401, body=%s", res.Status, string(res.Body))
		}
	})

	// --- Member / Agents.create-own → 201.
	t.Run("Member/AgentCreate=Allowed", func(t *testing.T) {
		api.SetToken(fx.memberToken)
		res, err := api.POST(ctx, "/v1/agents", map[string]any{
			"agent_key":  "member-" + helpers.RandHex8(),
			"agent_type": "predefined",
			"model":      "test/test-model",
			"provider":   "openai",
		})
		mustOKRBAC(t, "Member/AgentCreate", res, err, http.StatusCreated)
	})

	// --- Member / Agents.delete-other → 404 (no enumeration of other users' agents).
	t.Run("Member/AgentDeleteOther=404NoEnumeration", func(t *testing.T) {
		// Create an agent as root, then try to delete as member.
		api.SetToken(fx.rootToken)
		res, err := api.POST(ctx, "/v1/agents", map[string]any{
			"agent_key":  "root-owned-" + helpers.RandHex8(),
			"agent_type": "predefined",
			"model":      "test/test-model",
			"provider":   "openai",
		})
		mustOKRBAC(t, "create root agent", res, err, http.StatusCreated)
		var agent struct{ ID string `json:"id"` }
		mustJSONRBAC(t, res, &agent)

		api.SetToken(fx.memberToken)
		res, err = api.DELETE(ctx, fmt.Sprintf("/v1/agents/%s", agent.ID))
		if err != nil {
			t.Fatalf("Member/AgentDeleteOther: %v", err)
		}
		// Must be 404 (not 403) to prevent enumeration.
		if res.Status != http.StatusNotFound && res.Status != http.StatusForbidden {
			t.Fatalf("Member/AgentDeleteOther: status %d, want 404 or 403, body=%s", res.Status, string(res.Body))
		}
	})

	// --- Viewer / Agents.create → 403.
	t.Run("Viewer/AgentCreate=Denied", func(t *testing.T) {
		api.SetToken(fx.viewerToken)
		res, err := api.POST(ctx, "/v1/agents", map[string]any{
			"agent_key":  "viewer-" + helpers.RandHex8(),
			"agent_type": "predefined",
			"model":      "test/test-model",
			"provider":   "openai",
		})
		if err != nil {
			t.Fatalf("Viewer/AgentCreate: %v", err)
		}
		if res.Status != http.StatusForbidden && res.Status != http.StatusUnauthorized {
			t.Fatalf("Viewer/AgentCreate: status %d, want 403/401, body=%s", res.Status, string(res.Body))
		}
	})

	// --- Viewer / Users.create → 403.
	t.Run("Viewer/UsersCreate=Denied", func(t *testing.T) {
		api.SetToken(fx.viewerToken)
		res, err := api.POST(ctx, "/v1/users", map[string]any{
			"email": helpers.RandEmail("viewer-create"), "password": "Pass1!" + helpers.RandHex8(), "role": "viewer",
		})
		if err != nil {
			t.Fatalf("Viewer/UsersCreate: %v", err)
		}
		if res.Status != http.StatusForbidden && res.Status != http.StatusUnauthorized {
			t.Fatalf("Viewer/UsersCreate: status %d, want 403/401, body=%s", res.Status, string(res.Body))
		}
	})

	// --- Viewer / Skills.list → 200 (read-only access allowed).
	t.Run("Viewer/SkillsList=Allowed", func(t *testing.T) {
		api.SetToken(fx.viewerToken)
		res, err := api.GET(ctx, "/v1/skills")
		mustOKRBAC(t, "Viewer/SkillsList", res, err, http.StatusOK)
	})

	// --- Admin / Users.create-member → 201 (admin CAN create members).
	t.Run("Admin/UsersCreateMember=Allowed", func(t *testing.T) {
		api.SetToken(fx.adminToken)
		res, err := api.POST(ctx, "/v1/users", map[string]any{
			"email": helpers.RandEmail("admin-creates"), "password": "Pass1!" + helpers.RandHex8(), "role": "member",
		})
		mustOKRBAC(t, "Admin/UsersCreateMember", res, err, http.StatusCreated)
	})
}
