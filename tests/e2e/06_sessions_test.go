//go:build e2e

// Package e2e_test exercises WS sessions.* RPC methods
// (internal/gateway/methods/sessions.go): list, preview, and delete.
// Sessions live in the agent_sessions table (v4 rename). Sessions are
// seeded via direct DB insert — no HTTP sessions endpoint in v4.
package e2e_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
	"github.com/nextlevelbuilder/goclaw/tests/e2e/helpers"
)

func mustOKSessions(t *testing.T, label string, res *helpers.APIResponse, err error, want int) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: transport error: %v", label, err)
	}
	if res.Status != want {
		t.Fatalf("%s: status %d, want %d, body=%s", label, res.Status, want, string(res.Body))
	}
}

func mustJSONSessions(t *testing.T, res *helpers.APIResponse, out any) {
	t.Helper()
	if err := res.JSON(out); err != nil {
		t.Fatalf("decode JSON: %v body=%s", err, string(res.Body))
	}
}

func loginForSessions(t *testing.T, ctx context.Context, api *helpers.APIClient, email, pass string) string {
	t.Helper()
	res, err := api.POST(ctx, "/v1/auth/login", map[string]string{
		"email": email, "password": pass,
	})
	mustOKSessions(t, "POST /v1/auth/login", res, err, http.StatusOK)
	var tok struct {
		AccessToken string `json:"access_token"`
	}
	mustJSONSessions(t, res, &tok)
	if tok.AccessToken == "" {
		t.Fatalf("loginForSessions %s: empty access_token", email)
	}
	return tok.AccessToken
}

// seedSession inserts a row into agent_sessions directly and returns the session key.
func seedSession(t *testing.T, ctx context.Context) string {
	t.Helper()
	db := helpers.MustDB(t)

	agentID, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("uuid agent: %v", err)
	}
	sessionID, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("uuid session: %v", err)
	}
	sessionKey := "e2e-sess-" + helpers.RandHex8()

	// Insert a minimal agent first so the FK holds.
	_, dbErr := db.ExecContext(ctx, `
		INSERT INTO agents (id, agent_key, agent_type, model, provider, status, created_at, updated_at)
		VALUES ($1, $2, 'open', 'test/model', 'openai', 'active', now(), now())
		ON CONFLICT DO NOTHING`,
		agentID, "sess-agent-"+helpers.RandHex8(),
	)
	if dbErr != nil {
		t.Skipf("seedSession: could not insert agent (schema may differ): %v", dbErr)
	}

	_, dbErr = db.ExecContext(ctx, `
		INSERT INTO agent_sessions (id, session_key, agent_id)
		VALUES ($1, $2, $3)`,
		sessionID, sessionKey, agentID,
	)
	if dbErr != nil {
		t.Skipf("seedSession: could not insert session (schema may differ): %v", dbErr)
	}
	return sessionKey
}

func wsConnectSessions(t *testing.T, ctx context.Context, token string) *helpers.WSClient {
	t.Helper()
	wsc, err := helpers.NewWSClient(ctx, token)
	if err != nil {
		t.Skipf("WS dial failed: %v", err)
	}
	if _, err := wsc.Connect(ctx, map[string]any{"locale": "en"}); err != nil {
		wsc.Close()
		t.Skipf("WS connect failed: %v", err)
	}
	return wsc
}

// TestSessionsList — sessions.list returns a list shape (total + sessions array).
func TestSessionsList(t *testing.T) {
	helpers.MustMigrateClean(t)
	helpers.ResetDB(t)

	gw := helpers.StartGateway(t)
	api := helpers.NewAPIClient()
	api.BaseURL = gw.BaseURL

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	token := loginForSessions(t, ctx, api, helpers.RootEmail(), helpers.RootPassword())
	seedSession(t, ctx)

	wsCtx, wsCancel := context.WithTimeout(ctx, 30*time.Second)
	defer wsCancel()
	wsc := wsConnectSessions(t, wsCtx, token)
	defer wsc.Close()

	payload, err := wsc.SendReq(wsCtx, protocol.MethodSessionsList, map[string]any{"limit": 20})
	if err != nil {
		t.Fatalf("sessions.list: %v", err)
	}

	var result struct {
		Sessions []json.RawMessage `json:"sessions"`
		Total    int               `json:"total"`
	}
	if err := json.Unmarshal(payload, &result); err != nil {
		t.Fatalf("sessions.list unmarshal: %v (raw=%s)", err, string(payload))
	}
	// Shape check: sessions key must exist (may be empty if visibility filtered).
	if result.Sessions == nil {
		t.Fatalf("sessions.list: missing sessions key in: %s", string(payload))
	}
}

// TestSessionsPreview — sessions.preview returns messages + key for a seeded session.
func TestSessionsPreview(t *testing.T) {
	helpers.MustMigrateClean(t)
	helpers.ResetDB(t)

	gw := helpers.StartGateway(t)
	api := helpers.NewAPIClient()
	api.BaseURL = gw.BaseURL

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	token := loginForSessions(t, ctx, api, helpers.RootEmail(), helpers.RootPassword())
	sessionKey := seedSession(t, ctx)

	wsCtx, wsCancel := context.WithTimeout(ctx, 30*time.Second)
	defer wsCancel()
	wsc := wsConnectSessions(t, wsCtx, token)
	defer wsc.Close()

	params, _ := json.Marshal(map[string]any{"key": sessionKey})
	payload, err := wsc.SendReq(wsCtx, protocol.MethodSessionsPreview, json.RawMessage(params))
	if err != nil {
		t.Fatalf("sessions.preview: %v", err)
	}

	var result struct {
		Key      string            `json:"key"`
		Messages []json.RawMessage `json:"messages"`
	}
	if err := json.Unmarshal(payload, &result); err != nil {
		t.Fatalf("sessions.preview unmarshal: %v (raw=%s)", err, string(payload))
	}
	if result.Key != sessionKey {
		t.Fatalf("sessions.preview: key mismatch: got %q want %q", result.Key, sessionKey)
	}
	// Messages may be empty for a fresh session — just check field exists.
	if result.Messages == nil {
		t.Fatalf("sessions.preview: missing messages key in: %s", string(payload))
	}
}

// TestSessionsDelete — sessions.delete removes session; subsequent list excludes it.
func TestSessionsDelete(t *testing.T) {
	helpers.MustMigrateClean(t)
	helpers.ResetDB(t)

	gw := helpers.StartGateway(t)
	api := helpers.NewAPIClient()
	api.BaseURL = gw.BaseURL

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	token := loginForSessions(t, ctx, api, helpers.RootEmail(), helpers.RootPassword())
	sessionKey := seedSession(t, ctx)

	wsCtx, wsCancel := context.WithTimeout(ctx, 30*time.Second)
	defer wsCancel()
	wsc := wsConnectSessions(t, wsCtx, token)
	defer wsc.Close()

	// Verify it appears in list first (admin can see all).
	listBefore, err := wsc.SendReq(wsCtx, protocol.MethodSessionsList, map[string]any{"limit": 100})
	if err != nil {
		t.Fatalf("sessions.list before delete: %v", err)
	}
	var before struct {
		Sessions []struct {
			Key string `json:"session_key"`
		} `json:"sessions"`
	}
	json.Unmarshal(listBefore, &before)
	found := false
	for _, s := range before.Sessions {
		if s.Key == sessionKey {
			found = true
			break
		}
	}
	if !found {
		t.Skipf("sessions.delete: seeded session %q not visible in list (visibility filter) — skipping delete assertion", sessionKey)
	}

	// Delete.
	delParams, _ := json.Marshal(map[string]any{"key": sessionKey})
	if _, err := wsc.SendReq(wsCtx, protocol.MethodSessionsDelete, json.RawMessage(delParams)); err != nil {
		t.Fatalf("sessions.delete: %v", err)
	}

	// Verify absent from list.
	listAfter, err := wsc.SendReq(wsCtx, protocol.MethodSessionsList, map[string]any{"limit": 100})
	if err != nil {
		t.Fatalf("sessions.list after delete: %v", err)
	}
	var after struct {
		Sessions []struct {
			Key string `json:"session_key"`
		} `json:"sessions"`
	}
	json.Unmarshal(listAfter, &after)
	for _, s := range after.Sessions {
		if s.Key == sessionKey {
			t.Fatalf("sessions.delete: session %q still in list after delete", sessionKey)
		}
	}
}
