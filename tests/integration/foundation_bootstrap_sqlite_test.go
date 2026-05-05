//go:build sqliteonly && integration

package integration

import (
	"context"
	"database/sql"
	"testing"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/sqlitestore"
)

// newBootstrapSQLiteDB opens a fresh in-memory SQLite DB and applies the full
// v4 schema via EnsureSchema — equivalent to a greenfield desktop install.
func newBootstrapSQLiteDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite :memory:: %v", err)
	}
	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		db.Close()
		t.Fatalf("foreign_keys pragma: %v", err)
	}
	if err := sqlitestore.EnsureSchema(db); err != nil {
		db.Close()
		t.Fatalf("EnsureSchema: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// TestFoundation_GreenfieldBootstrap_SQLite simulates a fresh desktop (Lite)
// deployment: open :memory: SQLite, apply schema.sql, create root user, assert
// identity shape matches the PG bootstrap contract.
func TestFoundation_GreenfieldBootstrap_SQLite(t *testing.T) {
	db := newBootstrapSQLiteDB(t)
	ctx := context.Background()

	s := sqlitestore.NewSQLiteUsersStore(db)

	suffix := uuid.New().String()[:8]
	u := &store.User{
		Email:        "root-bootstrap-" + suffix + "@local",
		PasswordHash: "bcrypt-test-stub",
		Role:         "root",
		Status:       "active",
	}

	if err := s.Create(ctx, u); err != nil {
		t.Fatalf("Create root user: %v", err)
	}

	// UserKey must be auto-generated from email.
	if u.UserKey == "" {
		t.Error("UserKey must be non-empty after Create")
	}

	// Default identity kind for a new user is human.
	if u.Kind != "human" {
		t.Errorf("Kind = %q, want %q", u.Kind, "human")
	}

	// channel_type must be nil for human users.
	if u.ChannelType != nil {
		t.Errorf("ChannelType must be nil for human user, got %v", u.ChannelType)
	}

	// Metadata must be non-nil and valid JSON.
	if len(u.Metadata) == 0 {
		t.Error("Metadata must be non-nil after Create")
	}

	// Verify round-trip via Get.
	got, err := s.Get(ctx, u.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.UserKey != u.UserKey {
		t.Errorf("Get: UserKey = %q, want %q", got.UserKey, u.UserKey)
	}
	if got.Kind != "human" {
		t.Errorf("Get: Kind = %q, want %q", got.Kind, "human")
	}
}
