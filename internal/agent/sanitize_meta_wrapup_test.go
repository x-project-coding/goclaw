package agent

import (
	"strings"
	"testing"
)

// longReplyContaining builds a substantive answer (well over metaWrapupMaxRunes)
// that happens to embed one of the meta phrases.
func longReplyContaining(phrase string) string {
	return "Here is the migration plan you asked for. " +
		strings.Repeat("Back up the database, apply the schema change, verify row counts. ", 5) +
		"I can start whenever — " + phrase + "."
}

func TestIsMetaWrapupReply(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want bool
	}{
		// Confirmed production phrasings — monitor issues cep62x82kb7y3qnb2q046ngzg,
		// cbvi88r48fo6v3e2ngawxmw2y, cc6fk76drjcaeg9uo5x8e883i.
		{
			"unrelated technical content",
			"Looks like some unrelated technical content got mixed into our chat just now, not a request from you. What would you like me to help you build today?",
			true,
		},
		{"no new text", "(No new text — pills/template already cover the ask above.)", true},
		{"nothing new to add", "Nothing new to add here.", true},
		// Standalone family — meta only when the phrase is effectively the whole
		// reply, which is the shape confirmed in production.
		{"waiting on go-ahead", "Waiting on your go-ahead.", true},
		{"ready when you are", "Ready when you are, Henry.", true},
		{"ready when you are bare", "Ready when you are.", true},
		{"waiting on that bare", "Waiting on that!", true},
		// Case-insensitive.
		{"uppercase", "NO NEW TEXT.", true},
		{"mixed case", "Not A Request From You.", true},
		// Rune-based length gate: a short non-ASCII reply is still short.
		{"non-ascii short reply", strings.Repeat("привет ", 30) + "no new text", true},
		// NOT meta — ordinary short replies must survive untouched.
		{"short legitimate reply", "Done — pushed the fix.", false},
		{"short legitimate question", "Deployed to staging. Want me to run the smoke tests?", false},
		{"short legitimate answer", "Clause 2.1 already matches Шаблон 0.", false},
		{"unrelated wording", "The unrelated PR is still open.", false},
		{"empty", "", false},
		{"whitespace only", "   \n\t ", false},
		// Standalone phrases inside a reply that says something — these are
		// legitimate answers and must not be salvaged over.
		{"waiting phrase after an answer", "I've drafted the migration. Waiting on your go-ahead.", false},
		{"ready phrase after an answer", "Deployed. Ready when you are to run the smoke test.", false},
		{"ready phrase with an object", "Ready when you are to review the PR.", false},
		{"waiting on that with substance", "Waiting on that confirmation before I proceed.", false},
		// "unrelated content" alone is ordinary wording about the user's own input
		// (only the full "unrelated technical content" dismissal is confirmed).
		{"unrelated content in a question", "Your patch mixes in unrelated content — should I split it?", false},
		{"unrelated content statement", "That looks like unrelated content, not something you asked for.", false},
		// Length gate — the main false-positive guard.
		{"long reply containing phrase", longReplyContaining("ready when you are"), false},
		{"long reply containing dismissal phrase", longReplyContaining("no new text"), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := IsMetaWrapupReply(c.in); got != c.want {
				t.Errorf("IsMetaWrapupReply(%q) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}

// A NO_REPLY token is handled by IsSilentReply, not by the meta wrap-up path —
// the two classifiers must not overlap.
func TestIsMetaWrapupReply_DoesNotClaimSilentReplies(t *testing.T) {
	for _, in := range []string{"NO_REPLY", "NO_REPLY: user is away", "**NO_REPLY**"} {
		if IsMetaWrapupReply(in) {
			t.Errorf("IsMetaWrapupReply(%q) = true, want false (IsSilentReply owns this)", in)
		}
	}
}
