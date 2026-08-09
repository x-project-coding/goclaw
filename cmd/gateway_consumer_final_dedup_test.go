package cmd

import "testing"

// finalDuplicatesInterim guards the final-message publish against repeating an
// interim reply the user already received (42bucks fork patch —
// duplicate-continuation dedup). The compare is normalized, so
// formatting-only variants of the same text count as duplicates.
func TestFinalDuplicatesInterim(t *testing.T) {
	t.Parallel()
	const reply = "Hey! I'm Roman. What would you like to build today?"
	cases := []struct {
		name             string
		interimDelivered int
		lastInterim      string
		final            string
		mediaCount       int
		want             bool
	}{
		// Regression: the pre-existing exact-match suppression must keep firing.
		{"exact match suppressed", 1, reply, reply, 0, true},
		// New: normalized compare catches the production markdown-bold variant.
		{"markdown bold variant suppressed", 1, reply, "**Hey! I'm Roman.** What would you like to build today?", 0, true},
		{"whitespace variant suppressed", 2, reply, "Hey! I'm Roman.\n\nWhat would you like to build today?", 0, true},
		{"no interim delivered", 0, "", reply, 0, false},
		{"different final text", 1, reply, "Here is the summary you asked for.", 0, false},
		{"final with media never suppressed", 1, reply, reply, 1, false},
		{"empty final never suppressed", 1, reply, "", 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := finalDuplicatesInterim(tc.interimDelivered, tc.lastInterim, tc.final, tc.mediaCount)
			if got != tc.want {
				t.Errorf("finalDuplicatesInterim(%d, %q, %q, %d) = %v, want %v",
					tc.interimDelivered, tc.lastInterim, tc.final, tc.mediaCount, got, tc.want)
			}
		})
	}
}
