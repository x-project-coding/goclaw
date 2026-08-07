package agent

import (
	"strings"
	"testing"
)

func TestOutputClaimGuard_MatchesCompletionClaims(t *testing.T) {
	t.Parallel()
	guard := NewOutputClaimGuard()

	cases := []struct {
		name    string
		content string
	}{
		// Verbatim shapes from the monitored incidents.
		{"roman_built_and_working", "Your onboarding quiz for xsor.ai is built and working great."},
		{"roman_ran_through", "I ran through the whole thing myself end to end."},
		{"samantha_headless_tested", "The Mario game has been built and headlessly tested."},
		{"first_person_built", "I built the dashboard you asked for."},
		{"first_person_contraction", "We've deployed the new landing page."},
		{"first_person_adverbs", "I have already tested the flow and it looks good."},
		{"first_person_setup", "I just set it up on the server."},
		{"passive_deployed", "The API has been deployed."},
		{"passive_live", "The site is live at https://example.com."},
		{"ran_tests", "I ran the tests and everything looks good."},
		{"tests_passed", "All tests passed."},
		{"tests_passing", "The tests are passing on main."},
		{"live_now", "Your store is up, live now."},
		{"deployed_to_production", "The fix has been deployed to production."},
		{"end_to_end", "I verified the checkout flow end to end."},
		{"verified_claim", "I verified the numbers against the sheet."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			names, phrase := guard.ScanWithPhrase(tc.content)
			if len(names) == 0 {
				t.Fatalf("Scan(%q) = no matches, want at least one", tc.content)
			}
			if phrase == "" {
				t.Fatalf("ScanWithPhrase(%q) returned empty phrase", tc.content)
			}
			if !strings.Contains(strings.ToLower(tc.content), strings.ToLower(phrase)) {
				t.Fatalf("phrase %q not found in content %q", phrase, tc.content)
			}
		})
	}
}

func TestOutputClaimGuard_IgnoresOrdinaryReplies(t *testing.T) {
	t.Parallel()
	guard := NewOutputClaimGuard()

	cases := []struct {
		name    string
		content string
	}{
		{"empty", ""},
		{"plain_answer", "The capital of France is Paris."},
		{"future_tense", "I'll build the quiz now and let you know when it's ready."},
		{"offer", "I can build that for you. Want me to start?"},
		{"question", "Should I deploy this to staging or production?"},
		{"refusal", "No, I can't build that without access to your repository."},
		{"blocker", "I cannot deploy right now because the job service is unavailable."},
		{"advice_test", "Make sure the tests pass before merging."},
		{"advice_e2e", "You should test it end to end before launch."},
		{"plan", "Here is the plan: first we will create the schema, then deploy it."},
		{"conditional", "Once the build succeeds it can be deployed to production by CI."},
		{"describing_user_stack", "Your site runs on WordPress with a custom theme."},
		// Inline text deliverables: writing the artifact directly in chat is
		// the normal, correct, zero-tool-call path for copy/ops personas.
		{"inline_draft", "I have created a draft for you below: Dear customer, thank you for reaching out."},
		{"inline_outline", "I finished the outline - here it is."},
		{"inline_completed", "I've completed the summary you asked for - it's below."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if names := guard.Scan(tc.content); len(names) > 0 {
				t.Fatalf("Scan(%q) = %v, want no matches", tc.content, names)
			}
		})
	}
}

func TestOutputClaimGuard_PatternNames(t *testing.T) {
	t.Parallel()
	guard := NewOutputClaimGuard()
	if !guard.HasPatterns() {
		t.Fatal("HasPatterns() = false, want true")
	}
	names := guard.PatternNames()
	if len(names) == 0 {
		t.Fatal("PatternNames() is empty")
	}
	seen := map[string]bool{}
	for _, n := range names {
		if n == "" {
			t.Fatal("empty pattern name")
		}
		if seen[n] {
			t.Fatalf("duplicate pattern name %q", n)
		}
		seen[n] = true
	}
}
