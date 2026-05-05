//go:build e2e

// PR-05B-1a (L1): agents store drops tenant_id columns to match v4 schema.
// AgentData no longer carries TenantID. Scoping shifts to owner_user_id (UUID FK to users).
// Privileged roles (owner/root/admin) bypass the owner filter; regular users see only own agents.
package stores_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
	"github.com/nextlevelbuilder/goclaw/tests/e2e/helpers"
)

// TestAgentCreateNoTenant verifies agents.go matches v4 schema (no tenant_id column).
// Asserts: Create works without TenantID, owner_user_id round-trips, AgentData has no TenantID field.
func TestAgentCreateNoTenant(t *testing.T) {
	helpers.ResetDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	db := helpers.MustDB(t)
	users := pg.NewPGUsersStore(db)
	agents := pg.NewPGAgentStore(db)

	owner := seedAgentOwner(t, ctx, users, "owner")
	ownerCtx := store.WithUserID(ctx, owner.ID.String())

	ag := buildAgent("ag-create-"+helpers.RandHex8(), owner.ID)
	if err := agents.Create(ownerCtx, ag); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if ag.ID == uuid.Nil {
		t.Fatalf("Create: ID not populated")
	}
	if ag.OwnerUserID == nil || *ag.OwnerUserID != owner.ID {
		t.Fatalf("Create: OwnerUserID not preserved (got %v want %v)", ag.OwnerUserID, owner.ID)
	}

	got, err := agents.GetByKey(ownerCtx, ag.AgentKey)
	if err != nil {
		t.Fatalf("GetByKey owner: %v", err)
	}
	if got.ID != ag.ID {
		t.Fatalf("GetByKey id mismatch: got %s want %s", got.ID, ag.ID)
	}
	if got.OwnerUserID == nil || *got.OwnerUserID != owner.ID {
		t.Fatalf("GetByKey: owner_user_id not round-tripped")
	}
}

// TestAgentScopedByOwnerUserID verifies regular users see only their own agents.
// User A creates an agent; user B's context returns ErrNotFound; admin bypass works.
func TestAgentScopedByOwnerUserID(t *testing.T) {
	helpers.ResetDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	db := helpers.MustDB(t)
	users := pg.NewPGUsersStore(db)
	agents := pg.NewPGAgentStore(db)

	userA := seedAgentOwner(t, ctx, users, "userA")
	userB := seedAgentOwner(t, ctx, users, "userB")

	ctxA := store.WithUserID(ctx, userA.ID.String())
	ctxB := store.WithUserID(ctx, userB.ID.String())

	ag := buildAgent("ag-scope-"+helpers.RandHex8(), userA.ID)
	if err := agents.Create(ctxA, ag); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// User A sees its own agent.
	if _, err := agents.GetByKey(ctxA, ag.AgentKey); err != nil {
		t.Fatalf("GetByKey owner: %v", err)
	}

	// User B does not.
	if _, err := agents.GetByKey(ctxB, ag.AgentKey); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("GetByKey foreign: want ErrNotFound, got %v", err)
	}

	// Admin role bypasses the owner_user_id filter.
	adminCtx := store.WithRole(ctxB, "admin")
	if _, err := agents.GetByKey(adminCtx, ag.AgentKey); err != nil {
		t.Fatalf("GetByKey admin bypass: %v", err)
	}

	// GetByID parallel: same scoping rules.
	if _, err := agents.GetByID(ctxA, ag.ID); err != nil {
		t.Fatalf("GetByID owner: %v", err)
	}
	if _, err := agents.GetByID(ctxB, ag.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("GetByID foreign: want ErrNotFound, got %v", err)
	}
}

// TestAgentNoTenantIDColumn verifies SQL queries do not reference the dropped tenant_id column.
// Smoke test: any tenant_id reference in agents.go would surface as a column-not-found error
// from PG (schema has no tenant_id). Exercises Create + List which previously embedded tenant_id.
func TestAgentNoTenantIDColumn(t *testing.T) {
	helpers.ResetDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	db := helpers.MustDB(t)
	users := pg.NewPGUsersStore(db)
	agents := pg.NewPGAgentStore(db)

	owner := seedAgentOwner(t, ctx, users, "owner")
	ownerCtx := store.WithUserID(ctx, owner.ID.String())

	ag := buildAgent("ag-cols-"+helpers.RandHex8(), owner.ID)
	if err := agents.Create(ownerCtx, ag); err != nil {
		t.Fatalf("Create: %v (schema-code mismatch on tenant_id?)", err)
	}

	// List uses scopeClause-less path now; admin sees all, owner sees own.
	adminCtx := store.WithRole(ownerCtx, "admin")
	list, err := agents.List(adminCtx, "")
	if err != nil {
		t.Fatalf("List admin: %v", err)
	}
	found := false
	for _, x := range list {
		if x.ID == ag.ID {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("List admin: created agent not present (n=%d)", len(list))
	}
}

// seedAgentOwner inserts a fresh user and returns it. Suffix disambiguates email.
func seedAgentOwner(t *testing.T, ctx context.Context, users *pg.PGUsersStore, suffix string) *store.User {
	t.Helper()
	u := &store.User{
		Email:        helpers.RandEmail("agt-" + suffix),
		PasswordHash: "argon2id$placeholder$pre-p06",
		Role:         "member",
		Status:       "active",
	}
	if err := users.Create(ctx, u); err != nil {
		t.Fatalf("seed user %s: %v", suffix, err)
	}
	return u
}

// buildAgent returns a minimal AgentData satisfying NOT NULL columns of v4 agents schema.
// agent_key is caller-supplied to keep tests parallel-safe (R5).
func buildAgent(agentKey string, ownerUserID uuid.UUID) *store.AgentData {
	a := &store.AgentData{
		AgentKey:    agentKey,
		DisplayName: "Test Agent " + agentKey,
		OwnerID:     ownerUserID.String(), // legacy string field; same UUID for now
		Provider:    "openrouter",
		Model:       "anthropic/claude-sonnet-4.6",
		Status:      store.AgentStatusActive,
	}
	uid := ownerUserID
	a.OwnerUserID = &uid
	return a
}
