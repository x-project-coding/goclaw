//go:build e2e

package stores_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
	"github.com/nextlevelbuilder/goclaw/tests/e2e/helpers"
)

// TestSkillSidecarMetadata verifies sidecar column updates:
// MarkSkillUsed increments usage_count and sets last_used_at,
// MarkSkillViewed sets last_viewed_at,
// PinSkill toggles the pinned flag and it survives an UpdateSkill call.
func TestSkillSidecarMetadata(t *testing.T) {
	helpers.ResetDB(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	db := helpers.MustDB(t)
	ss := pg.NewPGSkillStore(db, "/tmp/skills")

	// Seed a skill directly via SQL (CreateSkill is a thin wrapper without source).
	skillID := uuid.Must(uuid.NewV7())
	slug := "sidecar-" + helpers.RandHex8()
	if _, err := db.ExecContext(ctx, `
		INSERT INTO skills (id, name, slug, owner_id, file_path, source, created_at, updated_at)
		VALUES ($1, $2, $2, 'system', '/skills/x/SKILL.md', 'user-uploaded', now(), now())`,
		skillID, slug,
	); err != nil {
		t.Fatalf("seed skill: %v", err)
	}

	// MarkSkillUsed once → last_used_at IS NOT NULL, usage_count = 1.
	if err := ss.MarkSkillUsed(ctx, skillID); err != nil {
		t.Fatalf("MarkSkillUsed #1: %v", err)
	}
	var lastUsed *time.Time
	var usageCount int64
	if err := db.QueryRowContext(ctx,
		`SELECT last_used_at, usage_count FROM skills WHERE id = $1`, skillID,
	).Scan(&lastUsed, &usageCount); err != nil {
		t.Fatalf("scan after MarkSkillUsed #1: %v", err)
	}
	if lastUsed == nil {
		t.Fatalf("last_used_at should be set after first MarkSkillUsed")
	}
	if usageCount != 1 {
		t.Fatalf("usage_count: want 1, got %d", usageCount)
	}

	// MarkSkillUsed again → usage_count = 2.
	if err := ss.MarkSkillUsed(ctx, skillID); err != nil {
		t.Fatalf("MarkSkillUsed #2: %v", err)
	}
	if err := db.QueryRowContext(ctx,
		`SELECT usage_count FROM skills WHERE id = $1`, skillID,
	).Scan(&usageCount); err != nil {
		t.Fatalf("scan after MarkSkillUsed #2: %v", err)
	}
	if usageCount != 2 {
		t.Fatalf("usage_count: want 2 after second call, got %d", usageCount)
	}

	// MarkSkillViewed → last_viewed_at IS NOT NULL.
	if err := ss.MarkSkillViewed(ctx, skillID); err != nil {
		t.Fatalf("MarkSkillViewed: %v", err)
	}
	var lastViewed *time.Time
	if err := db.QueryRowContext(ctx,
		`SELECT last_viewed_at FROM skills WHERE id = $1`, skillID,
	).Scan(&lastViewed); err != nil {
		t.Fatalf("scan after MarkSkillViewed: %v", err)
	}
	if lastViewed == nil {
		t.Fatalf("last_viewed_at should be set after MarkSkillViewed")
	}

	// PinSkill(true) → pinned = TRUE.
	if err := ss.PinSkill(ctx, skillID, true); err != nil {
		t.Fatalf("PinSkill(true): %v", err)
	}
	var pinned bool
	if err := db.QueryRowContext(ctx,
		`SELECT pinned FROM skills WHERE id = $1`, skillID,
	).Scan(&pinned); err != nil {
		t.Fatalf("scan pinned after PinSkill(true): %v", err)
	}
	if !pinned {
		t.Fatalf("pinned should be TRUE after PinSkill(true)")
	}

	// PinSkill(false) → pinned = FALSE.
	if err := ss.PinSkill(ctx, skillID, false); err != nil {
		t.Fatalf("PinSkill(false): %v", err)
	}
	if err := db.QueryRowContext(ctx,
		`SELECT pinned FROM skills WHERE id = $1`, skillID,
	).Scan(&pinned); err != nil {
		t.Fatalf("scan pinned after PinSkill(false): %v", err)
	}
	if pinned {
		t.Fatalf("pinned should be FALSE after PinSkill(false)")
	}

	// Pin it again, then call UpdateSkill with a description change.
	// Verify pinned survives the update (UpdateSkill must not reset it).
	if err := ss.PinSkill(ctx, skillID, true); err != nil {
		t.Fatalf("PinSkill(true) before update: %v", err)
	}
	newDesc := "updated description"
	if err := ss.UpdateSkill(ctx, skillID, map[string]any{"description": newDesc}); err != nil {
		t.Fatalf("UpdateSkill: %v", err)
	}
	var pinnedAfterUpdate bool
	var desc *string
	if err := db.QueryRowContext(ctx,
		`SELECT pinned, description FROM skills WHERE id = $1`, skillID,
	).Scan(&pinnedAfterUpdate, &desc); err != nil {
		t.Fatalf("scan after UpdateSkill: %v", err)
	}
	if !pinnedAfterUpdate {
		t.Fatalf("pinned should still be TRUE after UpdateSkill (pinned flag not in update map)")
	}
	if desc == nil || *desc != newDesc {
		t.Fatalf("description: want %q, got %v", newDesc, desc)
	}
}
