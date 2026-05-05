//go:build sqliteonly && integration

package integration

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/sqlitestore"
)

func sqliteResetDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := sqlitestore.EnsureSchema(db); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	return db
}

func sqliteSeedResetUser(t *testing.T, db *sql.DB) uuid.UUID {
	t.Helper()
	users := sqlitestore.NewSQLiteUsersStore(db)
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
	return u.ID
}

func TestSQLitePasswordReset_InsertAndGetActive(t *testing.T) {
	db := sqliteResetDB(t)
	ctx := context.Background()
	userID := sqliteSeedResetUser(t, db)
	pr := sqlitestore.NewSQLitePasswordResetStore(db)
	hash := "h-" + uuid.New().String()
	expires := time.Now().Add(time.Hour).UTC().Truncate(time.Second)

	if _, err := pr.Insert(ctx, userID, hash, expires); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	gotUser, gotExpires, err := pr.GetActive(ctx, hash)
	if err != nil {
		t.Fatalf("GetActive: %v", err)
	}
	if gotUser != userID {
		t.Errorf("user_id = %v, want %v", gotUser, userID)
	}
	if !gotExpires.UTC().Truncate(time.Second).Equal(expires) {
		t.Errorf("expires_at = %v, want %v", gotExpires, expires)
	}
}

func TestSQLitePasswordReset_DuplicateTokenHashFails(t *testing.T) {
	db := sqliteResetDB(t)
	ctx := context.Background()
	userID := sqliteSeedResetUser(t, db)
	pr := sqlitestore.NewSQLitePasswordResetStore(db)
	hash := "dup"
	if _, err := pr.Insert(ctx, userID, hash, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("first: %v", err)
	}
	if _, err := pr.Insert(ctx, userID, hash, time.Now().Add(time.Hour)); err == nil {
		t.Fatal("expected UNIQUE violation, got nil")
	}
}

func TestSQLitePasswordReset_GetActiveExpired(t *testing.T) {
	db := sqliteResetDB(t)
	ctx := context.Background()
	userID := sqliteSeedResetUser(t, db)
	pr := sqlitestore.NewSQLitePasswordResetStore(db)
	hash := "exp"
	if _, err := pr.Insert(ctx, userID, hash, time.Now().Add(-time.Minute)); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if _, _, err := pr.GetActive(ctx, hash); !errors.Is(err, store.ErrPasswordResetNotFound) {
		t.Errorf("expected ErrPasswordResetNotFound, got %v", err)
	}
}

func TestSQLitePasswordReset_MarkUsedAtomic(t *testing.T) {
	db := sqliteResetDB(t)
	ctx := context.Background()
	userID := sqliteSeedResetUser(t, db)
	pr := sqlitestore.NewSQLitePasswordResetStore(db)
	hash := "atomic"
	if _, err := pr.Insert(ctx, userID, hash, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("insert: %v", err)
	}
	got, err := pr.MarkUsed(ctx, nil, hash)
	if err != nil {
		t.Fatalf("MarkUsed: %v", err)
	}
	if got != userID {
		t.Errorf("got %v want %v", got, userID)
	}
	if _, err := pr.MarkUsed(ctx, nil, hash); !errors.Is(err, store.ErrPasswordResetNotFound) {
		t.Errorf("second MarkUsed: expected ErrPasswordResetNotFound, got %v", err)
	}
}

func TestSQLitePasswordReset_MarkUsedExpiredFails(t *testing.T) {
	db := sqliteResetDB(t)
	ctx := context.Background()
	userID := sqliteSeedResetUser(t, db)
	pr := sqlitestore.NewSQLitePasswordResetStore(db)
	hash := "expmark"
	if _, err := pr.Insert(ctx, userID, hash, time.Now().Add(-time.Minute)); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if _, err := pr.MarkUsed(ctx, nil, hash); !errors.Is(err, store.ErrPasswordResetNotFound) {
		t.Errorf("expected ErrPasswordResetNotFound, got %v", err)
	}
}

func TestSQLitePasswordReset_MarkUsedTxAware(t *testing.T) {
	db := sqliteResetDB(t)
	ctx := context.Background()
	userID := sqliteSeedResetUser(t, db)
	pr := sqlitestore.NewSQLitePasswordResetStore(db)
	hash := "tx"
	if _, err := pr.Insert(ctx, userID, hash, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("insert: %v", err)
	}

	// Rollback path.
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if _, err := pr.MarkUsed(ctx, tx, hash); err != nil {
		t.Fatalf("MarkUsed in tx: %v", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	if _, _, err := pr.GetActive(ctx, hash); err != nil {
		t.Errorf("after rollback: token must remain active, got %v", err)
	}

	// Commit path.
	tx, err = db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin 2: %v", err)
	}
	if _, err := pr.MarkUsed(ctx, tx, hash); err != nil {
		t.Fatalf("MarkUsed in tx 2: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
	if _, _, err := pr.GetActive(ctx, hash); !errors.Is(err, store.ErrPasswordResetNotFound) {
		t.Errorf("after commit: token must be used, got %v", err)
	}
}

func TestSQLitePasswordReset_DeleteExpired(t *testing.T) {
	db := sqliteResetDB(t)
	ctx := context.Background()
	userID := sqliteSeedResetUser(t, db)
	pr := sqlitestore.NewSQLitePasswordResetStore(db)
	if _, err := pr.Insert(ctx, userID, "del1", time.Now().Add(-2*time.Hour)); err != nil {
		t.Fatalf("insert exp1: %v", err)
	}
	if _, err := pr.Insert(ctx, userID, "del2", time.Now().Add(-time.Hour)); err != nil {
		t.Fatalf("insert exp2: %v", err)
	}
	if _, err := pr.Insert(ctx, userID, "active", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("insert active: %v", err)
	}
	n, err := pr.DeleteExpired(ctx, time.Now())
	if err != nil {
		t.Fatalf("DeleteExpired: %v", err)
	}
	if n != 2 {
		t.Errorf("count = %d, want 2", n)
	}
	if _, _, err := pr.GetActive(ctx, "active"); err != nil {
		t.Errorf("active token must remain, got %v", err)
	}
}
