package providers

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"strings"
)

// Textual tool-call rescue.
//
// Some OpenAI-compatible upstreams (observed live 2026-07-07: z-ai/glm-5.2
// served through OpenRouter behind the xrouter `gpt-5.4` alias) sometimes emit
// a tool call as TEXT inside message content instead of the structured
// tool_calls array:
//
//	<tool_call>call_skill_service
//	<arg_key>input</arg_key><arg_value>{"query":"..."}</arg_value>
//	<arg_key>operation</arg_key><arg_value>manage-view.set</arg_value>
//	</tool_call>
//
// Without rescue the raw markup leaks into the user-visible reply and the tool
// never executes. The opening <tool_call> tag is sometimes already consumed
// upstream (observed in prod payloads), so the parser keys on the arg pairs +
// the closing tag, with the tool name being the identifier immediately before
// the first <arg_key>.

// textToolCallRe matches one textual tool-call block. RE2 \w is ASCII-only, so
// the name capture cannot swallow preceding non-ASCII prose; requiring at
// least one <arg_key>/<arg_value> pair plus the closing </tool_call> keeps
// false positives out of ordinary text.
var textToolCallRe = regexp.MustCompile(
	`(?s)(?:<tool_call>)?\s*([A-Za-z_][A-Za-z0-9_.-]*)\s*` +
		`((?:<arg_key>.*?</arg_key>\s*<arg_value>.*?</arg_value>\s*)+)` +
		`</tool_call>`)

// textArgPairRe extracts the key/value pairs inside a matched block.
var textArgPairRe = regexp.MustCompile(`(?s)<arg_key>(.*?)</arg_key>\s*<arg_value>(.*?)</arg_value>`)

// rescueTextToolCalls extracts textual tool-call blocks from content. It
// returns the content with the blocks removed and one ToolCall per block.
// Content without any complete block is returned unchanged with nil calls.
func rescueTextToolCalls(content string) (string, []ToolCall) {
	if !strings.Contains(content, "</tool_call>") || !strings.Contains(content, "<arg_key>") {
		return content, nil
	}

	var calls []ToolCall
	cleaned := textToolCallRe.ReplaceAllStringFunc(content, func(block string) string {
		m := textToolCallRe.FindStringSubmatch(block)
		if m == nil {
			return block
		}
		args := map[string]any{}
		for _, pair := range textArgPairRe.FindAllStringSubmatch(m[2], -1) {
			key := strings.TrimSpace(pair[1])
			raw := strings.TrimSpace(pair[2])
			// Values arrive JSON-encoded for objects/arrays/numbers/bools and
			// bare for plain strings — try JSON first, fall back to the raw text.
			var v any
			if err := json.Unmarshal([]byte(raw), &v); err == nil {
				args[key] = v
			} else {
				args[key] = raw
			}
		}
		calls = append(calls, ToolCall{
			ID:        "textcall_" + randomCallSuffix(),
			Name:      strings.TrimSpace(m[1]),
			Arguments: args,
		})
		return ""
	})

	if len(calls) == 0 {
		return content, nil
	}
	return strings.TrimSpace(cleaned), calls
}

// randomCallSuffix returns a short unique id fragment for rescued calls, so
// tool_call ids echoed back in history never collide across turns.
func randomCallSuffix() string {
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "fallback"
	}
	return hex.EncodeToString(b[:])
}
