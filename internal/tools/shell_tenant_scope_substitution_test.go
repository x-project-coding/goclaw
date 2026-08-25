package tools

import (
	"path/filepath"
	"strings"
	"testing"
)

// Command substitution used to break the tenant-path guard's tokenizer.
//
// splitExecCommandSegments cut at shell operators (;|&<>) but NOT at the
// `$(`/`)` that delimit a command substitution, so a pipeline inside `$(…)`
// produced a segment carrying an unbalanced parenthesis:
//
//	counts=$(echo "$result" | jq -r '.data.countsByResult // empty')
//	                                 └─ segment: jq -r '.data…// empty')
//
// go-shellwords rejects that segment, parseExecCommandWords falls back to
// strings.Fields, and the fallback shatters the single-quoted jq filter into
// bare words — one of which is `//`. filepath.IsAbs("//") is true, it cleans to
// "/", and crossesTenantBoundary blocks the filesystem root. The result was a
// hard denial of an ordinary read-only command:
//
//	exec: path "//" is blocked by cross-tenant isolation policy …
//
// Observed in production 2026-08-24 (agent Jordan, XSOR Outreach): three
// identical denials in a row, which then fed the tool-loop guard's
// identical-result counter.
//
// The fix segments command substitutions as well, so every inner command is
// parsed as its own balanced segment. That keeps each inner path visible to the
// guard — the security cases below pin that it did not become a bypass.
func TestEnforceTenantPathScope_CommandSubstitution(t *testing.T) {
	fx := newTenantScopeFixture(t)
	tool := &ExecTool{workspace: fx.root}
	ctx := tenantCtx(fx.ownWS)

	cases := []struct {
		name       string
		command    string
		wantDenied bool
	}{
		{
			// The exact production command.
			name:       "jq alternative operator inside command substitution allowed",
			command:    `counts=$(echo "$result" | jq -r '.data.countsByResult // empty')`,
			wantDenied: false,
		},
		{
			name:       "jq alternative operator in a bare pipeline allowed",
			command:    `echo "$result" | jq -r '.data.countsByResult // empty'`,
			wantDenied: false,
		},
		{
			name:       "nested command substitution with jq alternative allowed",
			command:    `x=$(printf '%s' "$(cat sub/nested.txt)" | jq -r '.a // .b // empty')`,
			wantDenied: false,
		},
		{
			name:       "backtick substitution with jq alternative allowed",
			command:    "x=`echo \"$r\" | jq -r '.a // empty'`",
			wantDenied: false,
		},
		{
			// SECURITY: the inner command of a substitution must still be
			// checked. Splitting on $( … ) exists to make the inner words
			// parseable, never to hide them.
			name:       "sibling tenant path inside command substitution rejected",
			command:    `data=$(cat ` + filepath.Join(fx.otherWS, "secret.txt") + `)`,
			wantDenied: true,
		},
		{
			name:       "sibling tenant path inside a substituted pipeline rejected",
			command:    `data=$(cat ` + filepath.Join(fx.otherWS, "secret.txt") + ` | head -1)`,
			wantDenied: true,
		},
		{
			name:       "recursive walk of workspace root inside substitution rejected",
			command:    `hits=$(find ` + fx.root + ` -name secret.txt | wc -l)`,
			wantDenied: true,
		},
		{
			name:       "backtick substitution reaching a sibling tenant rejected",
			command:    "data=`cat " + filepath.Join(fx.otherWS, "secret.txt") + "`",
			wantDenied: true,
		},
		{
			// A literal `//` naming the filesystem root is still the root, and a
			// recursive tool rooted there still descends into every tenant.
			name:       "find at doubled filesystem root still rejected",
			command:    "find // -name secret.txt",
			wantDenied: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tool.enforceTenantPathScope(ctx, tc.command, fx.ownWS)
			if tc.wantDenied && got == nil {
				t.Fatalf("expected command to be denied, but it was allowed: %s", tc.command)
			}
			if !tc.wantDenied && got != nil {
				t.Fatalf("expected command to be allowed, but it was denied: %s\n  -> %s", tc.command, got.ForLLM)
			}
			if tc.wantDenied && got != nil && !strings.Contains(got.ForLLM, "cross-tenant isolation policy") {
				t.Fatalf("denial did not name the policy: %s", got.ForLLM)
			}
		})
	}
}
