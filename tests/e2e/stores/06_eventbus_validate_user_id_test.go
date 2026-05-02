//go:build e2e

// PR-05D (R2): eventbus publish-time observer warns on non-UUID UserID drift
// without blocking dispatch. Mirrors validateAgentID safety net.
package stores_test

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/eventbus"
)

// captureSlogE2E swaps the default slog logger for one writing to a buffer
// so the e2e test can assert on warning output without touching internal APIs.
func captureSlogE2E(t *testing.T) (*bytes.Buffer, func()) {
	t.Helper()
	var buf bytes.Buffer
	handler := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	prev := slog.Default()
	slog.SetDefault(slog.New(handler))
	return &buf, func() { slog.SetDefault(prev) }
}

func newE2ETestBus(t *testing.T) eventbus.DomainEventBus {
	t.Helper()
	return eventbus.NewDomainEventBus(eventbus.Config{
		QueueSize:     100,
		WorkerCount:   2,
		RetryAttempts: 1,
		RetryDelay:    5 * time.Millisecond,
		DedupTTL:      time.Minute,
	})
}

// TestValidateUserID exercises the publish-time observer for UserID drift via
// the public DomainEventBus API. Three cases: non-UUID warns; valid UUID does
// not warn; non-UUID still dispatches (observability-only, never blocks).
func TestValidateUserID(t *testing.T) {
	t.Run("non_uuid_warns", func(t *testing.T) {
		buf, restore := captureSlogE2E(t)
		defer restore()

		bus := newE2ETestBus(t)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		bus.Start(ctx)
		defer func() { _ = bus.Drain(500 * time.Millisecond) }()

		var received atomic.Int32
		bus.Subscribe(eventbus.EventRunCompleted, func(_ context.Context, _ eventbus.DomainEvent) error {
			received.Add(1)
			return nil
		})

		bus.Publish(eventbus.DomainEvent{
			Type:     eventbus.EventRunCompleted,
			SourceID: "e2e-uid-bad-1",
			UserID:   "alice@example.com",
		})

		deadline := time.Now().Add(300 * time.Millisecond)
		for received.Load() == 0 && time.Now().Before(deadline) {
			time.Sleep(2 * time.Millisecond)
		}

		out := buf.String()
		if !strings.Contains(out, "eventbus.non_uuid_user_id") {
			t.Errorf("non-UUID UserID should emit warning, got log:\n%s", out)
		}
		if !strings.Contains(out, "non_uuid_user_id=") {
			t.Errorf("warning should use distinct field name `non_uuid_user_id=`, got log:\n%s", out)
		}
		if received.Load() != 1 {
			t.Errorf("non-UUID UserID must NOT block dispatch, got received=%d", received.Load())
		}
	})

	t.Run("valid_uuid_no_warn", func(t *testing.T) {
		buf, restore := captureSlogE2E(t)
		defer restore()

		bus := newE2ETestBus(t)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		bus.Start(ctx)
		defer func() { _ = bus.Drain(500 * time.Millisecond) }()

		bus.Publish(eventbus.DomainEvent{
			Type:     eventbus.EventRunCompleted,
			SourceID: "e2e-uid-good-1",
			UserID:   uuid.New().String(),
		})

		// Give worker enough time to dispatch + dedup.
		time.Sleep(50 * time.Millisecond)

		if strings.Contains(buf.String(), "non_uuid_user_id") {
			t.Errorf("valid UUID UserID must not emit warning, got log:\n%s", buf.String())
		}
	})

	t.Run("empty_no_warn", func(t *testing.T) {
		buf, restore := captureSlogE2E(t)
		defer restore()

		bus := newE2ETestBus(t)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		bus.Start(ctx)
		defer func() { _ = bus.Drain(500 * time.Millisecond) }()

		bus.Publish(eventbus.DomainEvent{
			Type:     eventbus.EventRunCompleted,
			SourceID: "e2e-uid-empty-1",
			UserID:   "",
		})

		time.Sleep(50 * time.Millisecond)

		if strings.Contains(buf.String(), "non_uuid_user_id") {
			t.Errorf("empty UserID is legitimate (anonymous/system event) and must not warn, got log:\n%s", buf.String())
		}
	})
}
