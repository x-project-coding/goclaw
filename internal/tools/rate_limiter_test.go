package tools

import (
	"strings"
	"testing"
	"time"
)

func TestNewToolRateLimiter_Zero(t *testing.T) {
	rl := NewToolRateLimiter(0)
	if rl != nil {
		t.Errorf("expected nil for maxPerHour=0, got %v", rl)
	}
}

func TestNewToolRateLimiter_Negative(t *testing.T) {
	rl := NewToolRateLimiter(-5)
	if rl != nil {
		t.Errorf("expected nil for maxPerHour=-5, got %v", rl)
	}
}

func TestToolRateLimiter_AllowUnderLimit(t *testing.T) {
	rl := NewToolRateLimiter(5)
	for i := range 5 {
		if err := rl.Allow("user1"); err != nil {
			t.Errorf("action %d should be allowed: %v", i, err)
		}
	}
}

func TestToolRateLimiter_BlockOverLimit(t *testing.T) {
	rl := NewToolRateLimiter(3)

	for i := range 3 {
		if err := rl.Allow("user1"); err != nil {
			t.Fatalf("action %d should be allowed: %v", i, err)
		}
	}

	err := rl.Allow("user1")
	if err == nil {
		t.Error("4th action should be blocked")
	}
}

func TestToolRateLimiter_SeparateKeys(t *testing.T) {
	rl := NewToolRateLimiter(2)

	// Fill user1
	rl.Allow("user1")
	rl.Allow("user1")

	// user1 is blocked
	if err := rl.Allow("user1"); err == nil {
		t.Error("user1 should be blocked")
	}

	// user2 is independent
	if err := rl.Allow("user2"); err != nil {
		t.Errorf("user2 should be allowed: %v", err)
	}
}

func TestToolRateLimiter_WindowExpiry(t *testing.T) {
	rl := &ToolRateLimiter{
		windows:  make(map[string][]time.Time),
		maxPerHr: 2,
		window:   100 * time.Millisecond, // short window for testing
	}

	// Fill the window
	rl.Allow("key1")
	rl.Allow("key1")

	if err := rl.Allow("key1"); err == nil {
		t.Error("should be blocked at limit")
	}

	// Wait for window to expire
	time.Sleep(150 * time.Millisecond)

	if err := rl.Allow("key1"); err != nil {
		t.Errorf("should be allowed after window expiry: %v", err)
	}
}

func TestToolRateLimiter_Cleanup(t *testing.T) {
	rl := &ToolRateLimiter{
		windows:  make(map[string][]time.Time),
		maxPerHr: 10,
		window:   50 * time.Millisecond,
	}

	rl.Allow("key1")
	rl.Allow("key2")

	time.Sleep(100 * time.Millisecond)
	rl.Cleanup()

	rl.mu.Lock()
	count := len(rl.windows)
	rl.mu.Unlock()

	if count != 0 {
		t.Errorf("cleanup should remove all expired entries, got %d", count)
	}
}

func TestToolRateLimiter_CleanupPartial(t *testing.T) {
	rl := &ToolRateLimiter{
		windows:  make(map[string][]time.Time),
		maxPerHr: 10,
		window:   200 * time.Millisecond,
	}

	rl.Allow("key1") // will expire
	time.Sleep(100 * time.Millisecond)
	rl.Allow("key1") // still fresh

	// Only the first entry should be pruned, not the whole key
	time.Sleep(150 * time.Millisecond)
	rl.Cleanup()

	rl.mu.Lock()
	entries := len(rl.windows["key1"])
	rl.mu.Unlock()

	if entries != 1 {
		t.Errorf("expected 1 remaining entry, got %d", entries)
	}
}

func TestToolRateLimiter_NilIsPermissive(t *testing.T) {
	var rl *ToolRateLimiter
	if err := rl.Allow("user1"); err != nil {
		t.Errorf("nil limiter should allow everything: %v", err)
	}
	if rl.Max() != 0 {
		t.Errorf("nil limiter Max() = %d, want 0", rl.Max())
	}
	rl.SetMax(10) // must not panic
	rl.Cleanup()  // must not panic
}

func TestNewToolRateLimiterAlways_ZeroIsDisabledNotNil(t *testing.T) {
	rl := NewToolRateLimiterAlways(0)
	if rl == nil {
		t.Fatal("Always constructor must return a usable limiter even at 0")
	}
	for i := range 50 {
		if err := rl.Allow("user1"); err != nil {
			t.Fatalf("call %d should be allowed while disabled: %v", i, err)
		}
	}
}

// The point of the Always constructor: a limit configured as 0 at boot must be
// raisable at runtime. With the old nil-returning constructor this was impossible
// without restarting the gateway.
func TestToolRateLimiter_SetMax_EnablesAfterBoot(t *testing.T) {
	rl := NewToolRateLimiterAlways(0)
	for range 5 {
		rl.Allow("user1")
	}

	rl.SetMax(2)
	if got := rl.Max(); got != 2 {
		t.Fatalf("Max() = %d, want 2", got)
	}
	if err := rl.Allow("user1"); err != nil {
		t.Fatalf("first call after enabling should pass: %v", err)
	}
	rl.Allow("user1")
	if err := rl.Allow("user1"); err == nil {
		t.Error("third call should be blocked once the limit is 2")
	}
}

func TestToolRateLimiter_SetMax_RaisingUnblocks(t *testing.T) {
	rl := NewToolRateLimiterAlways(2)
	rl.Allow("user1")
	rl.Allow("user1")
	if err := rl.Allow("user1"); err == nil {
		t.Fatal("should be blocked at the original limit")
	}

	rl.SetMax(5)
	if err := rl.Allow("user1"); err != nil {
		t.Errorf("raising the limit should unblock immediately: %v", err)
	}
}

func TestToolRateLimiter_SetMax_ZeroDisables(t *testing.T) {
	rl := NewToolRateLimiterAlways(1)
	rl.Allow("user1")
	if err := rl.Allow("user1"); err == nil {
		t.Fatal("should be blocked at limit 1")
	}

	rl.SetMax(0)
	if err := rl.Allow("user1"); err != nil {
		t.Errorf("limit 0 should disable limiting: %v", err)
	}
}

func TestToolRateLimiter_ErrorCarriesRetryAfter(t *testing.T) {
	rl := NewToolRateLimiterAlways(1)
	rl.Allow("user1")

	err := rl.Allow("user1")
	if err == nil {
		t.Fatal("expected a rate limit error")
	}
	if !strings.Contains(err.Error(), "retry in ") {
		t.Errorf("error must tell the model how long to wait, got: %v", err)
	}
}

// A blocked caller that keeps retrying must not push its own window further out,
// or a hammering agent would never recover.
func TestToolRateLimiter_RejectionsDoNotExtendWindow(t *testing.T) {
	rl := NewToolRateLimiterAlways(2)
	rl.Allow("user1")
	rl.Allow("user1")

	for range 20 {
		if err := rl.Allow("user1"); err == nil {
			t.Fatal("expected these to be rejected")
		}
	}

	rl.mu.Lock()
	n := len(rl.windows["user1"])
	rl.mu.Unlock()
	if n != 2 {
		t.Errorf("window holds %d entries, want 2 — rejections were recorded", n)
	}
}

func TestToolRateLimiter_CleanupDropsIdleKeys(t *testing.T) {
	rl := NewToolRateLimiterAlways(5)
	rl.window = 10 * time.Millisecond
	rl.Allow("user1")
	rl.Allow("user2")

	if got := rl.TrackedKeys(); got != 2 {
		t.Fatalf("TrackedKeys() = %d, want 2", got)
	}

	time.Sleep(20 * time.Millisecond)
	rl.Cleanup()

	if got := rl.TrackedKeys(); got != 0 {
		t.Errorf("TrackedKeys() = %d after cleanup, want 0 — idle keys leak", got)
	}
}
