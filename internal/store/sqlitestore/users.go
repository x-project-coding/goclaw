//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// SQLiteUsersStore implements store.UsersStore on SQLite.
type SQLiteUsersStore struct {
	db *sql.DB
}

// NewSQLiteUsersStore returns a UsersStore backed by SQLite.
func NewSQLiteUsersStore(db *sql.DB) *SQLiteUsersStore {
	return &SQLiteUsersStore{db: db}
}

const usersSelectColumns = `id, email, display_name, password_hash, role, status,
	deleted_at, metadata, created_at, updated_at`

func (s *SQLiteUsersStore) Create(ctx context.Context, u *store.User) error {
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
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO users (id, email, display_name, password_hash, role, status, metadata)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		u.ID, u.Email, nilStr(deref(u.DisplayName)), u.PasswordHash, u.Role, u.Status, string(u.Metadata),
	)
	if err != nil {
		return fmt.Errorf("users insert: %w", err)
	}
	// Re-fetch so created_at/updated_at carry the DB-assigned defaults.
	got, err := s.Get(ctx, u.ID)
	if err != nil {
		return err
	}
	*u = *got
	return nil
}

func (s *SQLiteUsersStore) Get(ctx context.Context, id uuid.UUID) (*store.User, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+usersSelectColumns+` FROM users WHERE id = ?`, id)
	return scanSQLiteUser(row)
}

func (s *SQLiteUsersStore) GetByEmail(ctx context.Context, email string) (*store.User, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+usersSelectColumns+` FROM users WHERE email = ?`, email)
	return scanSQLiteUser(row)
}

func (s *SQLiteUsersStore) List(ctx context.Context, limit, offset int) ([]store.User, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+usersSelectColumns+`
		   FROM users
		  WHERE deleted_at IS NULL
		  ORDER BY created_at DESC
		  LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("users list: %w", err)
	}
	defer rows.Close()
	var out []store.User
	for rows.Next() {
		u, err := scanSQLiteUserRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *u)
	}
	return out, rows.Err()
}

func (s *SQLiteUsersStore) Update(ctx context.Context, id uuid.UUID, fields map[string]any) error {
	if len(fields) == 0 {
		return nil
	}
	return execMapUpdate(ctx, s.db, "users", id, fields)
}

func (s *SQLiteUsersStore) Delete(ctx context.Context, id uuid.UUID) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, id)
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

func scanSQLiteUser(row *sql.Row) (*store.User, error) {
	return scanSQLiteUserRow(row)
}

func scanSQLiteUserRow(r sqliteRowScanner) (*store.User, error) {
	var u store.User
	var displayName *string
	var deletedAt nullSqliteTime
	var metadata []byte
	createdAt, updatedAt := scanTimePair()
	err := r.Scan(
		&u.ID, &u.Email, &displayName, &u.PasswordHash, &u.Role, &u.Status,
		&deletedAt, &metadata, createdAt, updatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, store.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan user: %w", err)
	}
	u.DisplayName = displayName
	if deletedAt.Valid {
		t := deletedAt.Time
		u.DeletedAt = &t
	}
	u.Metadata = metadata
	u.CreatedAt = createdAt.Time
	u.UpdatedAt = updatedAt.Time
	return &u, nil
}

// deref unwraps *string for INSERT params, mirroring the PG store helper.
func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
