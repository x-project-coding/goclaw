package pg

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/identity"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// PGUsersStore implements store.UsersStore on PostgreSQL.
type PGUsersStore struct {
	db *sql.DB
}

// NewPGUsersStore returns a UsersStore backed by Postgres.
func NewPGUsersStore(db *sql.DB) *PGUsersStore {
	return &PGUsersStore{db: db}
}

const usersSelectColumns = `id, email, display_name, password_hash, role, status,
	deleted_at, metadata, user_key, kind, channel_type, created_at, updated_at`

func (s *PGUsersStore) Create(ctx context.Context, u *store.User) error {
	if u.ID == uuid.Nil {
		id, err := uuid.NewV7()
		if err != nil {
			return fmt.Errorf("uuid v7: %w", err)
		}
		u.ID = id
	}
	if len(u.Metadata) == 0 {
		u.Metadata = []byte("{}")
	}
	// Auto-generate slug from email when caller did not supply one.
	if u.UserKey == "" {
		u.UserKey = identity.SlugFromEmail(u.Email, u.ID.String()[:6])
	}
	// Default identity kind.
	if u.Kind == "" {
		u.Kind = "human"
	}
	row := s.db.QueryRowContext(ctx, `
		INSERT INTO users (id, email, display_name, password_hash, role, status, metadata, user_key, kind, channel_type)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING `+usersSelectColumns,
		u.ID, u.Email, nilStr(deref(u.DisplayName)), u.PasswordHash,
		u.Role, u.Status, u.Metadata, u.UserKey, u.Kind, u.ChannelType,
	)
	return scanUser(row, u)
}

func (s *PGUsersStore) Get(ctx context.Context, id uuid.UUID) (*store.User, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+usersSelectColumns+` FROM users WHERE id = $1`, id)
	var u store.User
	if err := scanUser(row, &u); err != nil {
		return nil, err
	}
	return &u, nil
}

func (s *PGUsersStore) GetByEmail(ctx context.Context, email string) (*store.User, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+usersSelectColumns+` FROM users WHERE email = $1`, email)
	var u store.User
	if err := scanUser(row, &u); err != nil {
		return nil, err
	}
	return &u, nil
}

func (s *PGUsersStore) List(ctx context.Context, limit, offset int) ([]store.User, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+usersSelectColumns+`
		   FROM users
		  WHERE deleted_at IS NULL
		  ORDER BY created_at DESC
		  LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("users list: %w", err)
	}
	defer rows.Close()
	var out []store.User
	for rows.Next() {
		var u store.User
		if err := scanUser(rows, &u); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s *PGUsersStore) Update(ctx context.Context, id uuid.UUID, fields map[string]any) error {
	if len(fields) == 0 {
		return nil
	}
	// Immutability: strip slug and identity columns from generic update path.
	// These are only set at creation (user_key) or via SetKind (kind, channel_type).
	delete(fields, "user_key")
	delete(fields, "kind")
	delete(fields, "channel_type")
	if len(fields) == 0 {
		return nil
	}
	return execMapUpdate(ctx, s.db, "users", id, fields)
}

func (s *PGUsersStore) Delete(ctx context.Context, id uuid.UUID) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM users WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("users delete: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return store.ErrNotFound
	}
	return nil
}

// SetKind atomically updates (kind, channel_type) in a single statement.
// The DB shape constraint enforces coherence — an invalid pair is rejected
// before the transaction commits.
func (s *PGUsersStore) SetKind(ctx context.Context, id uuid.UUID, kind string, channelType *string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE users SET kind = $1, channel_type = $2, updated_at = NOW() WHERE id = $3`,
		kind, channelType, id)
	if err != nil {
		return fmt.Errorf("set kind: %w", err)
	}
	return nil
}

// rowScanner unifies *sql.Row and *sql.Rows scanning.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanUser(r rowScanner, u *store.User) error {
	var displayName *string
	err := r.Scan(
		&u.ID, &u.Email, &displayName, &u.PasswordHash, &u.Role, &u.Status,
		&u.DeletedAt, &u.Metadata, &u.UserKey, &u.Kind, &u.ChannelType,
		&u.CreatedAt, &u.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return store.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("scan user: %w", err)
	}
	u.DisplayName = displayName
	return nil
}

// deref unwraps *string for INSERT param. Returns "" when nil so the SQL driver
// receives a value; nilStr() then converts "" → SQL NULL for the optional column.
func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
