package providers

import (
	"strings"
	"testing"
)

// The exact payload stored in prod on 2026-07-07 (session agent:default-7,
// tenant 019f3bd1): the opening <tool_call> was already consumed upstream and
// the whole block landed in assistant content.
const prodLeakPayload = `call_skill_service<arg_key>input</arg_key><arg_value>{"hints": {"pills": [{"text": "Придумать акцию"}]}, "sessionKey": "cmraf8o8d001v01puwvhr8vdy"}</arg_value><arg_key>operation</arg_key><arg_value>manage-view.set</arg_value></tool_call>`

func TestRescueTextToolCalls_ProdPayloadNoOpener(t *testing.T) {
	cleaned, calls := rescueTextToolCalls(prodLeakPayload)
	if len(calls) != 1 {
		t.Fatalf("want 1 rescued call, got %d", len(calls))
	}
	c := calls[0]
	if c.Name != "call_skill_service" {
		t.Fatalf("name = %q", c.Name)
	}
	if op, _ := c.Arguments["operation"].(string); op != "manage-view.set" {
		t.Fatalf("operation = %v", c.Arguments["operation"])
	}
	input, ok := c.Arguments["input"].(map[string]any)
	if !ok {
		t.Fatalf("input not decoded as object: %T", c.Arguments["input"])
	}
	if input["sessionKey"] != "cmraf8o8d001v01puwvhr8vdy" {
		t.Fatalf("input.sessionKey = %v", input["sessionKey"])
	}
	if cleaned != "" {
		t.Fatalf("cleaned content should be empty, got %q", cleaned)
	}
	if c.ID == "" || !strings.HasPrefix(c.ID, "textcall_") {
		t.Fatalf("id = %q", c.ID)
	}
}

func TestRescueTextToolCalls_WithOpenerAndSurroundingProse(t *testing.T) {
	content := "Проверяю каталог подключений.\n<tool_call>web_fetch\n<arg_key>maxChars</arg_key><arg_value>8000</arg_value>\n<arg_key>url</arg_key><arg_value>https://api.mail.ru/docs/</arg_value>\n</tool_call>\nСейчас посмотрю."
	cleaned, calls := rescueTextToolCalls(content)
	if len(calls) != 1 {
		t.Fatalf("want 1 call, got %d", len(calls))
	}
	if calls[0].Name != "web_fetch" {
		t.Fatalf("name = %q", calls[0].Name)
	}
	// Numbers arrive JSON-decoded.
	if n, ok := calls[0].Arguments["maxChars"].(float64); !ok || n != 8000 {
		t.Fatalf("maxChars = %v (%T)", calls[0].Arguments["maxChars"], calls[0].Arguments["maxChars"])
	}
	if calls[0].Arguments["url"] != "https://api.mail.ru/docs/" {
		t.Fatalf("url = %v", calls[0].Arguments["url"])
	}
	if !strings.Contains(cleaned, "Проверяю каталог подключений.") || !strings.Contains(cleaned, "Сейчас посмотрю.") {
		t.Fatalf("prose lost: %q", cleaned)
	}
	if strings.Contains(cleaned, "arg_key") || strings.Contains(cleaned, "tool_call") {
		t.Fatalf("markup survived: %q", cleaned)
	}
}

func TestRescueTextToolCalls_MultipleBlocks(t *testing.T) {
	content := "a<arg_key>x</arg_key><arg_value>1</arg_value></tool_call>middle<tool_call>b<arg_key>y</arg_key><arg_value>true</arg_value></tool_call>"
	cleaned, calls := rescueTextToolCalls(content)
	if len(calls) != 2 {
		t.Fatalf("want 2 calls, got %d (cleaned=%q)", len(calls), cleaned)
	}
	if calls[0].Name != "a" || calls[1].Name != "b" {
		t.Fatalf("names = %q, %q", calls[0].Name, calls[1].Name)
	}
	if calls[0].ID == calls[1].ID {
		t.Fatal("rescued call ids must be unique")
	}
	if cleaned != "middle" {
		t.Fatalf("cleaned = %q", cleaned)
	}
}

func TestRescueTextToolCalls_NoOpOnNormalContent(t *testing.T) {
	for _, content := range []string{
		"",
		"An ordinary reply about <tools> and calls.",
		"mentions </tool_call> but has no arg pairs",
		"has <arg_key>k</arg_key> but no closing tag or value",
	} {
		cleaned, calls := rescueTextToolCalls(content)
		if len(calls) != 0 || cleaned != content {
			t.Fatalf("false positive on %q: calls=%d cleaned=%q", content, len(calls), cleaned)
		}
	}
}

func TestParseResponse_RescuesTextualToolCall(t *testing.T) {
	p := &OpenAIProvider{name: "xrouter"}
	resp := &openAIResponse{
		Choices: []openAIChoice{{
			Message: openAIMessage{
				Role:    "assistant",
				Content: prodLeakPayload,
			},
			FinishReason: "stop",
		}},
	}
	result := p.parseResponse(resp)
	if len(result.ToolCalls) != 1 {
		t.Fatalf("want 1 tool call, got %d", len(result.ToolCalls))
	}
	if result.ToolCalls[0].Name != "call_skill_service" {
		t.Fatalf("name = %q", result.ToolCalls[0].Name)
	}
	if result.Content != "" {
		t.Fatalf("content should be cleaned, got %q", result.Content)
	}
	if result.FinishReason != "tool_calls" {
		t.Fatalf("finish reason = %q", result.FinishReason)
	}
}
