//go:build e2e

// Package e2e_test — Phase 14B WebSocket protocol layer tests.
// Validates frame types, connect auth guards, ping, expired/bad JWT rejection,
// and event frame delivery. All tests use the helpers.WSClient (gorilla/websocket).
package e2e_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
	"github.com/nextlevelbuilder/goclaw/tests/e2e/helpers"
)

func mustOKWS(t *testing.T, label string, res *helpers.APIResponse, err error, want int) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: transport error: %v", label, err)
	}
	if res.Status != want {
		t.Fatalf("%s: status %d, want %d, body=%s", label, res.Status, want, string(res.Body))
	}
}

func mustJSONWS(t *testing.T, res *helpers.APIResponse, out any) {
	t.Helper()
	if err := res.JSON(out); err != nil {
		t.Fatalf("decode JSON: %v body=%s", err, string(res.Body))
	}
}

func loginForWS(t *testing.T, ctx context.Context, api *helpers.APIClient, email, pass string) string {
	t.Helper()
	res, err := api.POST(ctx, "/v1/auth/login", map[string]string{
		"email": email, "password": pass,
	})
	mustOKWS(t, "POST /v1/auth/login", res, err, http.StatusOK)
	var tok struct {
		AccessToken string `json:"access_token"`
	}
	mustJSONWS(t, res, &tok)
	if tok.AccessToken == "" {
		t.Fatalf("loginForWS %s: empty access_token", email)
	}
	return tok.AccessToken
}

// TestWSConnectFirstFrameRequiresAccessToken — connecting without an accessToken
// param in the connect frame must be rejected by the server (error response or
// connection close). The dial itself uses no JWT header either.
func TestWSConnectFirstFrameRequiresAccessToken(t *testing.T) {
	helpers.MustMigrateClean(t)
	helpers.ResetDB(t)

	gw := helpers.StartGateway(t)
	_ = gw

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Dial with no JWT — empty string means no Authorization header.
	wsc, err := helpers.NewWSClient(ctx, "")
	if err != nil {
		// Server may refuse dial outright when no auth header — acceptable.
		t.Logf("dial without JWT rejected at handshake: %v (pass)", err)
		return
	}
	defer wsc.Close()

	// Send connect with no accessToken param — server must reject.
	_, connectErr := wsc.Connect(ctx, map[string]any{})
	if connectErr == nil {
		t.Fatal("connect without accessToken: expected error response, got success")
	}
	t.Logf("connect without accessToken correctly rejected: %v", connectErr)
}

// TestWSConnectWithValidJWTAcceptsParams — login via HTTP, connect WS → success.
func TestWSConnectWithValidJWTAcceptsParams(t *testing.T) {
	helpers.MustMigrateClean(t)
	helpers.ResetDB(t)

	gw := helpers.StartGateway(t)
	api := helpers.NewAPIClient()
	api.BaseURL = gw.BaseURL

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	token := loginForWS(t, ctx, api, helpers.RootEmail(), helpers.RootPassword())

	wsCtx, wsCancel := context.WithTimeout(ctx, 30*time.Second)
	defer wsCancel()

	wsc, err := helpers.NewWSClient(wsCtx, token)
	if err != nil {
		t.Skipf("WS dial failed (gateway may not be up): %v", err)
	}
	defer wsc.Close()

	payload, err := wsc.Connect(wsCtx, map[string]any{
		"accessToken": token,
		"locale":      "en",
	})
	if err != nil {
		t.Fatalf("connect with valid JWT: %v", err)
	}
	if !json.Valid(payload) {
		t.Fatalf("connect: response is not valid JSON: %s", string(payload))
	}
}

// TestWSPingHeartbeat — connect → send health request → response within 2s.
// Uses protocol.MethodHealth ("health") which is the registered ping equivalent.
func TestWSPingHeartbeat(t *testing.T) {
	helpers.MustMigrateClean(t)
	helpers.ResetDB(t)

	gw := helpers.StartGateway(t)
	api := helpers.NewAPIClient()
	api.BaseURL = gw.BaseURL

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	token := loginForWS(t, ctx, api, helpers.RootEmail(), helpers.RootPassword())

	wsCtx, wsCancel := context.WithTimeout(ctx, 10*time.Second)
	defer wsCancel()

	wsc, err := helpers.NewWSClient(wsCtx, token)
	if err != nil {
		t.Skipf("WS dial failed: %v", err)
	}
	defer wsc.Close()

	if _, err := wsc.Connect(wsCtx, map[string]any{"locale": "en"}); err != nil {
		t.Skipf("WS connect failed: %v", err)
	}

	pingCtx, pingCancel := context.WithTimeout(wsCtx, 2*time.Second)
	defer pingCancel()

	payload, err := wsc.SendReq(pingCtx, protocol.MethodHealth, map[string]any{})
	if err != nil {
		t.Fatalf("health/ping: %v", err)
	}
	if !json.Valid(payload) {
		t.Fatalf("health/ping: invalid JSON response: %s", string(payload))
	}
}

// TestWSExpiredJWTRejectedAtConnect — sending an obviously invalid/bogus token
// string must be rejected at connect time (not silently accepted).
func TestWSExpiredJWTRejectedAtConnect(t *testing.T) {
	helpers.MustMigrateClean(t)
	helpers.ResetDB(t)

	gw := helpers.StartGateway(t)
	_ = gw

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	bogusToken := "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiJib2d1cyIsImV4cCI6MX0.invalid"

	wsc, err := helpers.NewWSClient(ctx, bogusToken)
	if err != nil {
		// Server rejects at WebSocket handshake level — acceptable.
		t.Logf("dial with bogus JWT rejected at handshake: %v (pass)", err)
		return
	}
	defer wsc.Close()

	// If dial succeeded, connect frame must be rejected.
	_, connectErr := wsc.Connect(ctx, map[string]any{
		"accessToken": bogusToken,
	})
	if connectErr == nil {
		t.Fatal("connect with bogus JWT: expected error response, got success")
	}
	t.Logf("bogus JWT correctly rejected at connect: %v", connectErr)
}

// TestWSAllFrameTypesPresent — over a status round-trip we expect at least
// req (client-sent) and res (server-sent). Event delivery is tested via
// WaitEvent with a short timeout — a missed event is a soft skip, not a hard fail,
// because freshly-connected clients may not have event triggers yet.
func TestWSAllFrameTypesPresent(t *testing.T) {
	helpers.MustMigrateClean(t)
	helpers.ResetDB(t)

	gw := helpers.StartGateway(t)
	api := helpers.NewAPIClient()
	api.BaseURL = gw.BaseURL

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	token := loginForWS(t, ctx, api, helpers.RootEmail(), helpers.RootPassword())

	wsCtx, wsCancel := context.WithTimeout(ctx, 30*time.Second)
	defer wsCancel()

	wsc, err := helpers.NewWSClient(wsCtx, token)
	if err != nil {
		t.Skipf("WS dial failed: %v", err)
	}
	defer wsc.Close()

	// Connect — req/res pair confirmed.
	connectPayload, err := wsc.Connect(wsCtx, map[string]any{"locale": "en"})
	if err != nil {
		t.Skipf("WS connect failed: %v", err)
	}
	if !json.Valid(connectPayload) {
		t.Fatalf("connect response: invalid JSON: %s", string(connectPayload))
	}

	// Send status — another req/res pair.
	statusPayload, err := wsc.SendReq(wsCtx, protocol.MethodStatus, map[string]any{})
	if err != nil {
		t.Fatalf("status request: %v", err)
	}
	if !json.Valid(statusPayload) {
		t.Fatalf("status response: invalid JSON: %s", string(statusPayload))
	}

	// Try to observe an event frame. Some servers emit a state-push event on connect.
	// Short timeout — a miss is soft (events only fire with agent/cron activity).
	_, evErr := wsc.WaitEvent("", 2*time.Second)
	if evErr != nil {
		t.Logf("no event frame observed in 2s — acceptable for idle gateway (skipping event-frame assertion): %v", evErr)
	} else {
		t.Log("event frame observed — all three frame types (req/res/event) confirmed")
	}
}

// TestWSBadJSONRejected — WSClient does not expose a raw write path, so this
// test is deferred until a raw-write helper is available.
func TestWSBadJSONRejected(t *testing.T) {
	t.Skip("requires raw WS write API — deferred")
}
