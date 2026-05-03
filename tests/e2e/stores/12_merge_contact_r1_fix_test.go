//go:build e2e

// Phase 09 Sub-09A — R1 fix verification.
//
// v3 bug: contact_merge_handlers.go flipped channel_contacts.merged_id but
// did NOT migrate agent_sessions.user_id to the merged user. After auth, the
// user appeared logged-in but their conversation history was orphaned.
//
// v4 fix: ContactStore.MergeUserAggregate runs all four UPDATEs in one TX so
// channel_contacts + agent_sessions + user_context_files + memory_documents
// move atomically.

package stores_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
	"github.com/nextlevelbuilder/goclaw/tests/e2e/helpers"
)

// TestMergeContactMigratesSessions asserts agent_sessions.user_id flips with
// the merge. This is the headline R1 fix — without it, post-merge users see
// no chat history.
func TestMergeContactMigratesSessions(t *testing.T) {
	helpers.ResetDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	db := helpers.MustDB(t)
	source := helpers.SeedUser(t, helpers.SeedUserOpts{Email: helpers.RandEmail("src")})
	target := helpers.SeedUser(t, helpers.SeedUserOpts{Email: helpers.RandEmail("tgt")})
	agent := helpers.SeedAgent(t, target.ID, "open")

	contactID := mustInsertContact(t, db, source.ID, "telegram", "tg-main", "tg-12345")
	sessionID := mustInsertAgentSession(t, db, agent.ID, source.ID, "session-r1-"+helpers.RandHex8())
	fileID := mustInsertContextFile(t, db, agent.ID, source.ID, "ctx.md")
	docID := mustInsertMemoryDoc(t, db, agent.ID, source.ID, "doc/path-"+helpers.RandHex8())

	cs := pg.NewPGContactStore(db)
	if err := cs.MergeUserAggregate(ctx, store.MergeUserAggregateRequest{
		ContactIDs:    []uuid.UUID{contactID},
		SourceUserIDs: []uuid.UUID{source.ID},
		TargetUserID:  target.ID,
		MergeAudit:    mustJSON(t, map[string]any{"merged_by_user_id": target.ID, "merged_at": "2026-05-03T00:00:00Z"}),
	}); err != nil {
		t.Fatalf("MergeUserAggregate: %v", err)
	}

	assertColumnEquals(t, db, "channel_contacts", "merged_id", contactID, target.ID.String())
	assertColumnEquals(t, db, "agent_sessions", "user_id", sessionID, target.ID.String())
	assertColumnEquals(t, db, "user_context_files", "user_id", fileID, target.ID.String())
	assertColumnEquals(t, db, "memory_documents", "user_id", docID, target.ID.String())
}

// TestMergeContactAtomic asserts the four UPDATEs share a single TX: when one
// pre-check fails (chained merge here), nothing is committed. Without atomic
// semantics, channel_contacts could flip while agent_sessions stays orphaned.
func TestMergeContactAtomic(t *testing.T) {
	helpers.ResetDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	db := helpers.MustDB(t)
	source := helpers.SeedUser(t, helpers.SeedUserOpts{Email: helpers.RandEmail("src")})
	target := helpers.SeedUser(t, helpers.SeedUserOpts{Email: helpers.RandEmail("tgt")})
	priorMerge := helpers.SeedUser(t, helpers.SeedUserOpts{Email: helpers.RandEmail("prior")})
	agent := helpers.SeedAgent(t, target.ID, "open")

	contactID := mustInsertContact(t, db, source.ID, "telegram", "tg-main", "tg-67890")
	sessionID := mustInsertAgentSession(t, db, agent.ID, source.ID, "session-atomic-"+helpers.RandHex8())

	// Stage a chained-merge tripwire: target user has a contact pointing
	// elsewhere (priorMerge). MergeUserAggregate must reject and roll back.
	mustInsertContactWithMerge(t, db, target.ID, priorMerge.ID, "discord", "dc-main", "dc-1")

	cs := pg.NewPGContactStore(db)
	err := cs.MergeUserAggregate(ctx, store.MergeUserAggregateRequest{
		ContactIDs:    []uuid.UUID{contactID},
		SourceUserIDs: []uuid.UUID{source.ID},
		TargetUserID:  target.ID,
	})
	if err == nil {
		t.Fatalf("expected ErrMergeTargetAlreadyMerged, got nil")
	}

	// Atomicity proof: post-rollback, neither table moved.
	assertColumnEquals(t, db, "channel_contacts", "merged_id::text", contactID, "")
	assertColumnEquals(t, db, "agent_sessions", "user_id", sessionID, source.ID.String())
}

// mustJSON marshals v and fails the test on any error.
func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}
