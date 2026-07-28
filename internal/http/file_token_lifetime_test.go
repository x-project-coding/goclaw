package http

import (
	"strconv"
	"strings"
	"testing"
	"time"
)

// ─── 42bucks fork patch: signed file URLs must outlive response caches ────
//
// x-api caches signed history responses for up to an hour, and signed URLs
// sit in browser tabs long after delivery. A 5-minute token TTL plus a
// random per-process signing key meant every cache read >5min after write —
// and every read after a gateway restart — served dead URLs (broken images).

func tokenExpiry(t *testing.T, signed string) time.Time {
	t.Helper()
	i := strings.LastIndex(signed, "ft=")
	if i < 0 {
		t.Fatalf("no ft= token in %q", signed)
	}
	parts := strings.SplitN(signed[i+3:], ".", 2)
	if len(parts) != 2 {
		t.Fatalf("malformed token in %q", signed)
	}
	unix, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		t.Fatalf("bad expiry in %q: %v", signed, err)
	}
	return time.Unix(unix, 0)
}

func TestFileTokenTTL_OutlivesHistoryCaches(t *testing.T) {
	signed := SignMediaPath("/app/workspace/tenants/x/ws/u/.uploads/shot.jpg", "test-secret")
	exp := tokenExpiry(t, signed)
	minLife := 2 * time.Hour // must comfortably exceed x-api's 1h history cache
	if time.Until(exp) < minLife {
		t.Errorf("signed token lives %s, want >= %s (x-api caches signed history responses for 1h)",
			time.Until(exp).Round(time.Minute), minLife)
	}
}

func TestDeriveFileSigningKey_StableForSameSecret(t *testing.T) {
	a := deriveFileSigningKey("gateway-master-token")
	b := deriveFileSigningKey("gateway-master-token")
	if a == "" || a != b {
		t.Errorf("derivation not deterministic: %q vs %q", a, b)
	}
	if c := deriveFileSigningKey("another-token"); c == a {
		t.Error("different secrets must derive different keys")
	}
	if a == "gateway-master-token" {
		t.Error("derived key must not be the raw secret")
	}
}

func TestFileSigningKey_UsesStableSecretFromEnv(t *testing.T) {
	// FileSigningKey is a sync.Once singleton; compute what it WOULD derive
	// and verify a token signed with the derived key verifies — i.e. a
	// restarted process with the same env accepts tokens minted before the
	// restart. (The singleton itself is exercised via resolveFileSigningKey.)
	t.Setenv("GOCLAW_GATEWAY_TOKEN", "stable-master-token")
	k1 := resolveFileSigningKey()
	k2 := resolveFileSigningKey()
	if k1 == "" || k1 != k2 {
		t.Fatalf("resolveFileSigningKey not stable across calls: %q vs %q", k1, k2)
	}
	tok := SignFileToken("/v1/files/a.jpg", k1, time.Minute)
	if !VerifyFileToken(tok, "/v1/files/a.jpg", k2) {
		t.Error("token signed pre-'restart' must verify with the re-derived key")
	}
}

func TestFileSigningKey_RandomFallbackWithoutSecret(t *testing.T) {
	t.Setenv("GOCLAW_FILE_SIGNING_SECRET", "")
	t.Setenv("GOCLAW_GATEWAY_TOKEN", "")
	k1 := resolveFileSigningKey()
	k2 := resolveFileSigningKey()
	if k1 == "" || k2 == "" {
		t.Fatal("fallback key must not be empty")
	}
	if k1 == k2 {
		t.Error("without a stable secret, keys must stay random per resolution (upstream behavior)")
	}
}
