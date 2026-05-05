//go:build integration

package integration

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
)

// ============================================================
// PostgreSQL constraint tests — users slug identity columns
// ============================================================

// TestPGUserKeyUnique: duplicate user_key must fail with PG error 23505.
func TestPGUserKeyUnique(t *testing.T) {
	db := testDB(t)
	key := "ukey-" + uuid.New().String()[:8]

	insert := func(email string) error {
		_, err := db.ExecContext(context.Background(),
			`INSERT INTO users (id, email, password_hash, role, status, user_key, kind)
			 VALUES ($1, $2, 'hash', 'member', 'active', $3, 'human')`,
			uuid.New(), email, key)
		return err
	}

	if err := insert("first-" + key + "@example.com"); err != nil {
		t.Fatalf("first insert: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM users WHERE user_key = $1", key) })

	err := insert("second-" + key + "@example.com")
	if err == nil {
		t.Fatal("expected unique violation, got nil")
	}
	if !strings.Contains(err.Error(), "23505") {
		t.Errorf("want pg 23505, got: %v", err)
	}
}

// TestPGUserKindCheck: invalid kind value must fail with PG error 23514.
func TestPGUserKindCheck(t *testing.T) {
	db := testDB(t)
	_, err := db.ExecContext(context.Background(),
		`INSERT INTO users (id, email, password_hash, role, status, user_key, kind)
		 VALUES ($1, $2, 'hash', 'member', 'active', $3, 'bot')`,
		uuid.New(),
		"bot-"+uuid.New().String()[:8]+"@example.com",
		"bot-"+uuid.New().String()[:8])
	if err == nil {
		t.Fatal("expected check violation, got nil")
	}
	if !strings.Contains(err.Error(), "23514") {
		t.Errorf("want pg 23514, got: %v", err)
	}
}

// TestPGUserChannelTypeShapeHumanWithChannelType: kind='human' + channel_type set → 23514.
func TestPGUserChannelTypeShapeHumanWithChannelType(t *testing.T) {
	db := testDB(t)
	_, err := db.ExecContext(context.Background(),
		`INSERT INTO users (id, email, password_hash, role, status, user_key, kind, channel_type)
		 VALUES ($1, $2, 'hash', 'member', 'active', $3, 'human', 'telegram')`,
		uuid.New(),
		"hct-"+uuid.New().String()[:8]+"@example.com",
		"hct-"+uuid.New().String()[:8])
	if err == nil {
		t.Fatal("expected shape constraint violation, got nil")
	}
	if !strings.Contains(err.Error(), "23514") {
		t.Errorf("want pg 23514 (shape), got: %v", err)
	}
}

// TestPGUserChannelTypeShapeChannelWithoutChannelType: kind='channel' + NULL → 23514.
func TestPGUserChannelTypeShapeChannelWithoutChannelType(t *testing.T) {
	db := testDB(t)
	_, err := db.ExecContext(context.Background(),
		`INSERT INTO users (id, email, password_hash, role, status, user_key, kind)
		 VALUES ($1, $2, 'hash', 'member', 'active', $3, 'channel')`,
		uuid.New(),
		"cnull-"+uuid.New().String()[:8]+"@example.com",
		"cnull-"+uuid.New().String()[:8])
	if err == nil {
		t.Fatal("expected shape constraint violation, got nil")
	}
	if !strings.Contains(err.Error(), "23514") {
		t.Errorf("want pg 23514 (shape), got: %v", err)
	}
}

// TestPGUserChannelTypeShapeValidPairs: (human,NULL) and (channel,'telegram') must insert OK.
func TestPGUserChannelTypeShapeValidPairs(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	uid1 := uuid.New()
	_, err := db.ExecContext(ctx,
		`INSERT INTO users (id, email, password_hash, role, status, user_key, kind)
		 VALUES ($1, $2, 'hash', 'member', 'active', $3, 'human')`,
		uid1, "vhuman-"+uid1.String()[:8]+"@example.com", "vhuman-"+uid1.String()[:8])
	if err != nil {
		t.Fatalf("(human,NULL) insert failed: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM users WHERE id = $1", uid1) })

	uid2 := uuid.New()
	_, err = db.ExecContext(ctx,
		`INSERT INTO users (id, email, password_hash, role, status, user_key, kind, channel_type)
		 VALUES ($1, $2, 'hash', 'member', 'active', $3, 'channel', 'telegram')`,
		uid2, "vchan-"+uid2.String()[:8]+"@example.com", "vchan-"+uid2.String()[:8])
	if err != nil {
		t.Fatalf("(channel,telegram) insert failed: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM users WHERE id = $1", uid2) })
}

// TestPGTeamKeyUnique: duplicate team_key must fail with PG error 23505.
func TestPGTeamKeyUnique(t *testing.T) {
	db := testDB(t)
	_, agentID := seedTenantAgent(t, db)

	key := "tkey-" + uuid.New().String()[:8]
	insert := func(suffix string) error {
		_, err := db.ExecContext(context.Background(),
			`INSERT INTO agent_teams (id, name, lead_agent_id, status, settings, created_by, team_key)
			 VALUES ($1, $2, $3, 'active', '{}', 'test', $4)`,
			uuid.New(), "Team-"+suffix, agentID, key)
		return err
	}

	if err := insert("A"); err != nil {
		t.Fatalf("first team insert: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM agent_teams WHERE team_key = $1", key) })

	err := insert("B")
	if err == nil {
		t.Fatal("expected unique violation, got nil")
	}
	if !strings.Contains(err.Error(), "23505") {
		t.Errorf("want pg 23505, got: %v", err)
	}
}

// ============================================================
// PG store CRUD parity tests
// ============================================================

// TestUserCreateGeneratesSlug: PG store Create() auto-populates user_key.
func TestUserCreateGeneratesSlug(t *testing.T) {
	db := testDB(t)
	s := pg.NewPGUsersStore(db)

	u := &store.User{
		Email:        "slugtest-" + uuid.New().String()[:8] + "@example.com",
		PasswordHash: "testhash",
		Role:         "member",
		Status:       "active",
	}
	if err := s.Create(context.Background(), u); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM users WHERE id = $1", u.ID) })

	if u.UserKey == "" {
		t.Error("user_key must be non-empty after Create")
	}
	if u.Kind != "human" {
		t.Errorf("kind default: got %q want %q", u.Kind, "human")
	}
	if u.ChannelType != nil {
		t.Errorf("channel_type must be nil for human, got %v", u.ChannelType)
	}
}

// TestUserSetKindChannelAtomic: PG SetKind atomically flips (kind, channel_type).
func TestUserSetKindChannelAtomic(t *testing.T) {
	db := testDB(t)
	s := pg.NewPGUsersStore(db)

	u := &store.User{
		Email:        "setkind-" + uuid.New().String()[:8] + "@example.com",
		PasswordHash: "testhash",
		Role:         "member",
		Status:       "active",
	}
	if err := s.Create(context.Background(), u); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM users WHERE id = $1", u.ID) })

	ct := "telegram"
	if err := s.SetKind(context.Background(), u.ID, "channel", &ct); err != nil {
		t.Fatalf("SetKind(channel, telegram): %v", err)
	}

	// Shape constraint must reject channel + NULL.
	if err := s.SetKind(context.Background(), u.ID, "channel", nil); err == nil {
		t.Error("SetKind(channel, nil) must fail shape constraint")
	}
}
