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

// TestSkillVersionArchive verifies the archive lifecycle:
// archived_at set, archive_path stored, content cleared,
// filtered list excludes archived by default, re-archive returns error.
func TestSkillVersionArchive(t *testing.T) {
	helpers.ResetDB(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	db := helpers.MustDB(t)
	sv := pg.NewPGSkillVersionsStore(db)

	// Seed parent skill.
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
		Content:     "skill content here",
		Frontmatter: json.RawMessage(`{"name":"x"}`),
		Changelog:   ptrStr("initial"),
	}
	if err := sv.Create(ctx, v1); err != nil {
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
		Content:     "v2 content",
		Frontmatter: json.RawMessage(`{"name":"x"}`),
		Changelog:   ptrStr("v2 changes"),
	}
	if err := sv.Create(ctx, v2); err != nil {
		t.Fatalf("Create v2: %v", err)
	}

	// Archive v1 — sets archived_at, archive_path, clears content.
	archivePath := "archives/skills/" + skillID.String() + "/v1/123.tar.gz"
	if err := sv.Archive(ctx, v1.ID, skillID, archivePath); err != nil {
		t.Fatalf("Archive v1: %v", err)
	}

	// Cross-skill archive must fail — bogus skillID guard rejects.
	bogus := uuid.Must(uuid.NewV7())
	if err := sv.Archive(ctx, v2.ID, bogus, archivePath+".cross"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("cross-skill archive: want ErrNotFound, got %v", err)
	}

	// Re-fetch via ListBySkillIDFiltered(includeArchived=true) to inspect v1 state.
	all, err := sv.ListBySkillIDFiltered(ctx, skillID, true)
	if err != nil {
		t.Fatalf("ListBySkillIDFiltered(true): %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("want 2 versions, got %d", len(all))
	}

	// Find v1 in results (ordered DESC by version, so v2 first).
	var archivedV1 *store.SkillVersion
	for i := range all {
		if all[i].Version == 1 {
			archivedV1 = &all[i]
		}
	}
	if archivedV1 == nil {
		t.Fatalf("v1 not found in full list")
	}
	if archivedV1.ArchivedAt == nil {
		t.Fatalf("v1.archived_at should be set after Archive()")
	}
	if archivedV1.ArchivePath == nil || *archivedV1.ArchivePath != archivePath {
		t.Fatalf("v1.archive_path: want %q, got %v", archivePath, archivedV1.ArchivePath)
	}
	if archivedV1.Content != "" {
		t.Fatalf("v1.content should be empty after Archive(), got %q", archivedV1.Content)
	}

	// Filtered list (includeArchived=false) must return only v2.
	active, err := sv.ListBySkillIDFiltered(ctx, skillID, false)
	if err != nil {
		t.Fatalf("ListBySkillIDFiltered(false): %v", err)
	}
	if len(active) != 1 {
		t.Fatalf("active versions: want 1, got %d", len(active))
	}
	if active[0].Version != 2 {
		t.Fatalf("active version: want 2, got %d", active[0].Version)
	}

	// Filtered list (includeArchived=true) must return both.
	withArchived, err := sv.ListBySkillIDFiltered(ctx, skillID, true)
	if err != nil {
		t.Fatalf("ListBySkillIDFiltered(true) second call: %v", err)
	}
	if len(withArchived) != 2 {
		t.Fatalf("with archived: want 2, got %d", len(withArchived))
	}

	// Re-archive already-archived v1 must return ErrNotFound (no row with archived_at IS NULL).
	if err := sv.Archive(ctx, v1.ID, skillID, archivePath+".dup"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("re-archive: want ErrNotFound, got %v", err)
	}
}
