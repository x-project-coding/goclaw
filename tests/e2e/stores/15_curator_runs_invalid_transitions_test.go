//go:build e2e

package stores_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
	"github.com/nextlevelbuilder/goclaw/tests/e2e/helpers"
)

// TestCuratorStateMachine covers invalid state transitions and the SQL-level
// CHECK constraint on curator_runs.status.
// Basic happy-path transitions (running→completed, running→failed) are
// covered by TestCuratorRunsStore in 09_curator_runs_store_test.go.
func TestCuratorStateMachine(t *testing.T) {
	helpers.ResetDB(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	db := helpers.MustDB(t)
	runs := pg.NewPGCuratorRunsStore(db)

	// Seed parent skill.
	skillID := uuid.Must(uuid.NewV7())
	if _, err := db.ExecContext(ctx, `
		INSERT INTO skills (id, name, slug, owner_id, file_path, created_at, updated_at)
		VALUES ($1, $2, $2, 'system', '/x/SKILL.md', now(), now())`,
		skillID, "skill-sm-"+helpers.RandHex8(),
	); err != nil {
		t.Fatalf("seed skill: %v", err)
	}

	// --- Double-Complete: Complete → Complete must fail. ---
	r1 := &store.CuratorRun{SkillID: ptrUUID(skillID), TriggeredBy: ptrStr("e2e-sm-1")}
	if err := runs.Start(ctx, r1); err != nil {
		t.Fatalf("Start r1: %v", err)
	}
	if err := runs.Complete(ctx, r1.ID, json.RawMessage(`{"ok":true}`)); err != nil {
		t.Fatalf("Complete r1: %v", err)
	}
	if err := runs.Complete(ctx, r1.ID, json.RawMessage(`{}`)); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Complete after Complete: want ErrNotFound, got %v", err)
	}

	// --- Fail → Complete must fail. ---
	r2 := &store.CuratorRun{SkillID: ptrUUID(skillID), TriggeredBy: ptrStr("e2e-sm-2")}
	if err := runs.Start(ctx, r2); err != nil {
		t.Fatalf("Start r2: %v", err)
	}
	if err := runs.Fail(ctx, r2.ID, "lint error"); err != nil {
		t.Fatalf("Fail r2: %v", err)
	}
	if err := runs.Complete(ctx, r2.ID, json.RawMessage(`{}`)); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Complete after Fail: want ErrNotFound, got %v", err)
	}

	// --- SQL CHECK constraint rejects invalid status values at DB level. ---
	badRunID := uuid.Must(uuid.NewV7())
	_, err := db.ExecContext(ctx, `
		INSERT INTO curator_runs (id, skill_id, status)
		VALUES ($1, $2, 'invalid_status')`,
		badRunID, skillID,
	)
	if err == nil {
		t.Fatalf("INSERT with status='invalid_status': expected CHECK violation, got nil")
	}
}
