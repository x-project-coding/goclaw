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

// confirmTriple wires the trio of stores ConfirmPasswordReset depends on.
func confirmTriple(t *testing.T, db *sql.DB) (*pg.PGUsersStore, *pg.PGUserSessionsStore, *pg.PGPasswordResetStore) {
	t.Helper()
	users := pg.NewPGUsersStore(db)
	sessions := pg.NewPGUserSessionsStore(db)
	resets := pg.NewPGPasswordResetStore(db)
	users.UseSessions(sessions)
	users.UseResetTokens(resets)
	return users, sessions, resets
}

// seedUserWithSessionsAndResetToken returns userID + active sessions + a
// fresh reset token hash. The user has 2 active sessions; ConfirmPasswordReset
// must revoke both AND mark the token used AND rotate the password — all in
// one TX.
func seedUserWithSessionsAndResetToken(t *testing.T, db *sql.DB) (uuid.UUID, string) {
	t.Helper()

	users, _, resets := confirmTriple(t, db)
	suffix := uuid.New().String()[:8]
	u := &store.User{
		Email:        "confirm-" + suffix + "@local",
		PasswordHash: "stub-old",
		Role:         "member",
		Status:       "active",
	}
	if err := users.Create(context.Background(), u); err != nil {
		t.Fatalf("create user: %v", err)
	}
	t.Cleanup(func() {
		db.Exec("DELETE FROM password_reset_tokens WHERE user_id = $1", u.ID)
		db.Exec("DELETE FROM user_sessions WHERE user_id = $1", u.ID)
		db.Exec("DELETE FROM users WHERE id = $1", u.ID)
	})

	// 2 active sessions.
	sessionsStore := pg.NewPGUserSessionsStore(db)
	familyID := uuid.New()
	for i := 0; i < 2; i++ {
		s := &store.UserSession{
			UserID:           u.ID,
			FamilyID:         familyID,
			RefreshTokenHash: "hash-" + uuid.New().String(),
			ExpiresAt:        time.Now().Add(24 * time.Hour),
		}
		if err := sessionsStore.Create(context.Background(), s); err != nil {
			t.Fatalf("create session %d: %v", i, err)
		}
	}

	// Reset token.
	hash := "h-" + uuid.New().String()
	if _, err := resets.Insert(context.Background(), u.ID, hash, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("insert reset: %v", err)
	}
	return u.ID, hash
}

func TestConfirmPasswordReset_AtomicAcrossThreeWrites(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	userID, hash := seedUserWithSessionsAndResetToken(t, db)
	users, _, resets := confirmTriple(t, db)

	const newHash = "argon2id-new"
	if err := users.ConfirmPasswordReset(ctx, hash, newHash); err != nil {
		t.Fatalf("ConfirmPasswordReset: %v", err)
	}

	// Password rotated.
	var got string
	if err := db.QueryRow(`SELECT password_hash FROM users WHERE id = $1`, userID).Scan(&got); err != nil {
		t.Fatalf("read password: %v", err)
	}
	if got != newHash {
		t.Errorf("password_hash = %q, want %q", got, newHash)
	}
	// All sessions revoked.
	var active int
	if err := db.QueryRow(`SELECT COUNT(*) FROM user_sessions WHERE user_id = $1 AND revoked_at IS NULL`, userID).Scan(&active); err != nil {
		t.Fatalf("count active: %v", err)
	}
	if active != 0 {
		t.Errorf("expected 0 active sessions, got %d", active)
	}
	// Token consumed (GetActive returns ErrPasswordResetNotFound).
	if _, _, err := resets.GetActive(ctx, hash); !errors.Is(err, store.ErrPasswordResetNotFound) {
		t.Errorf("expected ErrPasswordResetNotFound after confirm, got %v", err)
	}
}

func TestConfirmPasswordReset_InvalidTokenReturnsSentinel(t *testing.T) {
	db := testDB(t)
	users, _, _ := confirmTriple(t, db)
	if err := users.ConfirmPasswordReset(context.Background(), "no-such-hash", "argon2id-new"); !errors.Is(err, store.ErrPasswordResetNotFound) {
		t.Errorf("expected ErrPasswordResetNotFound, got %v", err)
	}
}

func TestConfirmPasswordReset_RollsBackOnFailure(t *testing.T) {
	db := testDB(t)
	parent := context.Background()
	userID, hash := seedUserWithSessionsAndResetToken(t, db)
	users, _, _ := confirmTriple(t, db)

	var origHash string
	if err := db.QueryRow(`SELECT password_hash FROM users WHERE id = $1`, userID).Scan(&origHash); err != nil {
		t.Fatalf("read original: %v", err)
	}

	ctx, cancel := context.WithCancel(parent)
	cancel()

	if err := users.ConfirmPasswordReset(ctx, hash, "argon2id-new"); err == nil {
		t.Fatal("expected error from cancelled ctx, got nil")
	}

	// Password unchanged.
	var got string
	if err := db.QueryRow(`SELECT password_hash FROM users WHERE id = $1`, userID).Scan(&got); err != nil {
		t.Fatalf("read password: %v", err)
	}
	if got != origHash {
		t.Errorf("password mutated on rollback: got %q, want %q", got, origHash)
	}
	// Token still active.
	var active int
	if err := db.QueryRow(`SELECT COUNT(*) FROM user_sessions WHERE user_id = $1 AND revoked_at IS NULL`, userID).Scan(&active); err != nil {
		t.Fatalf("count active: %v", err)
	}
	if active != 2 {
		t.Errorf("expected 2 active sessions on rollback, got %d", active)
	}
}
