//go:build e2e

// Atomic contact-merge concurrency proof.
//
// If the merge isn't actually atomic (multiple TXes), concurrent admins or
// retried requests would cascade partial state: contact flipped but sessions
// orphaned, etc. This test asserts that under N concurrent merge attempts on
// the same source set, exactly one succeeds and the rest see
// ErrMergeSourceAlreadyMerged.

package stores_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
	"github.com/nextlevelbuilder/goclaw/tests/e2e/helpers"
)

// TestMergeContactDuringActiveSession races N goroutines competing to merge
// the same contact. Atomic semantics require: exactly one wins, the rest get
// ErrMergeSourceAlreadyMerged on their pre-check (because the winner's TX
// flipped merged_id, locked under SELECT FOR UPDATE).
//
// Side proof: the agent_sessions row owned by source MUST end up flipped to
// target — the winner's TX could not have committed without that UPDATE.
func TestMergeContactDuringActiveSession(t *testing.T) {
	helpers.ResetDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	db := helpers.MustDB(t)
	source := helpers.SeedUser(t, helpers.SeedUserOpts{Email: helpers.RandEmail("src")})
	target := helpers.SeedUser(t, helpers.SeedUserOpts{Email: helpers.RandEmail("tgt")})
	agent := helpers.SeedAgent(t, target.ID, "open")

	contactID := mustInsertContact(t, db, source.ID, "telegram", "tg-main", "tg-race")
	sessionID := mustInsertAgentSession(t, db, agent.ID, source.ID, "session-race-"+helpers.RandHex8())

	const racers = 8
	cs := pg.NewPGContactStore(db)

	var wg sync.WaitGroup
	var success int32
	var alreadyMerged int32
	wg.Add(racers)
	start := make(chan struct{})

	for range racers {
		go func() {
			defer wg.Done()
			<-start
			err := cs.MergeUserAggregate(ctx, store.MergeUserAggregateRequest{
				ContactIDs:    []uuid.UUID{contactID},
				SourceUserIDs: []uuid.UUID{source.ID},
				TargetUserID:  target.ID,
			})
			switch {
			case err == nil:
				atomic.AddInt32(&success, 1)
			case errors.Is(err, store.ErrMergeSourceAlreadyMerged):
				atomic.AddInt32(&alreadyMerged, 1)
			default:
				t.Errorf("unexpected error: %v", err)
			}
		}()
	}
	close(start)
	wg.Wait()

	if got := atomic.LoadInt32(&success); got != 1 {
		t.Fatalf("expected exactly 1 success, got %d", got)
	}
	if got := atomic.LoadInt32(&alreadyMerged); got != racers-1 {
		t.Fatalf("expected %d ErrMergeSourceAlreadyMerged, got %d", racers-1, got)
	}

	// Atomic invariant: winner's TX committed all four UPDATEs, so the
	// pre-existing session must have flipped to target.
	assertColumnEquals(t, db, "agent_sessions", "user_id", sessionID, target.ID.String())
	assertColumnEquals(t, db, "channel_contacts", "merged_id::text", contactID, target.ID.String())
}
