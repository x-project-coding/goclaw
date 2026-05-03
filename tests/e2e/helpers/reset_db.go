//go:build e2e

package helpers

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/nextlevelbuilder/goclaw/internal/auth"
)

var (
	// rootHashOnce caches the hashed root password so repeated ResetDB calls
	// in a single process don't re-run Argon2id (m=64MB, t=3 — measurable).
	rootHashOnce sync.Once
	rootHashStr  string
	rootHashErr  error
)

var (
	// dbOnce gates DB() singleton across the test process.
	dbOnce sync.Once
	dbInst *sql.DB
	dbErr  error

	// systemTables stay populated across resets (migration metadata, extensions).
	systemTables = map[string]struct{}{
		"schema_migrations": {},
		"spatial_ref_sys":   {},
	}
)

// DB returns a singleton *sql.DB pointing at the e2e Postgres.
// Loads env.e2e-tests/.env if not already loaded. Pings before returning.
func DB() (*sql.DB, error) {
	dbOnce.Do(func() {
		if err := LoadEnv(); err != nil {
			dbErr = fmt.Errorf("load env: %w", err)
			return
		}
		db, err := sql.Open("pgx", DatabaseURL())
		if err != nil {
			dbErr = fmt.Errorf("sql.Open pgx: %w", err)
			return
		}
		db.SetMaxOpenConns(8)
		db.SetMaxIdleConns(2)
		db.SetConnMaxLifetime(30 * time.Minute)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := db.PingContext(ctx); err != nil {
			dbErr = fmt.Errorf("ping: %w", err)
			return
		}
		dbInst = db
	})
	return dbInst, dbErr
}

// MustDB panics on connection failure — convenient for tests.
func MustDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := DB()
	if err != nil {
		t.Fatalf("e2e: connect db: %v", err)
	}
	return db
}

// ResetDB truncates every public schema table (CASCADE) except schema_migrations,
// then re-seeds the root user from E2E_ROOT_EMAIL/E2E_ROOT_PASSWORD.
//
// Designed to run < 200ms on an empty DB. Calls discoverPublicTables once then
// issues a single TRUNCATE statement listing all targets.
func ResetDB(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	db := MustDB(t)
	tables, err := discoverPublicTables(ctx, db)
	if err != nil {
		t.Fatalf("e2e: discover tables: %v", err)
	}
	if len(tables) > 0 {
		stmt := "TRUNCATE TABLE " + strings.Join(quoteIdents(tables), ", ") + " RESTART IDENTITY CASCADE"
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("e2e: TRUNCATE failed: %v", err)
		}
	}
	if err := seedRootUser(ctx, db); err != nil {
		t.Fatalf("e2e: seed root user: %v", err)
	}
}

// discoverPublicTables lists every base table in schema=public minus systemTables.
// Returns names in alphabetic order for deterministic TRUNCATE statements.
func discoverPublicTables(ctx context.Context, db *sql.DB) ([]string, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT tablename FROM pg_catalog.pg_tables
		   WHERE schemaname = 'public'
		   ORDER BY tablename`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		if _, skip := systemTables[name]; skip {
			continue
		}
		out = append(out, name)
	}
	return out, rows.Err()
}

// quoteIdents wraps each identifier in double quotes for safe inclusion in DDL.
func quoteIdents(in []string) []string {
	out := make([]string, len(in))
	for i, s := range in {
		out[i] = `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
	}
	return out
}

// seedRootUser inserts the root user used by Phase 06 bootstrap tests.
//
// Phase 06+: hashes E2E_ROOT_PASSWORD with Argon2id so /v1/auth/login works
// against the seeded credentials. Hash is cached process-wide.
func seedRootUser(ctx context.Context, db *sql.DB) error {
	exists, err := tableExists(ctx, db, "users")
	if err != nil {
		return err
	}
	if !exists {
		// Pre-Phase-03: nothing to seed. Harness self-tests still pass.
		return nil
	}
	hash, err := rootPasswordHash()
	if err != nil {
		return fmt.Errorf("hash root password: %w", err)
	}
	_, err = db.ExecContext(ctx, `
		INSERT INTO users (id, email, display_name, password_hash, role, created_at, updated_at)
		VALUES (uuid_generate_v7(), $1, $2, $3, 'root', now(), now())
		ON CONFLICT (email) DO NOTHING`,
		RootEmail(), RootDisplayName(), hash)
	return err
}

// rootPasswordHash returns an Argon2id PHC-encoded hash of E2E_ROOT_PASSWORD.
// Cached because Argon2id is intentionally slow and the same value is re-used
// for every ResetDB call in a single test process.
func rootPasswordHash() (string, error) {
	rootHashOnce.Do(func() {
		rootHashStr, rootHashErr = auth.HashPassword(RootPassword())
	})
	return rootHashStr, rootHashErr
}

// placeholderPasswordHash returns a non-real PHC string for SeedUser fixtures
// that never need to authenticate via password (only DB-level user identity).
// Login attempts against this hash will fail VerifyPassword as intended.
func placeholderPasswordHash() string {
	return "argon2id$placeholder$test-fixture-no-login"
}

// tableExists reports whether `name` is a table in schema=public.
func tableExists(ctx context.Context, db *sql.DB, name string) (bool, error) {
	var present bool
	err := db.QueryRowContext(ctx,
		`SELECT EXISTS (
		   SELECT 1 FROM pg_catalog.pg_tables
		   WHERE schemaname = 'public' AND tablename = $1)`,
		name).Scan(&present)
	return present, err
}
