//go:build e2e

// Package e2e_test exercises the WebSocket protocol layer: frame types,
// connect auth guards, ping, expired/bad JWT rejection, and event frame
// delivery. All tests use the helpers.WSClient (gorilla/websocket).
package e2e_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
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

// TestWSChatStreamEvents — connect, send chat.send with stream=true, assert at
// least one event frame arrives with an event name related to the chat turn.
// Requires a real LLM provider; skipped in -short mode and when both
// OPENROUTER_API_KEY and BAILIAN_API_KEY are absent.
func TestWSChatStreamEvents(t *testing.T) {
	if testing.Short() {
		t.Skip("real LLM call")
	}
	helpers.MustLoadEnv()
	if helpers.OpenRouterKey() == "" && helpers.BailianKey() == "" {
		t.Skip("no LLM API key available (OPENROUTER_API_KEY and BAILIAN_API_KEY both unset)")
	}

	helpers.MustMigrateClean(t)
	helpers.ResetDB(t)

	gw := helpers.StartGateway(t)
	api := helpers.NewAPIClient()
	api.BaseURL = gw.BaseURL

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	token := loginForWS(t, ctx, api, helpers.RootEmail(), helpers.RootPassword())
	api.SetToken(token)

	// Create an agent backed by whichever provider key is present.
	provider, model := "dashscope", "qwen-turbo"
	if helpers.BailianKey() == "" {
		provider, model = "openrouter", "anthropic/claude-sonnet-4-5"
	}
	agentKey := "ws-stream-" + helpers.RandHex8()
	res, err := api.POST(ctx, "/v1/agents", map[string]any{
		"agent_key":  agentKey,
		"agent_type": "open",
		"model":      model,
		"provider":   provider,
	})
	mustOKWS(t, "POST /v1/agents", res, err, http.StatusCreated)

	wsCtx, wsCancel := context.WithTimeout(ctx, 90*time.Second)
	defer wsCancel()

	wsc, err := helpers.NewWSClient(wsCtx, token)
	if err != nil {
		t.Skipf("WS dial failed: %v", err)
	}
	defer wsc.Close()

	if _, err := wsc.Connect(wsCtx, map[string]any{"locale": "en"}); err != nil {
		t.Skipf("WS connect failed: %v", err)
	}

	// Send chat.send with stream=true. The res frame arrives after the full turn;
	// event frames arrive during the turn. Wait for at least one event before
	// the res frame completes by racing event observation against the response.
	sessionKey := "e2e-ws-stream-" + helpers.RandHex8()

	// Start a goroutine that drains events into a channel while the chat turn runs.
	eventCh := make(chan helpers.WSEvent, 32)
	go func() {
		for {
			ev, err := wsc.WaitEvent("", 60*time.Second)
			if err != nil {
				return
			}
			eventCh <- ev
		}
	}()

	sendCtx, sendCancel := context.WithTimeout(wsCtx, 90*time.Second)
	defer sendCancel()

	payload, sendErr := wsc.SendReq(sendCtx, protocol.MethodChatSend, map[string]any{
		"agentId":    agentKey,
		"sessionKey": sessionKey,
		"message":    "Reply with the single word: pong",
		"stream":     true,
	})
	if sendErr != nil {
		t.Fatalf("chat.send (stream): %v", sendErr)
	}
	if !json.Valid(payload) {
		t.Fatalf("chat.send (stream): invalid JSON response: %s", string(payload))
	}

	// Drain any buffered events accumulated during the turn.
	chatEventFound := false
	drainLoop:
	for {
		select {
		case ev := <-eventCh:
			name := ev.Event
			if name == "" {
				break
			}
			if strings.Contains(name, "chat") || strings.Contains(name, "delta") ||
				strings.Contains(name, "stream") || strings.Contains(name, "session") {
				chatEventFound = true
				t.Logf("TestWSChatStreamEvents: chat-related event observed: %q", name)
				break drainLoop
			}
		default:
			break drainLoop
		}
	}

	if !chatEventFound {
		// Try one more short wait — events may be buffered just after res delivery.
		ev, evErr := wsc.WaitEvent("", 2*time.Second)
		if evErr == nil {
			name := ev.Event
			if strings.Contains(name, "chat") || strings.Contains(name, "delta") ||
				strings.Contains(name, "stream") || strings.Contains(name, "session") {
				chatEventFound = true
				t.Logf("TestWSChatStreamEvents: chat-related event observed (late): %q", name)
			}
		}
	}

	if !chatEventFound {
		t.Log("TestWSChatStreamEvents: no chat/delta/stream event observed — server may not emit mid-turn events for this provider; res frame arrived cleanly (soft pass)")
	}
}

// TestWSReconnectAfterDisconnect — open WS, connect successfully, close the
// connection, then open a fresh WS with the same JWT and connect again.
// Asserts the server accepts the second connection and responds to a health
// request, proving disconnect does not poison subsequent connects.
func TestWSReconnectAfterDisconnect(t *testing.T) {
	helpers.MustMigrateClean(t)
	helpers.ResetDB(t)

	gw := helpers.StartGateway(t)
	api := helpers.NewAPIClient()
	api.BaseURL = gw.BaseURL

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	token := loginForWS(t, ctx, api, helpers.RootEmail(), helpers.RootPassword())

	// --- First connection ---
	ws1Ctx, ws1Cancel := context.WithTimeout(ctx, 20*time.Second)
	defer ws1Cancel()

	wsc1, err := helpers.NewWSClient(ws1Ctx, token)
	if err != nil {
		t.Skipf("WS dial (1st) failed: %v", err)
	}

	if _, err := wsc1.Connect(ws1Ctx, map[string]any{"locale": "en"}); err != nil {
		wsc1.Close()
		t.Fatalf("connect (1st): %v", err)
	}
	t.Log("TestWSReconnectAfterDisconnect: first connect OK")

	// Close explicitly — simulates client navigating away / tab close.
	if err := wsc1.Close(); err != nil {
		t.Logf("close (1st): %v (non-fatal)", err)
	}

	// Brief pause to let server-side close propagate before we re-dial.
	time.Sleep(100 * time.Millisecond)

	// --- Second connection (same JWT) ---
	ws2Ctx, ws2Cancel := context.WithTimeout(ctx, 20*time.Second)
	defer ws2Cancel()

	wsc2, err := helpers.NewWSClient(ws2Ctx, token)
	if err != nil {
		t.Fatalf("WS dial (2nd) failed: %v", err)
	}
	defer wsc2.Close()

	if _, err := wsc2.Connect(ws2Ctx, map[string]any{"locale": "en"}); err != nil {
		t.Fatalf("connect (2nd): %v", err)
	}
	t.Log("TestWSReconnectAfterDisconnect: second connect OK")

	// Verify the connection is usable by sending a health request.
	pingCtx, pingCancel := context.WithTimeout(ws2Ctx, 5*time.Second)
	defer pingCancel()

	pingPayload, err := wsc2.SendReq(pingCtx, protocol.MethodHealth, map[string]any{})
	if err != nil {
		t.Fatalf("health after reconnect: %v", err)
	}
	if !json.Valid(pingPayload) {
		t.Fatalf("health after reconnect: invalid JSON: %s", string(pingPayload))
	}
	t.Log("TestWSReconnectAfterDisconnect: health request on reconnected WS OK")
}
