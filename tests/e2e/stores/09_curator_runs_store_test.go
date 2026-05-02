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

// TestCuratorRunsStore exercises Start → Complete and Start → Fail status transitions.
//
// Schema (curator_runs) has no separate events table; "events" are recorded
// cumulatively in the result JSONB. Plan prose mentioned "append events" —
// this test follows schema source-of-truth (single result snapshot per run).
func TestCuratorRunsStore(t *testing.T) {
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
		skillID, "skill-"+helpers.RandHex8(),
	); err != nil {
		t.Fatalf("seed skill: %v", err)
	}

	// Start a run — status='running', finished_at NULL.
	r := &store.CuratorRun{
		SkillID:     ptrUUID(skillID),
		TriggeredBy: ptrStr("e2e-test"),
	}
	if err := runs.Start(ctx, r); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if r.ID == uuid.Nil {
		t.Fatalf("Start: ID not populated")
	}
	if r.Status != "running" {
		t.Fatalf("Start: want status=running, got %q", r.Status)
	}

	// Complete with result JSONB.
	result := json.RawMessage(`{"events":[{"step":"compile","ok":true}],"summary":"ok"}`)
	if err := runs.Complete(ctx, r.ID, result); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	got, err := runs.Get(ctx, r.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != "completed" {
		t.Fatalf("Complete: want status=completed, got %q", got.Status)
	}
	if got.FinishedAt == nil {
		t.Fatalf("Complete: FinishedAt not set")
	}
	if len(got.Result) == 0 {
		t.Fatalf("Complete: Result empty")
	}

	// Second run — Fail path.
	r2 := &store.CuratorRun{SkillID: ptrUUID(skillID), TriggeredBy: ptrStr("e2e-fail")}
	if err := runs.Start(ctx, r2); err != nil {
		t.Fatalf("Start r2: %v", err)
	}
	if err := runs.Fail(ctx, r2.ID, "compile failed: missing import"); err != nil {
		t.Fatalf("Fail: %v", err)
	}
	got, _ = runs.Get(ctx, r2.ID)
	if got.Status != "failed" {
		t.Fatalf("Fail: want status=failed, got %q", got.Status)
	}
	if got.Error == nil || *got.Error == "" {
		t.Fatalf("Fail: Error not persisted")
	}

	// ListBySkillID returns both runs ordered by started_at DESC.
	list, err := runs.ListBySkillID(ctx, skillID)
	if err != nil {
		t.Fatalf("ListBySkillID: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("ListBySkillID: want 2, got %d", len(list))
	}

	// Get unknown id → ErrNotFound.
	if _, err := runs.Get(ctx, uuid.Must(uuid.NewV7())); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Get unknown: want ErrNotFound, got %v", err)
	}

	// Terminal-state guard: Complete-after-Complete and Complete-after-Fail
	// must surface as ErrNotFound (matches no row with status='running').
	if err := runs.Complete(ctx, r.ID, json.RawMessage(`{}`)); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("double Complete: want ErrNotFound, got %v", err)
	}
	if err := runs.Complete(ctx, r2.ID, json.RawMessage(`{}`)); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Complete after Fail: want ErrNotFound, got %v", err)
	}
	if err := runs.Fail(ctx, r.ID, "late"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Fail after Complete: want ErrNotFound, got %v", err)
	}
}

func ptrUUID(u uuid.UUID) *uuid.UUID { return &u }

