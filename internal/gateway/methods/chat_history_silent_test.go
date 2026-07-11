package methods

import (
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// TestFilterSilentReplies verifies that NO_REPLY sentinel assistant turns are
// stripped from user-facing history (the reload vector that leaked the literal
// token to the chat UI) while every other turn — including substantive replies
// and non-assistant roles — is preserved verbatim.
func TestFilterSilentReplies(t *testing.T) {
	tests := []struct {
		name     string
		in       []providers.Message
		wantLen  int
		wantLast string // content of the last surviving message ("" if none)
	}{
		{
			name: "bare NO_REPLY assistant final is dropped",
			in: []providers.Message{
				{Role: "user", Content: "[System] welcome"},
				{Role: "assistant", Content: "Let me check your setup."},
				{Role: "tool", Content: `{"data":[]}`},
				{Role: "assistant", Content: "NO_REPLY"},
			},
			wantLen:  3,
			wantLast: `{"data":[]}`,
		},
		{
			name: "decorated + trailing-reason NO_REPLY dropped",
			in: []providers.Message{
				{Role: "assistant", Content: "**NO_REPLY**"},
				{Role: "assistant", Content: "NO_REPLY: staying quiet, user is offline"},
				{Role: "assistant", Content: `"NO_REPLY"`},
			},
			wantLen:  0,
			wantLast: "",
		},
		{
			name: "substantive reply is preserved",
			in: []providers.Message{
				{Role: "assistant", Content: "Here is your report."},
			},
			wantLen:  1,
			wantLast: "Here is your report.",
		},
		{
			name: "NO_REPLYING (glued word) is NOT silent, preserved",
			in: []providers.Message{
				{Role: "assistant", Content: "NO_REPLYING is what I'm doing"},
			},
			wantLen:  1,
			wantLast: "NO_REPLYING is what I'm doing",
		},
		{
			name: "silent reply carrying media is preserved (deliverable not lost)",
			in: []providers.Message{
				{Role: "assistant", Content: "NO_REPLY", MediaRefs: []providers.MediaRef{{ID: "a.png"}}},
			},
			wantLen:  1,
			wantLast: "NO_REPLY",
		},
		{
			name: "a user turn that says NO_REPLY is NOT dropped (only assistant)",
			in: []providers.Message{
				{Role: "user", Content: "NO_REPLY"},
			},
			wantLen:  1,
			wantLast: "NO_REPLY",
		},
		{
			name:     "empty history stays empty",
			in:       []providers.Message{},
			wantLen:  0,
			wantLast: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Snapshot input length to assert the input slice is not mutated.
			origLen := len(tt.in)
			got := filterSilentReplies(tt.in)
			if len(got) != tt.wantLen {
				t.Fatalf("filterSilentReplies() len = %d, want %d (got=%+v)", len(got), tt.wantLen, got)
			}
			if len(tt.in) != origLen {
				t.Fatalf("filterSilentReplies mutated input length: %d != %d", len(tt.in), origLen)
			}
			if tt.wantLen > 0 && got[len(got)-1].Content != tt.wantLast {
				t.Fatalf("last surviving content = %q, want %q", got[len(got)-1].Content, tt.wantLast)
			}
		})
	}
}
