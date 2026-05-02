//go:build e2e

package stores_test

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
	"github.com/nextlevelbuilder/goclaw/tests/e2e/helpers"
)

// TestUserSessionsCreateRevoke creates a refresh session, looks it up by hash,
// revokes it, and verifies revoked sessions are excluded from the active query.
func TestUserSessionsCreateRevoke(t *testing.T) {
	helpers.ResetDB(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	users := pg.NewPGUsersStore(helpers.MustDB(t))
	sessions := pg.NewPGUserSessionsStore(helpers.MustDB(t))

	// Seed a user via the users store (so we have a valid FK target).
	u := &store.User{
		Email:        helpers.RandEmail("sess"),
		PasswordHash: "argon2id$opaque",
		Role:         "member",
		Status:       "active",
	}
	if err := users.Create(ctx, u); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	familyID := uuid.Must(uuid.NewV7())
	hash := sha256Hex(randBytes(32))
	sess := &store.UserSession{
		UserID:           u.ID,
		FamilyID:         familyID,
		RefreshTokenHash: hash,
		ExpiresAt:        time.Now().Add(30 * 24 * time.Hour),
	}
	if err := sessions.Create(ctx, sess); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if sess.ID == uuid.Nil {
		t.Fatalf("Create: ID not populated")
	}

	got, err := sessions.GetByHash(ctx, hash)
	if err != nil {
		t.Fatalf("GetByHash: %v", err)
	}
	if got.ID != sess.ID || got.UserID != u.ID {
		t.Fatalf("GetByHash mismatch")
	}
	if got.RevokedAt != nil {
		t.Fatalf("GetByHash: expected RevokedAt nil, got %v", got.RevokedAt)
	}

	active, err := sessions.ListActiveByUser(ctx, u.ID)
	if err != nil {
		t.Fatalf("ListActiveByUser: %v", err)
	}
	if len(active) != 1 {
		t.Fatalf("ListActiveByUser: want 1, got %d", len(active))
	}

	if err := sessions.Revoke(ctx, sess.ID); err != nil {
		t.Fatalf("Revoke: %v", err)
	}

	active, err = sessions.ListActiveByUser(ctx, u.ID)
	if err != nil {
		t.Fatalf("ListActiveByUser after Revoke: %v", err)
	}
	if len(active) != 0 {
		t.Fatalf("ListActiveByUser after Revoke: want 0, got %d", len(active))
	}

	// GetByHash should still find the row but with RevokedAt set
	// (callers decide whether to honor revocation; store stays a thin layer).
	got, err = sessions.GetByHash(ctx, hash)
	if err != nil {
		t.Fatalf("GetByHash post-revoke: %v", err)
	}
	if got.RevokedAt == nil {
		t.Fatalf("GetByHash post-revoke: RevokedAt still nil")
	}
}

// TestUserSessionsRefreshTokenUnique verifies the UNIQUE constraint on refresh_token_hash.
func TestUserSessionsRefreshTokenUnique(t *testing.T) {
	helpers.ResetDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	users := pg.NewPGUsersStore(helpers.MustDB(t))
	sessions := pg.NewPGUserSessionsStore(helpers.MustDB(t))

	u := &store.User{Email: helpers.RandEmail("dup"), PasswordHash: "x", Role: "member", Status: "active"}
	if err := users.Create(ctx, u); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	hash := sha256Hex(randBytes(32))
	first := &store.UserSession{
		UserID: u.ID, FamilyID: uuid.Must(uuid.NewV7()),
		RefreshTokenHash: hash, ExpiresAt: time.Now().Add(time.Hour),
	}
	dup := &store.UserSession{
		UserID: u.ID, FamilyID: uuid.Must(uuid.NewV7()),
		RefreshTokenHash: hash, ExpiresAt: time.Now().Add(time.Hour),
	}
	if err := sessions.Create(ctx, first); err != nil {
		t.Fatalf("Create first: %v", err)
	}
	if err := sessions.Create(ctx, dup); err == nil {
		t.Fatalf("Create dup hash: want UNIQUE error, got nil")
	}
}

// TestUserSessionsGetByHashNotFound returns store.ErrNotFound for unknown hash.
func TestUserSessionsGetByHashNotFound(t *testing.T) {
	helpers.ResetDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	sessions := pg.NewPGUserSessionsStore(helpers.MustDB(t))
	if _, err := sessions.GetByHash(ctx, sha256Hex(randBytes(32))); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func sha256Hex(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func randBytes(n int) []byte {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return b
}
