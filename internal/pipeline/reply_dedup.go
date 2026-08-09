package pipeline

import (
	"strings"
	"unicode"
)

// Duplicate-continuation dedup (42bucks fork patch). When a turn carries both
// reply text and a tool call, the model's internalized tool-calling convention
// sometimes restates the same text on the continuation turn that follows the
// tool result, so users see the reply twice. The helpers here normalize reply
// text so the loop (ThinkStage, makeCallLLM) and the gateway final-message net
// (cmd/gateway_consumer_normal.go) can recognize such repeats — an exact-match
// compare misses formatting-only variants (observed in production: the same
// sentence re-sent with markdown bold).

// NormalizeReplyText prepares assistant reply text for duplicate comparison:
// markdown emphasis markers ('*' and '_') are stripped, whitespace runs
// collapse to a single space, and the result is trimmed.
func NormalizeReplyText(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	pendingSpace := false
	for _, r := range s {
		switch {
		case r == '*' || r == '_':
			continue
		case unicode.IsSpace(r):
			pendingSpace = b.Len() > 0
		default:
			if pendingSpace {
				b.WriteByte(' ')
				pendingSpace = false
			}
			b.WriteRune(r)
		}
	}
	return b.String()
}

// IsDuplicateReplyText reports whether two reply texts are the same message
// after normalization. Empty text never counts as a duplicate.
func IsDuplicateReplyText(a, b string) bool {
	na := NormalizeReplyText(a)
	return na != "" && na == NormalizeReplyText(b)
}

// StreamDedup gates live chunk emission for a continuation turn against the
// text of the previous content-bearing turn in the same run. Chunks are held
// while the accumulated text is still a normalized prefix of that text and
// released the moment it diverges; a turn that ends as a full repeat is never
// emitted. An empty previous text disables the gate (pure pass-through).
// Not safe for concurrent use — one instance per LLM call.
type StreamDedup struct {
	prevNorm    string
	held        strings.Builder
	passthrough bool
}

// NewStreamDedup creates a gate comparing against prevTurnText.
func NewStreamDedup(prevTurnText string) *StreamDedup {
	norm := NormalizeReplyText(prevTurnText)
	return &StreamDedup{prevNorm: norm, passthrough: norm == ""}
}

// Push accepts the next streamed content delta and returns the text to emit
// now — empty while content is being held for comparison.
func (d *StreamDedup) Push(delta string) string {
	if d.passthrough {
		return delta
	}
	d.held.WriteString(delta)
	if strings.HasPrefix(d.prevNorm, NormalizeReplyText(d.held.String())) {
		return ""
	}
	d.passthrough = true
	out := d.held.String()
	d.held.Reset()
	return out
}

// Finish ends the turn. flush carries held text that must still be emitted
// (the turn ended as a shorter prefix, not a full repeat); suppressed is true
// when the whole turn matched the previous turn's text and was dropped.
// Subsequent Push calls pass through (relevant for guarded retry calls that
// reuse the same chunk callback).
func (d *StreamDedup) Finish() (flush string, suppressed bool) {
	if d.passthrough {
		return "", false
	}
	d.passthrough = true
	held := d.held.String()
	d.held.Reset()
	if held == "" {
		return "", false
	}
	if NormalizeReplyText(held) == d.prevNorm {
		return "", true
	}
	return held, false
}
