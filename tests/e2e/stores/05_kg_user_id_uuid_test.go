//go:build e2e

// PR-05D (R3 schema check): kg_entities.user_id is UUID NULL with FK to users(id).
// Confirms PG-level type enforcement: NULL OK, valid UUID OK, malformed string fails.
package stores_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
	"github.com/nextlevelbuilder/goclaw/tests/e2e/helpers"
)

// TestKGEntityUserIDNullable verifies the kg_entities.user_id column accepts
// NULL and valid UUIDs, and rejects malformed strings via PG's UUID type check.
// Catches drift if a future migration loosens the column type to TEXT.
func TestKGEntityUserIDNullable(t *testing.T) {
	helpers.ResetDB(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	db := helpers.MustDB(t)
	users := pg.NewPGUsersStore(db)
	agents := pg.NewPGAgentStore(db)

	owner := seedAgentOwner(t, ctx, users, "kg")
	ownerCtx := store.WithUserID(ctx, owner.ID.String())

	ag := buildAgent("ag-kg-"+helpers.RandHex8(), owner.ID)
	if err := agents.Create(ownerCtx, ag); err != nil {
		t.Fatalf("Create agent: %v", err)
	}

	t.Run("null_user_id_succeeds", func(t *testing.T) {
		_, err := db.ExecContext(ctx, `
			INSERT INTO kg_entities (agent_id, user_id, external_id, name, entity_type)
			VALUES ($1, NULL, $2, $3, $4)
		`, ag.ID, "ext-null-"+helpers.RandHex8(), "EntityNull", "Person")
		if err != nil {
			t.Fatalf("INSERT with NULL user_id failed: %v", err)
		}
	})

	t.Run("valid_uuid_succeeds", func(t *testing.T) {
		_, err := db.ExecContext(ctx, `
			INSERT INTO kg_entities (agent_id, user_id, external_id, name, entity_type)
			VALUES ($1, $2, $3, $4, $5)
		`, ag.ID, owner.ID, "ext-uuid-"+helpers.RandHex8(), "EntityUUID", "Person")
		if err != nil {
			t.Fatalf("INSERT with valid UUID user_id failed: %v", err)
		}
	})

	t.Run("malformed_user_id_fails", func(t *testing.T) {
		_, err := db.ExecContext(ctx, `
			INSERT INTO kg_entities (agent_id, user_id, external_id, name, entity_type)
			VALUES ($1, $2, $3, $4, $5)
		`, ag.ID, "not-a-uuid", "ext-bad-"+helpers.RandHex8(), "EntityBad", "Person")
		if err == nil {
			t.Fatalf("INSERT with malformed user_id should fail (PG UUID type check)")
		}
		// PG drivers may surface as `invalid input syntax for type uuid` or similar.
		msg := strings.ToLower(err.Error())
		if !strings.Contains(msg, "uuid") && !strings.Contains(msg, "invalid") {
			t.Errorf("expected UUID type error, got: %v", err)
		}
	})

	t.Run("random_uuid_no_fk_match_fails", func(t *testing.T) {
		// FK to users(id) — random UUID with no matching row must fail.
		_, err := db.ExecContext(ctx, `
			INSERT INTO kg_entities (agent_id, user_id, external_id, name, entity_type)
			VALUES ($1, $2, $3, $4, $5)
		`, ag.ID, uuid.New(), "ext-fk-"+helpers.RandHex8(), "EntityFK", "Person")
		if err == nil {
			t.Fatalf("INSERT with non-existent user_id should fail FK")
		}
		msg := strings.ToLower(err.Error())
		if !strings.Contains(msg, "foreign key") && !strings.Contains(msg, "violates") {
			t.Errorf("expected FK violation, got: %v", err)
		}
	})
}
