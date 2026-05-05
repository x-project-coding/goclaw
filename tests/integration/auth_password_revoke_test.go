//go:build integration

package integration

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
)

// seedUserWithSessions creates a user + N active sessions and returns the
// user ID, the active session IDs, and a cleanup func.
func seedUserWithSessions(t *testing.T, db *sql.DB, n int) (uuid.UUID, []uuid.UUID) {
	t.Helper()

	users := pg.NewPGUsersStore(db)
	sessions := pg.NewPGUserSessionsStore(db)

	suffix := uuid.New().String()[:8]
	u := &store.User{
		Email:        "revoke-" + suffix + "@local",
		PasswordHash: "bcrypt-stub",
		Role:         "member",
		Status:       "active",
	}
	if err := users.Create(context.Background(), u); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	t.Cleanup(func() {
		db.Exec("DELETE FROM user_sessions WHERE user_id = $1", u.ID)
		db.Exec("DELETE FROM users WHERE id = $1", u.ID)
	})

	familyID := uuid.New()
	ids := make([]uuid.UUID, 0, n)
	for i := 0; i < n; i++ {
		s := &store.UserSession{
			UserID:           u.ID,
			FamilyID:         familyID,
			RefreshTokenHash: "hash-" + uuid.New().String(),
			ExpiresAt:        time.Now().Add(24 * time.Hour),
		}
		if err := sessions.Create(context.Background(), s); err != nil {
			t.Fatalf("seed session %d: %v", i, err)
		}
		ids = append(ids, s.ID)
	}
	return u.ID, ids
}

func countActiveSessions(t *testing.T, db *sql.DB, userID uuid.UUID) int {
	t.Helper()
	var n int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM user_sessions WHERE user_id = $1 AND revoked_at IS NULL`,
		userID).Scan(&n); err != nil {
		t.Fatalf("count active: %v", err)
	}
	return n
}

// TestRevokeAllActiveByUser_BulkUpdate seeds 3 active + 1 pre-revoked,
// asserts all 3 are revoked and the pre-revoked row's revoked_at is unchanged.
func TestRevokeAllActiveByUser_BulkUpdate(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	userID, ids := seedUserWithSessions(t, db, 4)

	// Pre-revoke the last one with a deterministic timestamp.
	preRevoked := time.Now().Add(-1 * time.Hour).UTC().Truncate(time.Microsecond)
	if _, err := db.Exec(
		`UPDATE user_sessions SET revoked_at = $1 WHERE id = $2`,
		preRevoked, ids[3]); err != nil {
		t.Fatalf("pre-revoke: %v", err)
	}

	sessions := pg.NewPGUserSessionsStore(db)
	if err := sessions.RevokeAllActiveByUser(ctx, sessions.DB(), userID); err != nil {
		t.Fatalf("RevokeAllActiveByUser: %v", err)
	}

	if got := countActiveSessions(t, db, userID); got != 0 {
		t.Errorf("expected 0 active sessions, got %d", got)
	}

	// Pre-revoked row's revoked_at must NOT have shifted to "now".
	var got time.Time
	if err := db.QueryRow(
		`SELECT revoked_at FROM user_sessions WHERE id = $1`, ids[3]).Scan(&got); err != nil {
		t.Fatalf("read pre-revoked: %v", err)
	}
	if !got.UTC().Truncate(time.Microsecond).Equal(preRevoked) {
		t.Errorf("pre-revoked row was overwritten: %v != %v", got, preRevoked)
	}
}

// TestRevokeAllActiveByUser_TxAware verifies the bulk update honors a *sql.Tx
// — rollback leaves sessions active; commit revokes them.
func TestRevokeAllActiveByUser_TxAware(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	userID, _ := seedUserWithSessions(t, db, 2)

	sessions := pg.NewPGUserSessionsStore(db)

	// Rollback path.
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := sessions.RevokeAllActiveByUser(ctx, tx, userID); err != nil {
		t.Fatalf("revoke in tx: %v", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	if got := countActiveSessions(t, db, userID); got != 2 {
		t.Errorf("after rollback: expected 2 active, got %d", got)
	}

	// Commit path.
	tx, err = db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin 2: %v", err)
	}
	if err := sessions.RevokeAllActiveByUser(ctx, tx, userID); err != nil {
		t.Fatalf("revoke in tx 2: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
	if got := countActiveSessions(t, db, userID); got != 0 {
		t.Errorf("after commit: expected 0 active, got %d", got)
	}
}

// TestRevokeAllActiveByUser_NoActiveNoOp confirms a user with zero active
// sessions is a no-op (no error).
func TestRevokeAllActiveByUser_NoActiveNoOp(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	users := pg.NewPGUsersStore(db)
	suffix := uuid.New().String()[:8]
	u := &store.User{Email: "noop-" + suffix + "@local", PasswordHash: "stub", Role: "member", Status: "active"}
	if err := users.Create(ctx, u); err != nil {
		t.Fatalf("create user: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM users WHERE id = $1", u.ID) })

	sessions := pg.NewPGUserSessionsStore(db)
	if err := sessions.RevokeAllActiveByUser(ctx, sessions.DB(), u.ID); err != nil {
		t.Errorf("expected nil error on no-op, got %v", err)
	}
}

// TestChangePasswordAndRevokeSessions_Atomic happy path: password updates and
// every active session is revoked, all in one TX.
func TestChangePasswordAndRevokeSessions_Atomic(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	userID, _ := seedUserWithSessions(t, db, 3)

	users := pg.NewPGUsersStore(db)
	sessions := pg.NewPGUserSessionsStore(db)
	users.UseSessions(sessions)

	const newHash = "argon2id-stub-new"
	if err := users.ChangePasswordAndRevokeSessions(ctx, userID, newHash); err != nil {
		t.Fatalf("ChangePasswordAndRevokeSessions: %v", err)
	}

	var got string
	if err := db.QueryRow(`SELECT password_hash FROM users WHERE id = $1`, userID).Scan(&got); err != nil {
		t.Fatalf("read password_hash: %v", err)
	}
	if got != newHash {
		t.Errorf("password_hash = %q, want %q", got, newHash)
	}
	if n := countActiveSessions(t, db, userID); n != 0 {
		t.Errorf("expected 0 active sessions, got %d", n)
	}
}

// TestChangePasswordAndRevokeSessions_RollsBackOnFailure feeds a cancelled
// context after the BeginTx and asserts NEITHER the password nor the sessions
// are mutated.
func TestChangePasswordAndRevokeSessions_RollsBackOnFailure(t *testing.T) {
	db := testDB(t)
	parent := context.Background()
	userID, _ := seedUserWithSessions(t, db, 2)

	// Snapshot original password hash for the assertion.
	var origHash string
	if err := db.QueryRow(`SELECT password_hash FROM users WHERE id = $1`, userID).Scan(&origHash); err != nil {
		t.Fatalf("read original: %v", err)
	}

	users := pg.NewPGUsersStore(db)
	sessions := pg.NewPGUserSessionsStore(db)
	users.UseSessions(sessions)

	// Wire UseSessions to a sessions store whose RevokeAllActiveByUser is
	// guaranteed to fail by passing a pre-cancelled ctx.
	ctx, cancel := context.WithCancel(parent)
	cancel()

	if err := users.ChangePasswordAndRevokeSessions(ctx, userID, "argon2id-stub-new"); err == nil {
		t.Fatal("expected error from cancelled ctx, got nil")
	}

	// Password unchanged.
	var got string
	if err := db.QueryRow(`SELECT password_hash FROM users WHERE id = $1`, userID).Scan(&got); err != nil {
		t.Fatalf("read password_hash: %v", err)
	}
	if got != origHash {
		t.Errorf("password_hash mutated on rollback: got %q, want %q", got, origHash)
	}
	// Sessions still active.
	if n := countActiveSessions(t, db, userID); n != 2 {
		t.Errorf("expected 2 active sessions on rollback, got %d", n)
	}
}
