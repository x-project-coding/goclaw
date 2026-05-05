//go:build integration

package integration

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
)

// seedResetUser creates a fresh user and registers cleanup. Returned UUID is
// the user_id that reset tokens will FK to.
func seedResetUser(t *testing.T, db *sql.DB) uuid.UUID {
	t.Helper()
	users := pg.NewPGUsersStore(db)
	suffix := uuid.New().String()[:8]
	u := &store.User{
		Email:        "reset-" + suffix + "@local",
		PasswordHash: "stub",
		Role:         "member",
		Status:       "active",
	}
	if err := users.Create(context.Background(), u); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	t.Cleanup(func() {
		db.Exec("DELETE FROM password_reset_tokens WHERE user_id = $1", u.ID)
		db.Exec("DELETE FROM users WHERE id = $1", u.ID)
	})
	return u.ID
}

func TestPasswordReset_InsertAndGetActive(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	userID := seedResetUser(t, db)

	store := pg.NewPGPasswordResetStore(db)
	expires := time.Now().Add(time.Hour).UTC().Truncate(time.Microsecond)
	hash := "h-" + uuid.New().String()

	if _, err := store.Insert(ctx, userID, hash, expires); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	gotUser, gotExpires, err := store.GetActive(ctx, hash)
	if err != nil {
		t.Fatalf("GetActive: %v", err)
	}
	if gotUser != userID {
		t.Errorf("user_id = %v, want %v", gotUser, userID)
	}
	if !gotExpires.UTC().Truncate(time.Microsecond).Equal(expires) {
		t.Errorf("expires_at = %v, want %v", gotExpires, expires)
	}
}

func TestPasswordReset_InsertDuplicateTokenHashFails(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	userID := seedResetUser(t, db)
	store := pg.NewPGPasswordResetStore(db)
	hash := "dup-" + uuid.New().String()
	expires := time.Now().Add(time.Hour)

	if _, err := store.Insert(ctx, userID, hash, expires); err != nil {
		t.Fatalf("first insert: %v", err)
	}
	if _, err := store.Insert(ctx, userID, hash, expires); err == nil {
		t.Fatal("expected UNIQUE violation on duplicate token_hash, got nil")
	}
}

func TestPasswordReset_GetActiveExpired(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	userID := seedResetUser(t, db)
	pgStore := pg.NewPGPasswordResetStore(db)
	hash := "exp-" + uuid.New().String()

	if _, err := pgStore.Insert(ctx, userID, hash, time.Now().Add(-time.Minute)); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if _, _, err := pgStore.GetActive(ctx, hash); !errors.Is(err, store.ErrPasswordResetNotFound) {
		t.Errorf("expected ErrPasswordResetNotFound for expired token, got %v", err)
	}
}

func TestPasswordReset_GetActiveAlreadyUsed(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	userID := seedResetUser(t, db)
	pgStore := pg.NewPGPasswordResetStore(db)
	hash := "used-" + uuid.New().String()

	if _, err := pgStore.Insert(ctx, userID, hash, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if _, err := pgStore.MarkUsed(ctx, nil, hash); err != nil {
		t.Fatalf("MarkUsed: %v", err)
	}
	if _, _, err := pgStore.GetActive(ctx, hash); !errors.Is(err, store.ErrPasswordResetNotFound) {
		t.Errorf("expected ErrPasswordResetNotFound for used token, got %v", err)
	}
}

func TestPasswordReset_MarkUsedAtomicReturnsUserID(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	userID := seedResetUser(t, db)
	pgStore := pg.NewPGPasswordResetStore(db)
	hash := "atomic-" + uuid.New().String()

	if _, err := pgStore.Insert(ctx, userID, hash, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("insert: %v", err)
	}
	got, err := pgStore.MarkUsed(ctx, nil, hash)
	if err != nil {
		t.Fatalf("MarkUsed first: %v", err)
	}
	if got != userID {
		t.Errorf("MarkUsed user_id = %v, want %v", got, userID)
	}
	// Second call must fail.
	if _, err := pgStore.MarkUsed(ctx, nil, hash); !errors.Is(err, store.ErrPasswordResetNotFound) {
		t.Errorf("second MarkUsed: expected ErrPasswordResetNotFound, got %v", err)
	}
}

func TestPasswordReset_MarkUsedExpiredTokenFails(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	userID := seedResetUser(t, db)
	pgStore := pg.NewPGPasswordResetStore(db)
	hash := "expmark-" + uuid.New().String()

	if _, err := pgStore.Insert(ctx, userID, hash, time.Now().Add(-time.Minute)); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if _, err := pgStore.MarkUsed(ctx, nil, hash); !errors.Is(err, store.ErrPasswordResetNotFound) {
		t.Errorf("expected ErrPasswordResetNotFound for expired token, got %v", err)
	}
}

func TestPasswordReset_MarkUsedTxAware(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	userID := seedResetUser(t, db)
	pgStore := pg.NewPGPasswordResetStore(db)
	hash := "tx-" + uuid.New().String()

	if _, err := pgStore.Insert(ctx, userID, hash, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("insert: %v", err)
	}

	// Rollback path — token must remain active.
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if _, err := pgStore.MarkUsed(ctx, tx, hash); err != nil {
		t.Fatalf("MarkUsed in tx: %v", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	if _, _, err := pgStore.GetActive(ctx, hash); err != nil {
		t.Errorf("after rollback: token must still be active, got %v", err)
	}

	// Commit path.
	tx, err = db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin 2: %v", err)
	}
	got, err := pgStore.MarkUsed(ctx, tx, hash)
	if err != nil {
		t.Fatalf("MarkUsed in tx 2: %v", err)
	}
	if got != userID {
		t.Errorf("user_id = %v, want %v", got, userID)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
	if _, _, err := pgStore.GetActive(ctx, hash); !errors.Is(err, store.ErrPasswordResetNotFound) {
		t.Errorf("after commit: token must be used, got %v", err)
	}
}

func TestPasswordReset_DeleteExpired(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	userID := seedResetUser(t, db)
	pgStore := pg.NewPGPasswordResetStore(db)

	// 2 expired + 1 active.
	expHash1 := "del1-" + uuid.New().String()
	expHash2 := "del2-" + uuid.New().String()
	activeHash := "del3-" + uuid.New().String()
	if _, err := pgStore.Insert(ctx, userID, expHash1, time.Now().Add(-2*time.Hour)); err != nil {
		t.Fatalf("insert exp1: %v", err)
	}
	if _, err := pgStore.Insert(ctx, userID, expHash2, time.Now().Add(-time.Hour)); err != nil {
		t.Fatalf("insert exp2: %v", err)
	}
	if _, err := pgStore.Insert(ctx, userID, activeHash, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("insert active: %v", err)
	}

	n, err := pgStore.DeleteExpired(ctx, time.Now())
	if err != nil {
		t.Fatalf("DeleteExpired: %v", err)
	}
	if n != 2 {
		t.Errorf("DeleteExpired count = %d, want 2", n)
	}
	// Active row still present.
	if _, _, err := pgStore.GetActive(ctx, activeHash); err != nil {
		t.Errorf("active token must remain after DeleteExpired, got %v", err)
	}
}
