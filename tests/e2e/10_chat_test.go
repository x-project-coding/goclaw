//go:build e2e

// Package e2e_test exercises HTTP /v1/chat/completions (non-stream + stream)
// and WS chat.send. LLM real-call tests are gated by testing.Short() and
// env-var API keys. Agent model is addressed via "goclaw:<agent_key>" in
// the model field.
package e2e_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
	"github.com/nextlevelbuilder/goclaw/tests/e2e/helpers"
)

func mustOKChat(t *testing.T, label string, res *helpers.APIResponse, err error, want int) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: transport error: %v", label, err)
	}
	if res.Status != want {
		t.Fatalf("%s: status %d, want %d, body=%s", label, res.Status, want, string(res.Body))
	}
}

func mustJSONChat(t *testing.T, res *helpers.APIResponse, out any) {
	t.Helper()
	if err := res.JSON(out); err != nil {
		t.Fatalf("decode JSON: %v body=%s", err, string(res.Body))
	}
}

func loginForChat(t *testing.T, ctx context.Context, api *helpers.APIClient, email, pass string) string {
	t.Helper()
	res, err := api.POST(ctx, "/v1/auth/login", map[string]string{
		"email": email, "password": pass,
	})
	mustOKChat(t, "POST /v1/auth/login", res, err, http.StatusOK)
	var tok struct {
		AccessToken string `json:"access_token"`
	}
	mustJSONChat(t, res, &tok)
	if tok.AccessToken == "" {
		t.Fatalf("loginForChat %s: empty access_token", email)
	}
	return tok.AccessToken
}

// createAgentForChat seeds an agent via HTTP API and returns its agent_key.
// The agent is created with the given provider+model so real LLM calls route correctly.
func createAgentForChat(t *testing.T, ctx context.Context, api *helpers.APIClient, provider, model string) string {
	t.Helper()
	agentKey := "chat-" + helpers.RandHex8()
	res, err := api.POST(ctx, "/v1/agents", map[string]any{
		"agent_key":  agentKey,
		"agent_type": "open",
		"model":      model,
		"provider":   provider,
	})
	mustOKChat(t, "POST /v1/agents", res, err, http.StatusCreated)
	var ag struct{ AgentKey string `json:"agent_key"` }
	mustJSONChat(t, res, &ag)
	if ag.AgentKey == "" {
		t.Fatalf("createAgentForChat: empty agent_key")
	}
	return ag.AgentKey
}

// TestChatNonStream — POST /v1/chat/completions with stream:false via Bailian provider.
// Skipped in -short mode and when BAILIAN_API_KEY is missing.
func TestChatNonStream(t *testing.T) {
	if testing.Short() {
		t.Skip("LLM real-call skipped under -short")
	}
	helpers.MustLoadEnv()
	if helpers.BailianKey() == "" {
		t.Skip("BAILIAN_API_KEY missing")
	}

	helpers.MustMigrateClean(t)
	helpers.ResetDB(t)

	gw := helpers.StartGateway(t)
	api := helpers.NewAPIClient()
	api.BaseURL = gw.BaseURL

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	token := loginForChat(t, ctx, api, helpers.RootEmail(), helpers.RootPassword())
	api.SetToken(token)

	agentKey := createAgentForChat(t, ctx, api, "dashscope", "qwen-turbo")

	body := map[string]any{
		"model": fmt.Sprintf("goclaw:%s", agentKey),
		"messages": []map[string]string{
			{"role": "user", "content": "Reply with the single word: pong"},
		},
		"stream": false,
	}
	res, err := api.POST(ctx, "/v1/chat/completions", body)
	mustOKChat(t, "POST /v1/chat/completions (non-stream)", res, err, http.StatusOK)

	var resp struct {
		Choices []struct {
			Message struct{ Content string `json:"content"` } `json:"message"`
		} `json:"choices"`
		Usage struct {
			TotalTokens int `json:"total_tokens"`
		} `json:"usage"`
	}
	mustJSONChat(t, res, &resp)
	if resp.Usage.TotalTokens <= 0 {
		t.Fatalf("non-stream: total_tokens=%d, want >0 (body=%s)", resp.Usage.TotalTokens, string(res.Body))
	}
	if len(resp.Choices) == 0 {
		t.Fatalf("non-stream: empty choices (body=%s)", string(res.Body))
	}
}

// TestChatStreamHTTP — POST /v1/chat/completions with stream:true; collect SSE chunks.
// Skipped in -short mode and when BAILIAN_API_KEY is missing.
func TestChatStreamHTTP(t *testing.T) {
	if testing.Short() {
		t.Skip("LLM real-call skipped under -short")
	}
	helpers.MustLoadEnv()
	if helpers.BailianKey() == "" {
		t.Skip("BAILIAN_API_KEY missing")
	}

	helpers.MustMigrateClean(t)
	helpers.ResetDB(t)

	gw := helpers.StartGateway(t)
	api := helpers.NewAPIClient()
	api.BaseURL = gw.BaseURL

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	token := loginForChat(t, ctx, api, helpers.RootEmail(), helpers.RootPassword())
	api.SetToken(token)

	agentKey := createAgentForChat(t, ctx, api, "dashscope", "qwen-turbo")

	body := map[string]any{
		"model": fmt.Sprintf("goclaw:%s", agentKey),
		"messages": []map[string]string{
			{"role": "user", "content": "Reply with the single word: pong"},
		},
		"stream": true,
	}
	res, err := api.POST(ctx, "/v1/chat/completions", body)
	mustOKChat(t, "POST /v1/chat/completions (stream)", res, err, http.StatusOK)

	// Parse SSE chunks — look for at least one data: line with content.
	scanner := bufio.NewScanner(bytes.NewReader(res.Body))
	contentChunks := 0
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}
		var chunk struct {
			Choices []struct {
				Delta struct{ Content string `json:"content"` } `json:"delta"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}
		for _, ch := range chunk.Choices {
			if ch.Delta.Content != "" {
				contentChunks++
			}
		}
	}
	if contentChunks == 0 {
		t.Fatalf("stream: no content chunks received (body=%s)", string(res.Body))
	}
}

// TestChatViaWS — WS chat.send → wait for chat events then chat.completed.
// Skipped in -short mode and when BAILIAN_API_KEY is missing.
func TestChatViaWS(t *testing.T) {
	if testing.Short() {
		t.Skip("LLM real-call skipped under -short")
	}
	helpers.MustLoadEnv()
	if helpers.BailianKey() == "" {
		t.Skip("BAILIAN_API_KEY missing")
	}

	helpers.MustMigrateClean(t)
	helpers.ResetDB(t)

	gw := helpers.StartGateway(t)
	api := helpers.NewAPIClient()
	api.BaseURL = gw.BaseURL

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	token := loginForChat(t, ctx, api, helpers.RootEmail(), helpers.RootPassword())
	api.SetToken(token)

	agentKey := createAgentForChat(t, ctx, api, "dashscope", "qwen-turbo")

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

	sessionKey := "e2e-chat-ws-" + helpers.RandHex8()
	params, _ := json.Marshal(map[string]any{
		"agentKey":   agentKey,
		"sessionKey": sessionKey,
		"message":    "Reply with the single word: pong",
	})
	// SendReq for chat.send: response arrives after the full turn completes.
	payload, err := wsc.SendReq(wsCtx, protocol.MethodChatSend, json.RawMessage(params))
	if err != nil {
		t.Fatalf("chat.send: %v", err)
	}
	if !json.Valid(payload) {
		t.Fatalf("chat.send: invalid JSON response: %s", string(payload))
	}
}

// TestChatToolUseTurn — deferred; requires agent+tool setup beyond batch scope.
func TestChatToolUseTurn(t *testing.T) {
	t.Skip("TODO: tool-use loop requires agent+tool scaffolding — deferred")
}

// TestChatProviderOpenRouter — POST /v1/chat/completions via OpenRouter.
// Skipped in -short mode and when OPENROUTER_API_KEY is missing.
func TestChatProviderOpenRouter(t *testing.T) {
	if testing.Short() {
		t.Skip("LLM real-call skipped under -short")
	}
	helpers.MustLoadEnv()
	if helpers.OpenRouterKey() == "" {
		t.Skip("OPENROUTER_API_KEY missing")
	}

	helpers.MustMigrateClean(t)
	helpers.ResetDB(t)

	gw := helpers.StartGateway(t)
	api := helpers.NewAPIClient()
	api.BaseURL = gw.BaseURL

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	token := loginForChat(t, ctx, api, helpers.RootEmail(), helpers.RootPassword())
	api.SetToken(token)

	agentKey := createAgentForChat(t, ctx, api, "openrouter", "anthropic/claude-sonnet-4-5")

	body := map[string]any{
		"model": fmt.Sprintf("goclaw:%s", agentKey),
		"messages": []map[string]string{
			{"role": "user", "content": "Reply with the single word: pong"},
		},
		"stream": false,
	}
	res, err := api.POST(ctx, "/v1/chat/completions", body)
	mustOKChat(t, "POST /v1/chat/completions (openrouter)", res, err, http.StatusOK)

	var resp struct {
		Choices []struct {
			Message struct{ Content string `json:"content"` } `json:"message"`
		} `json:"choices"`
		Usage struct{ TotalTokens int `json:"total_tokens"` } `json:"usage"`
	}
	mustJSONChat(t, res, &resp)
	if resp.Usage.TotalTokens <= 0 {
		t.Fatalf("openrouter: total_tokens=%d, want >0", resp.Usage.TotalTokens)
	}
	if len(resp.Choices) == 0 {
		t.Fatalf("openrouter: empty choices")
	}
}

// TestProviderBailian — POST /v1/chat/completions via Alibaba DashScope (Bailian) provider.
// Skipped in -short mode and when BAILIAN_API_KEY is missing.
func TestProviderBailian(t *testing.T) {
	if testing.Short() {
		t.Skip("LLM real-call skipped under -short")
	}
	helpers.MustLoadEnv()
	if helpers.BailianKey() == "" {
		t.Skip("BAILIAN_API_KEY missing")
	}

	helpers.MustMigrateClean(t)
	helpers.ResetDB(t)

	gw := helpers.StartGateway(t)
	api := helpers.NewAPIClient()
	api.BaseURL = gw.BaseURL

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	token := loginForChat(t, ctx, api, helpers.RootEmail(), helpers.RootPassword())
	api.SetToken(token)

	// Use the env-configured model or fall back to qwen-turbo.
	model := os.Getenv("BAILIAN_DEFAULT_MODEL")
	if model == "" {
		model = "qwen-turbo"
	}

	agentKey := createAgentForChat(t, ctx, api, "dashscope", model)

	body := map[string]any{
		"model": fmt.Sprintf("goclaw:%s", agentKey),
		"messages": []map[string]string{
			{"role": "user", "content": "Reply with the single word: pong"},
		},
		"stream": false,
	}
	res, err := api.POST(ctx, "/v1/chat/completions", body)
	mustOKChat(t, "POST /v1/chat/completions (bailian)", res, err, http.StatusOK)

	var resp struct {
		Choices []struct {
			Message struct{ Content string `json:"content"` } `json:"message"`
		} `json:"choices"`
		Usage struct{ TotalTokens int `json:"total_tokens"` } `json:"usage"`
	}
	mustJSONChat(t, res, &resp)
	if resp.Usage.TotalTokens <= 0 {
		t.Fatalf("bailian: total_tokens=%d, want >0 (body=%s)", resp.Usage.TotalTokens, string(res.Body))
	}
	if len(resp.Choices) == 0 {
		t.Fatalf("bailian: empty choices (body=%s)", string(res.Body))
	}
	if resp.Choices[0].Message.Content == "" {
		t.Fatalf("bailian: empty message content (body=%s)", string(res.Body))
	}
}
