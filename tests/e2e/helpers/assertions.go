//go:build e2e

package helpers

import (
	"encoding/json"
	"reflect"
	"testing"
)

// AssertStatus fatals when resp.Status != want.
// Includes body snippet (first 256 bytes) in failure message for debugging.
func AssertStatus(t *testing.T, resp *APIResponse, want int) {
	t.Helper()
	if resp == nil {
		t.Fatalf("e2e: AssertStatus: response is nil")
	}
	if resp.Status != want {
		body := string(resp.Body)
		if len(body) > 256 {
			body = body[:256] + "...(truncated)"
		}
		t.Fatalf("e2e: status=%d want=%d body=%q", resp.Status, want, body)
	}
}

// AssertJSONEq fatals if want vs got JSON values differ structurally.
// Both inputs are normalized through encoding/json so ordering / numeric formatting
// differences don't cause false negatives.
func AssertJSONEq(t *testing.T, want, got any) {
	t.Helper()
	wantNorm, err := normalizeJSON(want)
	if err != nil {
		t.Fatalf("e2e: AssertJSONEq normalize want: %v", err)
	}
	gotNorm, err := normalizeJSON(got)
	if err != nil {
		t.Fatalf("e2e: AssertJSONEq normalize got: %v", err)
	}
	if !reflect.DeepEqual(wantNorm, gotNorm) {
		t.Fatalf("e2e: JSON mismatch\n  want: %s\n  got:  %s", mustEncode(wantNorm), mustEncode(gotNorm))
	}
}

// AssertEventType fatals if the event name doesn't match.
func AssertEventType(t *testing.T, ev WSEvent, want string) {
	t.Helper()
	if ev.Event != want {
		t.Fatalf("e2e: event=%q want=%q", ev.Event, want)
	}
}

// normalizeJSON marshals→unmarshals via map[string]any so the result is
// directly comparable with reflect.DeepEqual regardless of input concrete type.
func normalizeJSON(v any) (any, error) {
	if raw, ok := v.(json.RawMessage); ok {
		var out any
		if err := json.Unmarshal(raw, &out); err != nil {
			return nil, err
		}
		return out, nil
	}
	if s, ok := v.(string); ok {
		var out any
		if err := json.Unmarshal([]byte(s), &out); err != nil {
			return nil, err
		}
		return out, nil
	}
	buf, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var out any
	if err := json.Unmarshal(buf, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func mustEncode(v any) string {
	buf, err := json.Marshal(v)
	if err != nil {
		return "<encode error: " + err.Error() + ">"
	}
	return string(buf)
}
