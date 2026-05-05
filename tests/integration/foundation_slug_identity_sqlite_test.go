//go:build sqliteonly && integration

package integration

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/sqlitestore"
)

func newSlugTestSQLiteDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		t.Fatalf("foreign_keys pragma: %v", err)
	}
	if err := sqlitestore.EnsureSchema(db); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// TestSQLiteUserKeyUnique: duplicate user_key must fail with UNIQUE constraint.
func TestSQLiteUserKeyUnique(t *testing.T) {
	db := newSlugTestSQLiteDB(t)
	key := "ukey-" + uuid.New().String()[:8]

	insert := func(email string) error {
		_, err := db.ExecContext(context.Background(),
			`INSERT INTO users (id, email, password_hash, role, status, user_key, kind)
			 VALUES (?, ?, 'hash', 'member', 'active', ?, 'human')`,
			uuid.New().String(), email, key)
		return err
	}

	if err := insert("first-" + key + "@example.com"); err != nil {
		t.Fatalf("first insert: %v", err)
	}
	err := insert("second-" + key + "@example.com")
	if err == nil {
		t.Fatal("expected unique violation, got nil")
	}
	if !strings.Contains(strings.ToUpper(err.Error()), "UNIQUE") {
		t.Errorf("want UNIQUE violation, got: %v", err)
	}
}

// TestSQLiteUserKindCheck: invalid kind value must fail CHECK constraint.
func TestSQLiteUserKindCheck(t *testing.T) {
	db := newSlugTestSQLiteDB(t)
	_, err := db.ExecContext(context.Background(),
		`INSERT INTO users (id, email, password_hash, role, status, user_key, kind)
		 VALUES (?, ?, 'hash', 'member', 'active', ?, 'bot')`,
		uuid.New().String(),
		"bot-"+uuid.New().String()[:8]+"@example.com",
		"bot-"+uuid.New().String()[:8])
	if err == nil {
		t.Fatal("expected check violation, got nil")
	}
	if !strings.Contains(strings.ToUpper(err.Error()), "CHECK") {
		t.Errorf("want CHECK violation, got: %v", err)
	}
}

// TestSQLiteUserChannelTypeShapeHumanWithChannelType: kind='human' + channel_type set → CHECK fail.
func TestSQLiteUserChannelTypeShapeHumanWithChannelType(t *testing.T) {
	db := newSlugTestSQLiteDB(t)
	_, err := db.ExecContext(context.Background(),
		`INSERT INTO users (id, email, password_hash, role, status, user_key, kind, channel_type)
		 VALUES (?, ?, 'hash', 'member', 'active', ?, 'human', 'telegram')`,
		uuid.New().String(),
		"hct-"+uuid.New().String()[:8]+"@example.com",
		"hct-"+uuid.New().String()[:8])
	if err == nil {
		t.Fatal("expected shape constraint violation, got nil")
	}
	if !strings.Contains(strings.ToUpper(err.Error()), "CHECK") {
		t.Errorf("want CHECK violation (shape), got: %v", err)
	}
}

// TestSQLiteUserChannelTypeShapeChannelWithoutChannelType: kind='channel' + NULL → CHECK fail.
func TestSQLiteUserChannelTypeShapeChannelWithoutChannelType(t *testing.T) {
	db := newSlugTestSQLiteDB(t)
	_, err := db.ExecContext(context.Background(),
		`INSERT INTO users (id, email, password_hash, role, status, user_key, kind)
		 VALUES (?, ?, 'hash', 'member', 'active', ?, 'channel')`,
		uuid.New().String(),
		"cnull-"+uuid.New().String()[:8]+"@example.com",
		"cnull-"+uuid.New().String()[:8])
	if err == nil {
		t.Fatal("expected shape constraint violation, got nil")
	}
	if !strings.Contains(strings.ToUpper(err.Error()), "CHECK") {
		t.Errorf("want CHECK violation (shape), got: %v", err)
	}
}

// TestSQLiteUserChannelTypeShapeValidPairs: both valid combinations must insert OK.
func TestSQLiteUserChannelTypeShapeValidPairs(t *testing.T) {
	db := newSlugTestSQLiteDB(t)
	ctx := context.Background()

	uid1 := uuid.New().String()
	_, err := db.ExecContext(ctx,
		`INSERT INTO users (id, email, password_hash, role, status, user_key, kind)
		 VALUES (?, ?, 'hash', 'member', 'active', ?, 'human')`,
		uid1, "vhuman-"+uid1[:8]+"@example.com", "vhuman-"+uid1[:8])
	if err != nil {
		t.Fatalf("(human,NULL) insert failed: %v", err)
	}

	uid2 := uuid.New().String()
	_, err = db.ExecContext(ctx,
		`INSERT INTO users (id, email, password_hash, role, status, user_key, kind, channel_type)
		 VALUES (?, ?, 'hash', 'member', 'active', ?, 'channel', 'telegram')`,
		uid2, "vchan-"+uid2[:8]+"@example.com", "vchan-"+uid2[:8])
	if err != nil {
		t.Fatalf("(channel,telegram) insert failed: %v", err)
	}
}

// TestSQLiteTeamKeyUnique: duplicate team_key must fail with UNIQUE constraint.
func TestSQLiteTeamKeyUnique(t *testing.T) {
	db := newSlugTestSQLiteDB(t)
	ctx := context.Background()

	agentID := uuid.New().String()
	_, err := db.ExecContext(ctx,
		`INSERT INTO agents (id, agent_key, status, provider, model, owner_id)
		 VALUES (?, 'agent-slug-test', 'active', 'test', 'test-model', 'test-owner')`,
		agentID)
	if err != nil {
		t.Fatalf("seed agent: %v", err)
	}

	key := "tkey-" + uuid.New().String()[:8]
	insert := func(suffix string) error {
		_, e := db.ExecContext(ctx,
			`INSERT INTO agent_teams (id, name, lead_agent_id, status, settings, created_by, team_key)
			 VALUES (?, ?, ?, 'active', '{}', 'test', ?)`,
			uuid.New().String(), "Team-"+suffix, agentID, key)
		return e
	}

	if err := insert("A"); err != nil {
		t.Fatalf("first team insert: %v", err)
	}
	err = insert("B")
	if err == nil {
		t.Fatal("expected unique violation, got nil")
	}
	if !strings.Contains(strings.ToUpper(err.Error()), "UNIQUE") {
		t.Errorf("want UNIQUE violation, got: %v", err)
	}
}

// TestSQLiteUserCreateGeneratesSlug: SQLite store Create() auto-populates user_key.
func TestSQLiteUserCreateGeneratesSlug(t *testing.T) {
	db := newSlugTestSQLiteDB(t)
	s := sqlitestore.NewSQLiteUsersStore(db)

	u := &store.User{
		Email:        "slugtest-" + uuid.New().String()[:8] + "@example.com",
		PasswordHash: "testhash",
		Role:         "member",
		Status:       "active",
	}
	if err := s.Create(context.Background(), u); err != nil {
		t.Fatalf("Create: %v", err)
	}
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

// TestSQLiteUserSetKindChannelAtomic: SQLite SetKind atomically flips (kind, channel_type).
func TestSQLiteUserSetKindChannelAtomic(t *testing.T) {
	db := newSlugTestSQLiteDB(t)
	s := sqlitestore.NewSQLiteUsersStore(db)

	u := &store.User{
		Email:        "setkind-" + uuid.New().String()[:8] + "@example.com",
		PasswordHash: "testhash",
		Role:         "member",
		Status:       "active",
	}
	if err := s.Create(context.Background(), u); err != nil {
		t.Fatalf("Create: %v", err)
	}

	ct := "telegram"
	if err := s.SetKind(context.Background(), u.ID, "channel", &ct); err != nil {
		t.Fatalf("SetKind(channel, telegram): %v", err)
	}

	// Shape constraint must reject channel + NULL.
	if err := s.SetKind(context.Background(), u.ID, "channel", nil); err == nil {
		t.Error("SetKind(channel, nil) must fail shape constraint")
	}
}
