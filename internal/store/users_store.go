package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// User mirrors a row of the `users` table.
//
// Identity: ID is a UUID v7 generated DB-side (DEFAULT uuid_generate_v7()) on
// PG or by the Go layer (uuid.NewV7()) on SQLite — SQLite schema has no
// DEFAULT. The partial unique index `users_only_one_root` enforces at most one
// root user; bootstrap is responsible for honoring that invariant. The store
// layer treats `role` as opaque text.
type User struct {
	ID           uuid.UUID       `db:"id"`
	Email        string          `db:"email"`
	DisplayName  *string         `db:"display_name"`
	PasswordHash string          `db:"password_hash"`
	Role         string          `db:"role"`   // root|admin|member|viewer
	Status       string          `db:"status"` // active|suspended|...
	DeletedAt    *time.Time      `db:"deleted_at"`
	Metadata     json.RawMessage `db:"metadata"`
	CreatedAt    time.Time       `db:"created_at"`
	UpdatedAt    time.Time       `db:"updated_at"`
}

// UsersStore provides CRUD over the `users` table.
//
// PasswordHash is opaque text at this layer; the auth layer is responsible for
// enforcing the Argon2id format on write.
type UsersStore interface {
	Create(ctx context.Context, u *User) error
	Get(ctx context.Context, id uuid.UUID) (*User, error)
	GetByEmail(ctx context.Context, email string) (*User, error)
	List(ctx context.Context, limit, offset int) ([]User, error)
	Update(ctx context.Context, id uuid.UUID, fields map[string]any) error
	Delete(ctx context.Context, id uuid.UUID) error
}
