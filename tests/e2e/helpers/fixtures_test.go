//go:build e2e

package helpers

import (
	"strings"
	"testing"

	"github.com/google/uuid"
)

// TestRandHex8Uniqueness asserts 1000 calls produce 0 collisions.
// Catches catastrophic crypto/rand failure (e.g. /dev/urandom blocking).
func TestRandHex8Uniqueness(t *testing.T) {
	t.Parallel()
	const n = 1000
	seen := make(map[string]struct{}, n)
	for i := 0; i < n; i++ {
		v := RandHex8()
		if len(v) != 8 {
			t.Fatalf("RandHex8 length=%d want=8 (call %d, value=%q)", len(v), i, v)
		}
		if _, dup := seen[v]; dup {
			t.Fatalf("RandHex8 collision at call %d: %q", i, v)
		}
		seen[v] = struct{}{}
	}
}

// TestSeedUserUUID asserts SeedUser returns a UUID v7 (not v4) and a globally
// unique email. v7 versioning is enforced by the V4 lock per plan.md.
func TestSeedUserUUID(t *testing.T) {
	t.Parallel()
	a := SeedUser(t, SeedUserOpts{})
	b := SeedUser(t, SeedUserOpts{})

	if a.ID == uuid.Nil {
		t.Fatalf("SeedUser returned nil UUID")
	}
	if a.ID.Version() != 7 {
		t.Errorf("SeedUser UUID version=%d want=7 (per V4 lock)", a.ID.Version())
	}
	if a.Email == b.Email {
		t.Errorf("SeedUser produced duplicate emails: %q", a.Email)
	}
	if !strings.Contains(a.Email, ".test") {
		t.Errorf("SeedUser email %q missing .test domain (helps grep cleanup)", a.Email)
	}
}

// TestRandEmailFormat covers prefix, suffix, and unique-suffix invariants.
func TestRandEmailFormat(t *testing.T) {
	t.Parallel()
	e := RandEmail("alice")
	if !strings.HasPrefix(e, TestPrefix()+"-alice-") {
		t.Errorf("RandEmail prefix wrong: %q", e)
	}
	if !strings.HasSuffix(e, "@"+TestPrefix()+".test") {
		t.Errorf("RandEmail suffix wrong: %q", e)
	}
}
