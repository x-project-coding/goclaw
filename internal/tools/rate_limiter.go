package tools

import (
	"fmt"
	"math"
	"sync"
	"time"
)

// ToolRateLimiter implements a sliding window rate limiter for tool executions.
// Tracks actions per key (typically agent:userID) within a configurable window.
type ToolRateLimiter struct {
	mu       sync.Mutex
	windows  map[string][]time.Time
	maxPerHr int
	window   time.Duration
}

// NewToolRateLimiter creates a rate limiter with the given max actions per hour.
// Pass 0 to disable rate limiting.
//
// Prefer NewToolRateLimiterAlways for long-lived processes: a nil limiter cannot
// be re-enabled later, so a limit configured as 0 at boot can never be raised at
// runtime without a restart.
func NewToolRateLimiter(maxPerHour int) *ToolRateLimiter {
	if maxPerHour <= 0 {
		return nil
	}
	return NewToolRateLimiterAlways(maxPerHour)
}

// NewToolRateLimiterAlways creates a limiter that is always installed, even when
// the limit is 0 (disabled). Keeping the object in place is what makes the limit
// tunable at runtime via SetMax — see the gateway's system_configs subscriber.
func NewToolRateLimiterAlways(maxPerHour int) *ToolRateLimiter {
	return &ToolRateLimiter{
		windows:  make(map[string][]time.Time),
		maxPerHr: maxPerHour,
		window:   time.Hour,
	}
}

// SetMax updates the limit in place. n <= 0 disables limiting without discarding
// the limiter, so it can be re-enabled later. Recorded history is kept: lowering
// the limit takes effect against the calls already in the window.
func (rl *ToolRateLimiter) SetMax(n int) {
	if rl == nil {
		return
	}
	rl.mu.Lock()
	defer rl.mu.Unlock()
	rl.maxPerHr = n
}

// Max returns the current limit (0 or less means disabled).
func (rl *ToolRateLimiter) Max() int {
	if rl == nil {
		return 0
	}
	rl.mu.Lock()
	defer rl.mu.Unlock()
	return rl.maxPerHr
}

// Allow checks if a tool execution is allowed for the given key.
// Returns nil if allowed, or an error describing the rate limit.
//
// Rejected calls are deliberately not recorded — a blocked caller that keeps
// retrying must not push its own window further out.
func (rl *ToolRateLimiter) Allow(key string) error {
	if rl == nil {
		return nil
	}

	rl.mu.Lock()
	defer rl.mu.Unlock()

	if rl.maxPerHr <= 0 {
		return nil
	}

	now := time.Now()
	cutoff := now.Add(-rl.window)

	// Prune expired entries
	entries := rl.windows[key]
	start := 0
	for start < len(entries) && entries[start].Before(cutoff) {
		start++
	}
	entries = entries[start:]

	if len(entries) >= rl.maxPerHr {
		// The oldest call in the window is the next slot to free. Telling the
		// model how long to wait is what stops it hammering a closed door.
		retryIn := time.Until(entries[0].Add(rl.window))
		if retryIn < time.Second {
			retryIn = time.Second
		}
		rl.windows[key] = entries
		return fmt.Errorf(
			"tool rate limit exceeded: %d actions/hour for key %s — retry in %ds",
			rl.maxPerHr, key, int(math.Ceil(retryIn.Seconds())))
	}

	// Record this action
	rl.windows[key] = append(entries, now)
	return nil
}

// Cleanup removes stale entries older than the window. Call periodically to prevent memory growth.
func (rl *ToolRateLimiter) Cleanup() {
	if rl == nil {
		return
	}

	rl.mu.Lock()
	defer rl.mu.Unlock()

	cutoff := time.Now().Add(-rl.window)
	for key, entries := range rl.windows {
		start := 0
		for start < len(entries) && entries[start].Before(cutoff) {
			start++
		}
		if start == len(entries) {
			delete(rl.windows, key)
		} else {
			rl.windows[key] = entries[start:]
		}
	}
}

// TrackedKeys reports how many keys the limiter is holding history for.
// Used by the cleanup loop's logging and by tests.
func (rl *ToolRateLimiter) TrackedKeys() int {
	if rl == nil {
		return 0
	}
	rl.mu.Lock()
	defer rl.mu.Unlock()
	return len(rl.windows)
}
