package skillcatalog

import "testing"

func TestHintHasField(t *testing.T) {
	cases := []struct {
		hint, field string
		want        bool
	}{
		{"sessionKey:string, hints:{...}", "sessionKey", true},
		{"fromSessionKey:string, agentId?:string", "fromSessionKey", true},
		{"fromSessionKey:string, agentId?:string", "sessionKey", false}, // no bare sessionKey
		{"query:string", "sessionKey", false},
		{"a?:string, sessionKey?:string", "sessionKey", true},
		{"", "sessionKey", false},
	}
	for _, c := range cases {
		if got := HintHasField(c.hint, c.field); got != c.want {
			t.Errorf("HintHasField(%q, %q) = %v, want %v", c.hint, c.field, got, c.want)
		}
	}
}
