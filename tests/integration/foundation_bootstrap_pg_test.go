//go:build integration

package integration

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
)

// TestFoundation_GreenfieldBootstrap_PG simulates a fresh PG deployment: run
// migrations then create the root user and assert identity shape is correct.
// The testDB helper applies migrations once via golang-migrate so this test
// exercises the real greenfield path without requiring a pre-seeded database.
func TestFoundation_GreenfieldBootstrap_PG(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	s := pg.NewPGUsersStore(db)

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
	t.Cleanup(func() { db.Exec("DELETE FROM users WHERE id = $1", u.ID) })

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

	// Metadata must be non-nil and valid JSON (at minimum an empty object).
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
