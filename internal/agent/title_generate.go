package agent

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
	usagecaps "github.com/nextlevelbuilder/goclaw/internal/usage/caps"
)

const titleGenerateTimeout = 120 * time.Second

// Output-token budget for the title call. Reasoning-capable cheap-tier models
// can spend part of the budget on thinking traces even with thinkingLevel=off,
// so leave generous room for the actual title to survive. If it still comes
// back empty, GenerateTitle falls back to the first message (never unnamed).
const titleMaxTokens = 512

const titleSystemPrompt = `Generate a short title (max 15 words) for this conversation based on the user's message. Reply with only the title, no quotes or punctuation wrapping.`

// GenerateTitle uses a lightweight LLM call to create a short conversation title
// from the user's first message. Returns empty string on error.
func GenerateTitle(ctx context.Context, provider providers.Provider, model, userMessage string) string {
	return GenerateTitleWithUsageCaps(ctx, nil, provider, model, userMessage)
}

func GenerateTitleWithUsageCaps(ctx context.Context, usageCaps *usagecaps.Service, provider providers.Provider, model, userMessage string) string {
	ctx, cancel := context.WithTimeout(ctx, titleGenerateTimeout)
	defer cancel()

	req := providers.ChatRequest{
		Messages: []providers.Message{
			{Role: "system", Content: titleSystemPrompt},
			{Role: "user", Content: userMessage},
		},
		Model: model,
		Options: map[string]any{
			// Larger budget: thinking-capable models (Gemini 2.5/3, GPT-5, glm)
			// can consume output tokens on reasoning traces even with thinking
			// disabled, which otherwise leaves no room for the title itself.
			providers.OptMaxTokens:   titleMaxTokens,
			providers.OptTemperature: 0.3,
			// Disable extended thinking for title generation — it's a trivial task
			// that doesn't benefit from reasoning and defaults (esp. Gemini's "high")
			// otherwise eat the entire max_tokens budget, truncating the title to 1 word.
			providers.OptThinkingLevel: "off",
			// Route via x-router "fast" mode so this trivial call ignores the
			// agent's pinned model (e.g. gpt-5.4) and uses the cheap tier. With no
			// mode, x-router forwards the pinned model verbatim to OpenRouter.
			providers.OptRoutingMode: "background",
		},
	}
	resp, err := usageCaps.Chat(ctx, provider, req, usagecaps.ChatOptions{
		ModelID:         model,
		Purpose:         "session-title",
		MaxOutputTokens: titleMaxTokens,
	})
	if err != nil {
		slog.Warn("title generation failed; using first-message fallback", "error", err)
		return fallbackTitle(userMessage)
	}

	title := strings.TrimSpace(resp.Content)
	// Strip surrounding quotes if present.
	title = strings.Trim(title, "\"'`")
	title = strings.TrimSpace(title)

	if title == "" {
		// Reasoning-capable cheap-tier models (e.g. glm) can spend the entire
		// output budget on thinking traces despite thinkingLevel=off and return
		// no title text. Never leave the session unnamed — fall back to a short
		// form of the user's first message.
		slog.Warn("title generation returned empty content; using first-message fallback")
		return fallbackTitle(userMessage)
	}

	if runes := []rune(title); len(runes) > 100 {
		title = string(runes[:100])
	}
	return title
}

// fallbackTitle derives a short, deterministic conversation title from the
// user's first message so a chat is never left unnamed when the LLM title call
// fails or returns empty content.
func fallbackTitle(userMessage string) string {
	s := strings.TrimSpace(userMessage)
	// Drop the hidden-user "[System]" marker prefix if present.
	s = strings.TrimSpace(strings.TrimPrefix(s, "[System]"))
	// Use the first non-empty line so multi-line messages give a tidy title.
	for _, line := range strings.Split(s, "\n") {
		if t := strings.TrimSpace(line); t != "" {
			s = t
			break
		}
	}
	if runes := []rune(s); len(runes) > 60 {
		s = strings.TrimSpace(string(runes[:60]))
	}
	if s == "" {
		return "New chat"
	}
	return s
}
