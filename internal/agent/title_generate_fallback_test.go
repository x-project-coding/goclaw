package agent

import (
	"strings"
	"testing"
)

// TestFallbackTitle pins the deterministic title used when the LLM title call
// fails or returns empty, so a chat is never left unnamed.
func TestFallbackTitle(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", "New chat"},
		{"whitespace only", "   \n  \t", "New chat"},
		{"simple", "Hello there", "Hello there"},
		{"strips system marker", "[System] Welcome to the workspace", "Welcome to the workspace"},
		{"first non-empty line", "First line\nsecond line", "First line"},
		{"skips leading blank lines", "  \n\n  Real content\nmore", "Real content"},
		{"trims", "   padded title   ", "padded title"},
		{"truncates to 60 runes", strings.Repeat("a", 80), strings.Repeat("a", 60)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := fallbackTitle(c.in); got != c.want {
				t.Errorf("fallbackTitle(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

// TestFallbackTitleNeverEmpty is the core guarantee: whatever the input, the
// fallback yields a non-empty label.
func TestFallbackTitleNeverEmpty(t *testing.T) {
	for _, in := range []string{"", " ", "\n\n", "[System]", "[System]   ", "x"} {
		if got := fallbackTitle(in); got == "" {
			t.Errorf("fallbackTitle(%q) returned empty; must never be empty", in)
		}
	}
}
