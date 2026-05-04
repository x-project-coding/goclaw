//go:build e2e

// Contact-merge security guards.
//
// Without these checks, RoleAdmin alone could silently hijack any user's
// data: merge user A's contacts into user B, or chain merges so data hops
// across multiple users undetected.

package stores_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
	"github.com/nextlevelbuilder/goclaw/tests/e2e/helpers"
)

// TestMergeRejectsSourceAlreadyMerged blocks user→user data movement: a
// contact whose merged_id is already populated cannot be re-merged elsewhere.
func TestMergeRejectsSourceAlreadyMerged(t *testing.T) {
	helpers.ResetDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	db := helpers.MustDB(t)
	source := helpers.SeedUser(t, helpers.SeedUserOpts{Email: helpers.RandEmail("src")})
	prior := helpers.SeedUser(t, helpers.SeedUserOpts{Email: helpers.RandEmail("prior")})
	target := helpers.SeedUser(t, helpers.SeedUserOpts{Email: helpers.RandEmail("tgt")})

	// Already-merged contact: merged_id = prior. Admin trying to re-merge
	// into target = silent account hijack — must be rejected.
	contactID := mustInsertContactWithMerge(t, db, source.ID, prior.ID, "telegram", "tg-main", "tg-already")

	cs := pg.NewPGContactStore(db)
	err := cs.MergeUserAggregate(ctx, store.MergeUserAggregateRequest{
		ContactIDs:    []uuid.UUID{contactID},
		SourceUserIDs: []uuid.UUID{source.ID},
		TargetUserID:  target.ID,
	})
	if !errors.Is(err, store.ErrMergeSourceAlreadyMerged) {
		t.Fatalf("want ErrMergeSourceAlreadyMerged, got %v", err)
	}

	// State unchanged.
	assertColumnEquals(t, db, "channel_contacts", "merged_id::text", contactID, prior.ID.String())
}

// TestMergeRejectsChainedMerges caps merge depth at 1 — once a user has been
// merged elsewhere, they cannot become a merge target.
func TestMergeRejectsChainedMerges(t *testing.T) {
	helpers.ResetDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	db := helpers.MustDB(t)
	source := helpers.SeedUser(t, helpers.SeedUserOpts{Email: helpers.RandEmail("src")})
	target := helpers.SeedUser(t, helpers.SeedUserOpts{Email: helpers.RandEmail("tgt")})
	upstream := helpers.SeedUser(t, helpers.SeedUserOpts{Email: helpers.RandEmail("up")})

	contactID := mustInsertContact(t, db, source.ID, "telegram", "tg-main", "tg-chain")
	// target was previously merged into upstream → chained-merge tripwire.
	mustInsertContactWithMerge(t, db, target.ID, upstream.ID, "discord", "dc-main", "dc-prev")

	cs := pg.NewPGContactStore(db)
	err := cs.MergeUserAggregate(ctx, store.MergeUserAggregateRequest{
		ContactIDs:    []uuid.UUID{contactID},
		SourceUserIDs: []uuid.UUID{source.ID},
		TargetUserID:  target.ID,
	})
	if !errors.Is(err, store.ErrMergeTargetAlreadyMerged) {
		t.Fatalf("want ErrMergeTargetAlreadyMerged, got %v", err)
	}
}

// TestMergeRejectsMissingTarget enforces the FK guard — admin cannot conjure
// a target user UUID; the row must exist.
func TestMergeRejectsMissingTarget(t *testing.T) {
	helpers.ResetDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	db := helpers.MustDB(t)
	source := helpers.SeedUser(t, helpers.SeedUserOpts{Email: helpers.RandEmail("src")})
	contactID := mustInsertContact(t, db, source.ID, "telegram", "tg-main", "tg-orphan")

	missing, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("uuid: %v", err)
	}

	cs := pg.NewPGContactStore(db)
	mergeErr := cs.MergeUserAggregate(ctx, store.MergeUserAggregateRequest{
		ContactIDs:    []uuid.UUID{contactID},
		SourceUserIDs: []uuid.UUID{source.ID},
		TargetUserID:  missing,
	})
	if !errors.Is(mergeErr, store.ErrMergeTargetUserNotFound) {
		t.Fatalf("want ErrMergeTargetUserNotFound, got %v", mergeErr)
	}
}

// TestMergeAuditPopulated verifies merge_audit JSONB is written so forensic
// queries can reconstruct who/when/from-where for each merge.
func TestMergeAuditPopulated(t *testing.T) {
	helpers.ResetDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	db := helpers.MustDB(t)
	source := helpers.SeedUser(t, helpers.SeedUserOpts{Email: helpers.RandEmail("src")})
	target := helpers.SeedUser(t, helpers.SeedUserOpts{Email: helpers.RandEmail("tgt")})

	contactID := mustInsertContact(t, db, source.ID, "telegram", "tg-main", "tg-audit")

	cs := pg.NewPGContactStore(db)
	if err := cs.MergeUserAggregate(ctx, store.MergeUserAggregateRequest{
		ContactIDs:    []uuid.UUID{contactID},
		SourceUserIDs: []uuid.UUID{source.ID},
		TargetUserID:  target.ID,
		MergeAudit:    mustJSON(t, map[string]any{"merged_by_user_id": target.ID, "merged_at": "2026-05-03T00:00:00Z"}),
	}); err != nil {
		t.Fatalf("MergeUserAggregate: %v", err)
	}

	// Audit JSONB must include merged_by_user_id (not the empty default).
	var audit string
	if err := db.QueryRowContext(ctx,
		`SELECT merge_audit::text FROM channel_contacts WHERE id = $1`, contactID,
	).Scan(&audit); err != nil {
		t.Fatalf("read audit: %v", err)
	}
	if audit == "" || audit == "{}" {
		t.Fatalf("merge_audit empty post-merge: %q", audit)
	}
}
