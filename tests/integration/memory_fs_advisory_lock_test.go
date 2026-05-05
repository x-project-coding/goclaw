//go:build integration

package integration

import (
	"context"
	"sync"
	"testing"
	"time"
)

// TestFSWriterAdvisoryLockSerializes verifies that pg_advisory_xact_lock
// correctly serializes concurrent writers operating on the same file_path.
//
// Layer 2 of the 4-layer race defense: two separate database transactions
// competing for the same advisory lock on hashtext(file_path) must serialize
// — the second TX blocks until the first commits or rolls back.
//
// This test uses raw SQL (two separate *sql.Tx) to simulate what the Phase 04
// FSWriter does internally, without requiring the FSWriter impl to exist yet.
// The advisory lock key uses hashtext(path) — the same function Phase 04 will
// use — so this test validates the exact locking primitive.
func TestFSWriterAdvisoryLockSerializes(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	// Use a per-test unique path so parallel test runs don't contend.
	lockPath := "shared/" + t.Name() + "/advisory-lock-test.md"

	// tx1 acquires the advisory lock and holds it.
	tx1, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin tx1: %v", err)
	}
	defer tx1.Rollback()

	if _, err := tx1.ExecContext(ctx,
		"SELECT pg_advisory_xact_lock(hashtext($1))", lockPath); err != nil {
		t.Fatalf("tx1 acquire advisory lock: %v", err)
	}

	// tx2 tries the same lock in a goroutine — it must block while tx1 holds it.
	tx2Started := make(chan struct{})
	tx2Done := make(chan error, 1)
	var tx2BlockDuration time.Duration
	var mu sync.Mutex

	go func() {
		tx2, err := db.BeginTx(ctx, nil)
		if err != nil {
			tx2Done <- err
			return
		}
		defer tx2.Rollback()

		close(tx2Started)
		start := time.Now()

		// This call blocks until tx1 releases the lock.
		_, err = tx2.ExecContext(ctx,
			"SELECT pg_advisory_xact_lock(hashtext($1))", lockPath)
		mu.Lock()
		tx2BlockDuration = time.Since(start)
		mu.Unlock()

		tx2Done <- err
	}()

	// Wait until tx2 has started (and is blocked on the advisory lock).
	select {
	case <-tx2Started:
	case <-time.After(2 * time.Second):
		t.Fatal("tx2 did not start within 2s")
	}

	// Hold tx1 long enough that tx2's blocking is observable.
	holdDuration := 300 * time.Millisecond
	time.Sleep(holdDuration)

	// tx1 commits — this releases the advisory lock, unblocking tx2.
	if err := tx1.Commit(); err != nil {
		t.Fatalf("tx1 commit: %v", err)
	}

	// tx2 must unblock promptly after tx1 commits.
	select {
	case err := <-tx2Done:
		if err != nil {
			t.Fatalf("tx2 advisory lock acquisition failed: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("tx2 did not unblock within 3s after tx1 committed")
	}

	// tx2 must have blocked for at least the hold duration minus a small margin.
	// This proves the lock actually serialized the two transactions.
	mu.Lock()
	blocked := tx2BlockDuration
	mu.Unlock()

	minExpected := holdDuration - 50*time.Millisecond
	if blocked < minExpected {
		t.Errorf("tx2 unblocked too quickly: blocked=%v, want>=%v — lock may not have serialized", blocked, minExpected)
	}
	t.Logf("tx2 blocked for %v (tx1 held lock for %v) — serialization confirmed", blocked, holdDuration)
}

// TestFSWriterAdvisoryLockPerPathIsolation verifies that advisory locks on
// different file_paths do not block each other — only same-path writes serialize.
//
// This validates the hashtext(path) key design: two distinct paths with
// distinct hashtext() values must acquire their locks concurrently.
func TestFSWriterAdvisoryLockPerPathIsolation(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	pathA := "shared/" + t.Name() + "/path-a.md"
	pathB := "shared/" + t.Name() + "/path-b.md"

	tx1, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin tx1: %v", err)
	}
	defer tx1.Rollback()

	// tx1 holds lock on pathA.
	if _, err := tx1.ExecContext(ctx,
		"SELECT pg_advisory_xact_lock(hashtext($1))", pathA); err != nil {
		t.Fatalf("tx1 acquire lock on pathA: %v", err)
	}

	// tx2 acquires lock on pathB — must NOT block on tx1's pathA lock.
	acquiredWithin := make(chan bool, 1)
	go func() {
		tx2, err := db.BeginTx(ctx, nil)
		if err != nil {
			acquiredWithin <- false
			return
		}
		defer tx2.Rollback()

		ctx2, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
		defer cancel()

		_, err = tx2.ExecContext(ctx2,
			"SELECT pg_advisory_xact_lock(hashtext($1))", pathB)
		acquiredWithin <- err == nil
	}()

	select {
	case ok := <-acquiredWithin:
		if !ok {
			t.Error("tx2 failed to acquire lock on pathB while tx1 holds pathA — isolation broken")
		}
	case <-time.After(1 * time.Second):
		t.Error("tx2 blocked acquiring lock on pathB while tx1 holds pathA — isolation broken")
	}
}

