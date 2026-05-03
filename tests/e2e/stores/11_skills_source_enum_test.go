//go:build e2e

package stores_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/tests/e2e/helpers"
)

// TestSkillSourceEnum verifies the skills.source CHECK constraint.
// Valid values: builtin, hub-verified, hub-unverified, agent-created, user-uploaded.
// Invalid values (e.g. "system") must be rejected by the DB.
// Structural assertion: skills table has NO is_system column.
func TestSkillSourceEnum(t *testing.T) {
	helpers.ResetDB(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	db := helpers.MustDB(t)

	validSources := []string{
		"builtin",
		"hub-verified",
		"hub-unverified",
		"agent-created",
		"user-uploaded",
	}

	for _, src := range validSources {
		id := uuid.Must(uuid.NewV7())
		slug := "skill-src-" + src + "-" + helpers.RandHex8()
		_, err := db.ExecContext(ctx, `
			INSERT INTO skills (id, name, slug, owner_id, file_path, source, created_at, updated_at)
			VALUES ($1, $2, $2, 'system', '/skills/x/SKILL.md', $3, now(), now())`,
			id, slug, src,
		)
		if err != nil {
			t.Errorf("source=%q: expected INSERT to succeed, got: %v", src, err)
		}
	}

	// "system" is not a valid source value — CHECK constraint must reject it.
	badID := uuid.Must(uuid.NewV7())
	badSlug := "skill-bad-src-" + helpers.RandHex8()
	_, err := db.ExecContext(ctx, `
		INSERT INTO skills (id, name, slug, owner_id, file_path, source, created_at, updated_at)
		VALUES ($1, $2, $2, 'system', '/skills/x/SKILL.md', 'system', now(), now())`,
		badID, badSlug,
	)
	if err == nil {
		t.Fatalf("source='system': expected CHECK violation, got nil error")
	}

	// Structural assertion: is_system column must not exist on skills table.
	var count int
	if err := db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = 'skills' AND column_name = 'is_system'`,
	).Scan(&count); err != nil {
		t.Fatalf("information_schema query: %v", err)
	}
	if count != 0 {
		t.Fatalf("skills.is_system column must not exist (count=%d)", count)
	}
}
