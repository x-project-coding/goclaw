//go:build e2e

// Phase 09 Sub-09C — paired_devices.user_id is nullable + bound after auth.
//
// v3 paired devices were tenant-scoped (paired_devices.tenant_id NOT NULL).
// v4 drops tenant_id and exposes user_id as nullable: row inserts pre-auth,
// PairingStore.BindUser stamps the resolved user once the channel manager has
// the UUID.

package stores_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
	"github.com/nextlevelbuilder/goclaw/tests/e2e/helpers"
)

func TestPairedDevicesUserNullableThenBound(t *testing.T) {
	helpers.ResetDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	db := helpers.MustDB(t)
	user := helpers.SeedUser(t, helpers.SeedUserOpts{Email: helpers.RandEmail("paired")})
	ps := pg.NewPGPairingStore(db)

	// Request + approve produces a paired_devices row with user_id NULL.
	code, err := ps.RequestPairing(ctx, "tg-12345", "telegram", "chat-9", "default", nil)
	if err != nil {
		t.Fatalf("RequestPairing: %v", err)
	}
	if _, err := ps.ApprovePairing(ctx, code, "admin"); err != nil {
		t.Fatalf("ApprovePairing: %v", err)
	}

	var preBind sql.NullString
	if err := db.QueryRowContext(ctx,
		`SELECT user_id::text FROM paired_devices WHERE sender_id = $1 AND channel = $2`,
		"tg-12345", "telegram",
	).Scan(&preBind); err != nil && !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("read pre-bind user_id: %v", err)
	}
	if preBind.Valid && preBind.String != "" {
		t.Fatalf("user_id must be NULL pre-bind, got %q", preBind.String)
	}

	// First message resolves to user → channel manager calls BindUser.
	if err := ps.BindUser(ctx, "tg-12345", "telegram", user.ID); err != nil {
		t.Fatalf("BindUser: %v", err)
	}

	var postBind string
	if err := db.QueryRowContext(ctx,
		`SELECT user_id::text FROM paired_devices WHERE sender_id = $1 AND channel = $2`,
		"tg-12345", "telegram",
	).Scan(&postBind); err != nil {
		t.Fatalf("read post-bind user_id: %v", err)
	}
	if postBind != user.ID.String() {
		t.Fatalf("user_id post-bind want %s, got %s", user.ID, postBind)
	}

	// Idempotent: re-binding the same user is a no-op.
	if err := ps.BindUser(ctx, "tg-12345", "telegram", user.ID); err != nil {
		t.Fatalf("BindUser (re-bind): %v", err)
	}

	// Hijack rejection: a different user trying to bind the same device must fail.
	other := helpers.SeedUser(t, helpers.SeedUserOpts{Email: helpers.RandEmail("hijack")})
	if err := ps.BindUser(ctx, "tg-12345", "telegram", other.ID); !errors.Is(err, store.ErrPairingBoundToDifferentUser) {
		t.Fatalf("hijack: want ErrPairingBoundToDifferentUser, got %v", err)
	}
}
