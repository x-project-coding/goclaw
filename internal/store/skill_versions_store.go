package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// SkillVersion mirrors a row of `skill_versions`.
//
// Schema has UNIQUE(skill_id, version); "active" version = MAX(version) per
// skill. There is no archived_at column — version lifecycle at the store layer
// is just Create / Delete. Higher-level archival semantics belong to the
// skills domain layer that owns the Skill catalog.
type SkillVersion struct {
	ID          uuid.UUID       `db:"id"`
	SkillID     uuid.UUID       `db:"skill_id"`
	Version     int             `db:"version"`
	FileHash    string          `db:"file_hash"`
	FilePath    string          `db:"file_path"`
	FileSize    int64           `db:"file_size"`
	Frontmatter json.RawMessage `db:"frontmatter"`
	Changelog   *string         `db:"changelog"`
	PublishedBy *string         `db:"published_by"`
	CreatedAt   time.Time       `db:"created_at"`
}

// SkillVersionsStore manages immutable skill version snapshots.
type SkillVersionsStore interface {
	Create(ctx context.Context, v *SkillVersion) error
	ListBySkillID(ctx context.Context, skillID uuid.UUID) ([]SkillVersion, error)
	GetActive(ctx context.Context, skillID uuid.UUID) (*SkillVersion, error)
	Delete(ctx context.Context, id uuid.UUID) error
}
