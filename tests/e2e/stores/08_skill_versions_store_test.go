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

// TestSkillVersionsStore exercises Create/List/GetActive/Delete.
//
// Schema has no `archived_at` column on skill_versions — "active" simply
// means `MAX(version)` per skill_id. This test follows the schema source-of-truth.
func TestSkillVersionsStore(t *testing.T) {
	helpers.ResetDB(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	db := helpers.MustDB(t)
	skillsStore := pg.NewPGSkillVersionsStore(db)

	// Seed parent skill row directly — SkillStore refactor lives elsewhere.
	skillID := uuid.Must(uuid.NewV7())
	skillName := "skill-" + helpers.RandHex8()
	if _, err := db.ExecContext(ctx, `
		INSERT INTO skills (id, name, slug, owner_id, file_path, created_at, updated_at)
		VALUES ($1, $2, $2, 'system', '/skills/x/SKILL.md', now(), now())`,
		skillID, skillName,
	); err != nil {
		t.Fatalf("seed skill: %v", err)
	}

	v1 := &store.SkillVersion{
		SkillID:     skillID,
		Version:     1,
		FileHash:    "sha256-aaaa",
		FilePath:    "/skills/x/v1/SKILL.md",
		FileSize:    1024,
		Frontmatter: json.RawMessage(`{"name":"x"}`),
		Changelog:   ptrStr("initial"),
	}
	if err := skillsStore.Create(ctx, v1); err != nil {
		t.Fatalf("Create v1: %v", err)
	}
	if v1.ID == uuid.Nil {
		t.Fatalf("Create v1: ID not populated")
	}

	v2 := &store.SkillVersion{
		SkillID:     skillID,
		Version:     2,
		FileHash:    "sha256-bbbb",
		FilePath:    "/skills/x/v2/SKILL.md",
		FileSize:    2048,
		Frontmatter: json.RawMessage(`{"name":"x"}`),
		Changelog:   ptrStr("v2 changes"),
	}
	if err := skillsStore.Create(ctx, v2); err != nil {
		t.Fatalf("Create v2: %v", err)
	}

	all, err := skillsStore.ListBySkillID(ctx, skillID)
	if err != nil {
		t.Fatalf("ListBySkillID: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("List: want 2 versions, got %d", len(all))
	}
	// Ordered DESC by version
	if all[0].Version != 2 || all[1].Version != 1 {
		t.Fatalf("List order: want [2,1], got [%d,%d]", all[0].Version, all[1].Version)
	}

	active, err := skillsStore.GetActive(ctx, skillID)
	if err != nil {
		t.Fatalf("GetActive: %v", err)
	}
	if active.Version != 2 {
		t.Fatalf("GetActive: want v2, got v%d", active.Version)
	}

	// Duplicate (skill_id, version) must fail UNIQUE.
	dup := &store.SkillVersion{
		SkillID:  skillID,
		Version:  2,
		FileHash: "sha256-cccc",
		FilePath: "/x",
	}
	if err := skillsStore.Create(ctx, dup); err == nil {
		t.Fatalf("Create dup version: want UNIQUE error, got nil")
	}

	if err := skillsStore.Delete(ctx, v1.ID); err != nil {
		t.Fatalf("Delete v1: %v", err)
	}
	all, _ = skillsStore.ListBySkillID(ctx, skillID)
	if len(all) != 1 {
		t.Fatalf("post-Delete List: want 1, got %d", len(all))
	}

	// Delete non-existent ID returns ErrNotFound.
	if err := skillsStore.Delete(ctx, uuid.Must(uuid.NewV7())); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Delete missing: want ErrNotFound, got %v", err)
	}
}
