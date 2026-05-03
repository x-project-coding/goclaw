//go:build e2e

package stores_test

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
	"github.com/nextlevelbuilder/goclaw/tests/e2e/helpers"
)

// TestCuratorEventsStore verifies Append / ListByRunID, FK rejection, and
// ON DELETE CASCADE when the parent curator_run is deleted.
func TestCuratorEventsStore(t *testing.T) {
	helpers.ResetDB(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	db := helpers.MustDB(t)
	runs := pg.NewPGCuratorRunsStore(db)
	events := pg.NewPGCuratorEventsStore(db)

	// Seed parent skill.
	skillID := uuid.Must(uuid.NewV7())
	if _, err := db.ExecContext(ctx, `
		INSERT INTO skills (id, name, slug, owner_id, file_path, created_at, updated_at)
		VALUES ($1, $2, $2, 'system', '/x/SKILL.md', now(), now())`,
		skillID, "skill-ev-"+helpers.RandHex8(),
	); err != nil {
		t.Fatalf("seed skill: %v", err)
	}

	// Start a curator run.
	run := &store.CuratorRun{SkillID: ptrUUID(skillID), TriggeredBy: ptrStr("e2e-events")}
	if err := runs.Start(ctx, run); err != nil {
		t.Fatalf("Start run: %v", err)
	}

	// Append 3 events of different types with JSON payloads.
	eventDefs := []struct {
		typ     string
		payload json.RawMessage
	}{
		{"lint_started", json.RawMessage(`{"file":"SKILL.md"}`)},
		{"lint_warn", json.RawMessage(`{"line":42,"msg":"unused import"}`)},
		{"lint_done", json.RawMessage(`{"errors":0,"warnings":1}`)},
	}

	var created []store.CuratorEvent
	for _, d := range eventDefs {
		e := &store.CuratorEvent{
			RunID:     run.ID,
			EventType: d.typ,
			Payload:   d.payload,
		}
		if err := events.Append(ctx, e); err != nil {
			t.Fatalf("Append %q: %v", d.typ, err)
		}
		if e.ID == uuid.Nil {
			t.Fatalf("Append %q: ID not populated", d.typ)
		}
		created = append(created, *e)
	}

	// ListByRunID must return 3 events ordered by created_at ASC.
	listed, err := events.ListByRunID(ctx, run.ID)
	if err != nil {
		t.Fatalf("ListByRunID: %v", err)
	}
	if len(listed) != 3 {
		t.Fatalf("ListByRunID: want 3 events, got %d", len(listed))
	}
	for i, want := range eventDefs {
		if listed[i].EventType != want.typ {
			t.Errorf("event[%d].event_type: want %q, got %q", i, want.typ, listed[i].EventType)
		}
		// Compare payloads as decoded maps to avoid key-ordering differences.
		var gotMap, wantMap map[string]any
		if err := json.Unmarshal(listed[i].Payload, &gotMap); err != nil {
			t.Errorf("event[%d]: unmarshal got payload: %v", i, err)
			continue
		}
		if err := json.Unmarshal(want.payload, &wantMap); err != nil {
			t.Errorf("event[%d]: unmarshal want payload: %v", i, err)
			continue
		}
		if !reflect.DeepEqual(gotMap, wantMap) {
			t.Errorf("event[%d].payload mismatch: want %v, got %v", i, wantMap, gotMap)
		}
	}

	// Append event for non-existent run_id → FK violation.
	fakeRunID := uuid.Must(uuid.NewV7())
	badEvent := &store.CuratorEvent{
		RunID:     fakeRunID,
		EventType: "orphan",
		Payload:   json.RawMessage(`{}`),
	}
	if err := events.Append(ctx, badEvent); err == nil {
		t.Fatalf("Append with non-existent run_id: expected FK error, got nil")
	}

	// Cascade: delete the curator_run → events must also be deleted.
	if _, err := db.ExecContext(ctx,
		`DELETE FROM curator_runs WHERE id = $1`, run.ID,
	); err != nil {
		t.Fatalf("DELETE curator_run: %v", err)
	}
	var count int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM curator_events WHERE run_id = $1`, run.ID,
	).Scan(&count); err != nil {
		t.Fatalf("count curator_events after run delete: %v", err)
	}
	if count != 0 {
		t.Fatalf("ON DELETE CASCADE: expected 0 events after run deleted, got %d", count)
	}
}
