//go:build e2e

package helpers

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
)

// User holds the seeded user data exposed to tests.
type User struct {
	ID          uuid.UUID
	Email       string
	DisplayName string
	Role        string
}

// Agent holds the seeded agent data exposed to tests.
type Agent struct {
	ID       uuid.UUID
	OwnerID  uuid.UUID
	AgentKey string
	Type     string
}

// LoginTokens is returned from LoginAs once auth endpoints land.
type LoginTokens struct {
	AccessToken  string
	RefreshToken string
	ExpiresIn    int
	UserID       uuid.UUID
}

// SeedUserOpts configures a seeded user. Email/Password use random suffix when empty (R5 fix).
type SeedUserOpts struct {
	Email       string
	Password    string // pre-hash; SeedUser will run through placeholder hash pre-P06.
	DisplayName string
	Role        string // root|admin|member|viewer
}

// SeedUser inserts a row into `users` (post-P03) or returns a placeholder pre-P03.
// Uses uuid.NewV7() per V4 lock (universal UUID v7).
func SeedUser(t *testing.T, opts SeedUserOpts) *User {
	t.Helper()
	if opts.Email == "" {
		opts.Email = RandEmail("u")
	}
	if opts.Password == "" {
		opts.Password = "test-password-" + RandHex8()
	}
	if opts.DisplayName == "" {
		opts.DisplayName = "Test " + RandHex8()
	}
	if opts.Role == "" {
		opts.Role = "member"
	}

	id, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("e2e: uuid.NewV7: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	db := MustDB(t)

	exists, err := tableExists(ctx, db, "users")
	if err != nil {
		t.Fatalf("e2e: tableExists users: %v", err)
	}
	if !exists {
		// Pre-Phase-03: return synthetic user so harness self-tests can assert UUID v7.
		return &User{ID: id, Email: opts.Email, DisplayName: opts.DisplayName, Role: opts.Role}
	}

	_, err = db.ExecContext(ctx, `
		INSERT INTO users (id, email, display_name, password_hash, role, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, now(), now())`,
		id, opts.Email, opts.DisplayName, placeholderPasswordHash(), opts.Role)
	if err != nil {
		t.Fatalf("e2e: insert user %s: %v", opts.Email, err)
	}
	return &User{ID: id, Email: opts.Email, DisplayName: opts.DisplayName, Role: opts.Role}
}

// SeedAgent inserts an agent row owned by the given user. Pre-P03 returns a placeholder.
func SeedAgent(t *testing.T, ownerID uuid.UUID, agentType string) *Agent {
	t.Helper()
	if agentType == "" {
		agentType = "open"
	}
	id, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("e2e: uuid.NewV7: %v", err)
	}
	agentKey := "agent-" + RandHex8()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	db := MustDB(t)

	exists, err := tableExists(ctx, db, "agents")
	if err != nil {
		t.Fatalf("e2e: tableExists agents: %v", err)
	}
	if !exists {
		return &Agent{ID: id, OwnerID: ownerID, AgentKey: agentKey, Type: agentType}
	}

	_, err = db.ExecContext(ctx, `
		INSERT INTO agents (id, owner_id, owner_user_id, agent_key, model, created_at, updated_at)
		VALUES ($1, $2, $3, $4, 'test/test-model', now(), now())`,
		id, ownerID.String(), ownerID, agentKey)
	if err != nil {
		t.Fatalf("e2e: insert agent %s: %v", agentKey, err)
	}
	return &Agent{ID: id, OwnerID: ownerID, AgentKey: agentKey, Type: agentType}
}

// RandHex8 returns 8 hex chars from crypto/rand.
// Used for parallel-safe random suffix per R5 (fixture isolation).
func RandHex8() string {
	var buf [4]byte
	if _, err := rand.Read(buf[:]); err != nil {
		// crypto/rand failure on linux/darwin is unrecoverable — fall back to ns timestamp
		return fmt.Sprintf("%08x", time.Now().UnixNano()&0xFFFFFFFF)
	}
	return hex.EncodeToString(buf[:])
}

// RandEmail returns a unique-per-call email under the e2e test prefix.
func RandEmail(prefix string) string {
	if prefix == "" {
		prefix = "u"
	}
	return fmt.Sprintf("%s-%s-%s@%s.test", TestPrefix(), prefix, RandHex8(), TestPrefix())
}

// LoginAs returns access+refresh tokens for the given credentials.
// Currently unimplemented — skips clearly to avoid silent test passes until
// the auth login/refresh endpoints land.
func LoginAs(t *testing.T, email, password string) *LoginTokens {
	t.Helper()
	t.Skipf("e2e: LoginAs requires auth endpoints (login/refresh) — not yet shipped")
	return nil
}
