//go:build integration

package integration

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
	"github.com/nextlevelbuilder/goclaw/internal/tracing"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// TestCollector_UpdateRetry_SucceedsAfterTwoFailures verifies that when the
// store fails twice, the retry mechanism recovers and persists the update.
func TestCollector_UpdateRetry_SucceedsAfterTwoFailures(t *testing.T) {
	db := testDB(t)
	seedTenantAgent(t, db)

	// Create a real store and collector
	tracingStore := pg.NewPGTracingStore(db)
	collector := tracing.NewCollector(tracingStore)
	defer collector.Stop()

	trace := &store.TraceData{
		ID:        uuid.New(),
		Status:    "running",
		StartTime: time.Now().UTC(),
	}
	_, err := db.ExecContext(context.Background(),
		`INSERT INTO traces (id, status, start_time) VALUES ($1, $2, $3)`,
		trace.ID, trace.Status, trace.StartTime)
	if err != nil {
		t.Fatalf("insert trace failed: %v", err)
	}

	// Wrap the store with a flaky version that fails 2 times
	flaky := newFlakyTracingStore(tracingStore, 2)
	collector2 := tracing.NewCollector(flaky)
	collector2.Start()
	t.Cleanup(collector2.Stop)

	// Call SetTraceStatus — should fail twice inline, then succeed on retry
	setCtx, setCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer setCancel()

	collector2.SetTraceStatus(setCtx, trace.ID, "completed")

	// Give the retry worker time to process
	time.Sleep(100 * time.Millisecond)

	// Query the trace status from the real store (using fresh context + connection)
	var status string
	queryErr := db.QueryRowContext(
		context.Background(),
		`SELECT status FROM traces WHERE id = $1`,
		trace.ID,
	).Scan(&status)
	if queryErr != nil {
		t.Fatalf("query trace status failed: %v", queryErr)
	}

	if status != "completed" {
		t.Errorf("expected status='completed', got '%s'", status)
	}
	t.Logf("Trace status updated to: %s", status)
}

// TestCollector_UpdateRetry_EnqueuesOnAllFailures verifies that when all inline
// retries fail, the update is enqueued in the retry queue (RetryQueueLen >= 1).
func TestCollector_UpdateRetry_EnqueuesOnAllFailures(t *testing.T) {
	db := testDB(t)
	seedTenantAgent(t, db)

	// Create a trace in the DB so UpdateTrace has a valid target.
	traceID := uuid.New()
	_, err := db.ExecContext(context.Background(),
		`INSERT INTO traces (id, status, start_time) VALUES ($1, $2, $3)`,
		traceID, "running", time.Now().UTC())
	if err != nil {
		t.Fatalf("insert trace failed: %v", err)
	}

	// Wrap store so every UpdateTrace call fails — more than inline retry count (3).
	realStore := pg.NewPGTracingStore(db)
	flaky := newFlakyTracingStore(realStore, 1000)

	// Do NOT call collector.Start() — we want the retry worker idle so items stay in queue.
	collector := tracing.NewCollector(flaky)

	setCtx := context.Background()
	collector.SetTraceStatus(setCtx, traceID, "completed")

	// After all 4 inline attempts fail, the update must be in the retry queue.
	queueLen := collector.RetryQueueLen()
	if queueLen < 1 {
		t.Errorf("expected RetryQueueLen >= 1 after all inline retries failed, got %d", queueLen)
	}
	t.Logf("RetryQueueLen after all-fail: %d", queueLen)
}

// TestCollector_BroadcastsStatusEvent verifies that when a trace status is
// updated successfully, the StatusBroadcaster callback is invoked with the
// correct payload.
func TestCollector_BroadcastsStatusEvent(t *testing.T) {
	db := testDB(t)
	seedTenantAgent(t, db)

	tracingStore := pg.NewPGTracingStore(db)
	collector := tracing.NewCollector(tracingStore)

	msgBus := bus.New()

	broadcastedPayloads := make(chan tracing.TraceStatusPayload, 10)
	broadcaster := func(payload tracing.TraceStatusPayload) {
		broadcastedPayloads <- payload
		msgBus.Broadcast(bus.Event{
			Name:    protocol.EventTraceStatusChanged,
			Payload: payload,
		})
	}
	collector.SetStatusBroadcaster(broadcaster)

	trace := &store.TraceData{
		ID:        uuid.New(),
		Status:    "running",
		StartTime: time.Now().UTC(),
	}
	_, err := db.ExecContext(context.Background(),
		`INSERT INTO traces (id, status, start_time) VALUES ($1, $2, $3)`,
		trace.ID, trace.Status, trace.StartTime)
	if err != nil {
		t.Fatalf("insert trace failed: %v", err)
	}

	finishCtx, finishCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer finishCancel()
	collector.FinishTrace(finishCtx, trace.ID, "completed", "", "test output")

	time.Sleep(50 * time.Millisecond)

	select {
	case p := <-broadcastedPayloads:
		if p.TraceID != trace.ID.String() {
			t.Errorf("expected traceId=%s, got %s", trace.ID.String(), p.TraceID)
		}
		if p.Status != "completed" {
			t.Errorf("expected status='completed', got '%s'", p.Status)
		}
		if p.EndedAt == nil {
			t.Error("expected EndedAt to be non-nil")
		}
	case <-time.After(1 * time.Second):
		t.Error("broadcaster was not called within timeout")
	}
}

// TestCollector_StaleRecovery_MarksOldRunningAsError inserts a "running" trace
// older than 2 minutes and verifies that the stale recovery loop marks it as "error".
func TestCollector_StaleRecovery_MarksOldRunningAsError(t *testing.T) {
	db := testDB(t)
	seedTenantAgent(t, db)

	tracingStore := pg.NewPGTracingStore(db)
	collector := tracing.NewCollector(tracingStore)
	collector.Start()
	t.Cleanup(collector.Stop)

	// Insert a "running" trace with start_time 11 minutes ago (exceeds 10min staleThreshold)
	traceID := uuid.New()
	oldStartTime := time.Now().UTC().Add(-11 * time.Minute)
	_, err := db.ExecContext(
		context.Background(),
		`INSERT INTO traces (id, status, start_time)
		 VALUES ($1, $2, $3)`,
		traceID, "running", oldStartTime,
	)
	if err != nil {
		t.Fatalf("insert stale trace failed: %v", err)
	}
	t.Logf("Inserted stale trace %s with start_time %v", traceID.String(), oldStartTime)

	// Manually trigger stale recovery (wait up to 35s or use helper if available)
	// The stale recovery loop runs every 30s by default, so we trigger it directly
	collector.RecoverStaleNow()

	// Give it a moment to complete
	time.Sleep(500 * time.Millisecond)

	// Query the trace status
	var status string
	err = db.QueryRowContext(
		context.Background(),
		`SELECT status FROM traces WHERE id = $1`,
		traceID,
	).Scan(&status)
	if err != nil && err != sql.ErrNoRows {
		t.Fatalf("query trace status failed: %v", err)
	}
	if err == sql.ErrNoRows {
		t.Errorf("trace was deleted instead of marked as error")
		return
	}

	if status != "error" {
		t.Errorf("expected status='error' after stale recovery, got '%s'", status)
	}
	t.Logf("Stale trace marked as error: %s", status)
}
