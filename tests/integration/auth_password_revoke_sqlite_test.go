//go:build sqliteonly && integration

package integration

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/sqlitestore"
)

// sqliteAuthDB returns a fresh in-memory SQLite DB with v4 schema applied.
func sqliteAuthDB(t *testing.T) *sql.DB {
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

func sqliteSeedUserWithSessions(t *testing.T, db *sql.DB, n int) (uuid.UUID, []uuid.UUID) {
	t.Helper()

	users := sqlitestore.NewSQLiteUsersStore(db)
	sessions := sqlitestore.NewSQLiteUserSessionsStore(db)

	suffix := uuid.New().String()[:8]
	u := &store.User{
		Email:        "revoke-" + suffix + "@local",
		PasswordHash: "stub",
		Role:         "member",
		Status:       "active",
	}
	if err := users.Create(context.Background(), u); err != nil {
		t.Fatalf("seed user: %v", err)
	}

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

func sqliteCountActive(t *testing.T, db *sql.DB, userID uuid.UUID) int {
	t.Helper()
	var n int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM user_sessions WHERE user_id = ? AND revoked_at IS NULL`,
		userID).Scan(&n); err != nil {
		t.Fatalf("count active: %v", err)
	}
	return n
}

func TestSQLiteRevokeAllActiveByUser_BulkUpdate(t *testing.T) {
	db := sqliteAuthDB(t)
	ctx := context.Background()
	userID, ids := sqliteSeedUserWithSessions(t, db, 4)

	preRevoked := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339Nano)
	if _, err := db.Exec(`UPDATE user_sessions SET revoked_at = ? WHERE id = ?`, preRevoked, ids[3]); err != nil {
		t.Fatalf("pre-revoke: %v", err)
	}

	sessions := sqlitestore.NewSQLiteUserSessionsStore(db)
	if err := sessions.RevokeAllActiveByUser(ctx, sessions.DB(), userID); err != nil {
		t.Fatalf("RevokeAllActiveByUser: %v", err)
	}
	if got := sqliteCountActive(t, db, userID); got != 0 {
		t.Errorf("expected 0 active, got %d", got)
	}

	var got string
	if err := db.QueryRow(`SELECT revoked_at FROM user_sessions WHERE id = ?`, ids[3]).Scan(&got); err != nil {
		t.Fatalf("read pre-revoked: %v", err)
	}
	if got != preRevoked {
		t.Errorf("pre-revoked overwritten: %q != %q", got, preRevoked)
	}
}

func TestSQLiteRevokeAllActiveByUser_TxAware(t *testing.T) {
	db := sqliteAuthDB(t)
	ctx := context.Background()
	userID, _ := sqliteSeedUserWithSessions(t, db, 2)
	sessions := sqlitestore.NewSQLiteUserSessionsStore(db)

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
	if got := sqliteCountActive(t, db, userID); got != 2 {
		t.Errorf("after rollback: expected 2, got %d", got)
	}

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
	if got := sqliteCountActive(t, db, userID); got != 0 {
		t.Errorf("after commit: expected 0, got %d", got)
	}
}

func TestSQLiteChangePasswordAndRevokeSessions_Atomic(t *testing.T) {
	db := sqliteAuthDB(t)
	ctx := context.Background()
	userID, _ := sqliteSeedUserWithSessions(t, db, 3)

	users := sqlitestore.NewSQLiteUsersStore(db)
	sessions := sqlitestore.NewSQLiteUserSessionsStore(db)
	users.UseSessions(sessions)

	const newHash = "argon2id-stub-new"
	if err := users.ChangePasswordAndRevokeSessions(ctx, userID, newHash); err != nil {
		t.Fatalf("ChangePasswordAndRevokeSessions: %v", err)
	}
	var got string
	if err := db.QueryRow(`SELECT password_hash FROM users WHERE id = ?`, userID).Scan(&got); err != nil {
		t.Fatalf("read password_hash: %v", err)
	}
	if got != newHash {
		t.Errorf("password_hash = %q, want %q", got, newHash)
	}
	if n := sqliteCountActive(t, db, userID); n != 0 {
		t.Errorf("expected 0 active sessions, got %d", n)
	}
}

func TestSQLiteChangePasswordAndRevokeSessions_RollsBackOnFailure(t *testing.T) {
	db := sqliteAuthDB(t)
	parent := context.Background()
	userID, _ := sqliteSeedUserWithSessions(t, db, 2)

	var origHash string
	if err := db.QueryRow(`SELECT password_hash FROM users WHERE id = ?`, userID).Scan(&origHash); err != nil {
		t.Fatalf("read original: %v", err)
	}

	users := sqlitestore.NewSQLiteUsersStore(db)
	sessions := sqlitestore.NewSQLiteUserSessionsStore(db)
	users.UseSessions(sessions)

	ctx, cancel := context.WithCancel(parent)
	cancel()

	if err := users.ChangePasswordAndRevokeSessions(ctx, userID, "argon2id-stub-new"); err == nil {
		t.Fatal("expected error from cancelled ctx, got nil")
	}

	var got string
	if err := db.QueryRow(`SELECT password_hash FROM users WHERE id = ?`, userID).Scan(&got); err != nil {
		t.Fatalf("read password_hash: %v", err)
	}
	if got != origHash {
		t.Errorf("password_hash mutated on rollback: got %q, want %q", got, origHash)
	}
	if n := sqliteCountActive(t, db, userID); n != 2 {
		t.Errorf("expected 2 active sessions on rollback, got %d", n)
	}
}
