package methods

import (
	"encoding/json"
	"strings"
	"testing"
)

// 42bucks fork patch (chat-view-context): chat.send's viewContext must
// round-trip off the wire into chatSendParams.ViewContext, which handleSend
// seeds into RunRequest.ExtraSystemPrompt. Mirrors the modelOverride/routingMode
// wire fields (same struct, same json-tag convention).

func TestChatSendParamsUnmarshalsViewContext(t *testing.T) {
	raw := []byte(`{"message":"hi","sessionKey":"agent:jordan:ws:direct:u1","viewContext":"The user is talking to you from the \"Outreach\" app's chat bubble and is currently viewing the app page \"/campaigns\"."}`)
	var p chatSendParams
	if err := json.Unmarshal(raw, &p); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	want := `The user is talking to you from the "Outreach" app's chat bubble and is currently viewing the app page "/campaigns".`
	if p.ViewContext != want {
		t.Fatalf("ViewContext = %q, want %q", p.ViewContext, want)
	}
}

func TestChatSendParamsViewContextOmittedDefaultsEmpty(t *testing.T) {
	var p chatSendParams
	if err := json.Unmarshal([]byte(`{"message":"hi"}`), &p); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if p.ViewContext != "" {
		t.Fatalf("ViewContext = %q, want empty when absent", p.ViewContext)
	}
}

// viewContext carries `omitempty`, so an empty field must not appear on the wire
// (keeps the RPC payload identical to today for non-bubble sends).
func TestChatSendParamsViewContextOmitemptyOnMarshal(t *testing.T) {
	b, err := json.Marshal(chatSendParams{Message: "hi"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if got := string(b); strings.Contains(got, "viewContext") {
		t.Fatalf("empty ViewContext must be omitted, got %s", got)
	}
}

// mergeChatSendRequests keeps the latest params, so a debounced burst preserves
// the ViewContext x-api attached — the value handleSend reads into the run.
func TestMergeChatSendRequestsPreservesViewContext(t *testing.T) {
	items := []chatSendRequest{
		{params: chatSendParams{Message: "first"}},
		{params: chatSendParams{Message: "second", ViewContext: "ctx-2"}},
	}
	if got := mergeChatSendRequests(items).ViewContext; got != "ctx-2" {
		t.Fatalf("merged ViewContext = %q, want %q", got, "ctx-2")
	}
}
