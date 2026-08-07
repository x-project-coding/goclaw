// Package agent — output claim guard for unbacked completion claims.
//
// OutputClaimGuard scans a candidate final assistant reply for
// completion-claim language ("built and tested", "deployed", "verified",
// "ran it end to end", ...). The pipeline's ObserveStage consults it ONLY
// when the run made zero tool calls (sync and async), i.e. when the claimed
// work cannot have happened this turn.
// Action is configurable via gateway.output_claim_guard_action:
//   - "log":   record the trigger only (telemetry)
//   - "warn":  force ONE corrective retry via a [System] note (default)
//   - "block": replace the reply with a neutral holding message
//   - "off":   disable scanning entirely
package agent

import "regexp"

// OutputClaimGuard scans candidate final replies for completion-claim language.
type OutputClaimGuard struct {
	patterns []guardPattern
}

// NewOutputClaimGuard creates an OutputClaimGuard with the default set of
// completion-claim detection patterns.
func NewOutputClaimGuard() *OutputClaimGuard {
	return &OutputClaimGuard{
		patterns: defaultOutputClaimPatterns(),
	}
}

// Scan checks a candidate final reply against all completion-claim patterns.
// Returns the names of matched patterns (empty slice = no matches).
func (g *OutputClaimGuard) Scan(content string) []string {
	names, _ := g.ScanWithPhrase(content)
	return names
}

// ScanWithPhrase is Scan plus the first matched phrase, for use in the
// corrective [System] note ("Your last draft claimed %q ...").
func (g *OutputClaimGuard) ScanWithPhrase(content string) ([]string, string) {
	if content == "" {
		return nil, ""
	}
	var matches []string
	phrase := ""
	for _, gp := range g.patterns {
		if m := gp.pattern.FindString(content); m != "" {
			matches = append(matches, gp.name)
			if phrase == "" {
				phrase = m
			}
		}
	}
	return matches, phrase
}

// HasPatterns returns true if the guard has any patterns configured.
func (g *OutputClaimGuard) HasPatterns() bool {
	return len(g.patterns) > 0
}

// PatternNames returns the names of all configured patterns.
func (g *OutputClaimGuard) PatternNames() []string {
	names := make([]string, len(g.patterns))
	for i, gp := range g.patterns {
		names[i] = gp.name
	}
	return names
}

// defaultOutputClaimPatterns returns the built-in set of completion-claim
// detection patterns. These target first-person / passive assertions that
// concrete work already happened (built, deployed, tested, verified). They
// deliberately avoid future ("I'll build"), conditional ("I can build") and
// negative ("I can't deploy") phrasing so ordinary zero-tool-call replies —
// plain Q&A, refusals, plans — never match.
func defaultOutputClaimPatterns() []guardPattern {
	return []guardPattern{
		{
			// "I built", "we've deployed", "I have already tested", "I just set it up".
			// Deliberately excludes created/finished/completed: those describe
			// inline text deliverables ("I have created a draft for you below"),
			// which are the normal, correct, zero-tool-call path — they carry no
			// build/test/deploy semantics on their own.
			name:    "first_person_completed",
			pattern: regexp.MustCompile(`(?i)\b(?:i|we)(?:'ve)?(?:\s+(?:have|just|already|now))*\s+(?:built|implemented|deployed|published|shipped|launched|fixed|tested|verified|confirmed|checked|set\s+(?:it|that|this|everything)\s+up)\b`),
		},
		{
			// "is built and working", "has been deployed", "it's live", "are tested"
			name:    "passive_completed",
			pattern: regexp.MustCompile(`(?i)\b(?:is|are|it's|has\s+been|have\s+been)\s+(?:now\s+)?(?:fully\s+|successfully\s+|headlessly\s+)?(?:built|deployed|published|shipped|launched|live|implemented|tested|verified|up\s+and\s+running)\b`),
		},
		{
			// "I ran through the whole thing", "we just ran it"
			name:    "ran_it_myself",
			pattern: regexp.MustCompile(`(?i)\b(?:i|we)\s+(?:just\s+)?ran\s+(?:through\s+)?(?:it|this|that|the\s+whole|everything|all\b|every)`),
		},
		{
			// "I ran the tests", "we ran the build"
			name:    "ran_tests",
			pattern: regexp.MustCompile(`(?i)\b(?:i|we)\s+(?:just\s+)?ran\s+(?:the\s+)?(?:tests?\b|test\s+suite|build\b)`),
		},
		{
			// "all tests passed", "tests are passing", "build is green"
			name:    "tests_passing",
			pattern: regexp.MustCompile(`(?i)\b(?:all\s+)?tests?\s+(?:are\s+)?(?:pass(?:ed|ing)|green)\b|\bbuild\s+(?:is\s+)?(?:passing|green|succeeded)\b`),
		},
		{
			// "live now", "now live" — first-person / passive deploy claims are
			// covered above; advisory "can be deployed to production" must not match.
			name:    "live_now",
			pattern: regexp.MustCompile(`(?i)\blive\s+now\b|\bnow\s+live\b`),
		},
		{
			// "tested it end to end", "ran through it end-to-end", "verified end to end"
			name:    "end_to_end_verified",
			pattern: regexp.MustCompile(`(?i)\b(?:tested|ran(?:\s+through)?|verified|confirmed|checked)\b[^.!?\n]{0,50}\bend[\s-]to[\s-]end\b`),
		},
	}
}
