//go:build integration

// Package integration provides shared test helpers for integration tests
// against a real PostgreSQL instance with the v4 schema applied.
package integration

import (
	"context"
	"database/sql"
	"os"
	"sync"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/nextlevelbuilder/goclaw/internal/security"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
)

const defaultTestDSN = "postgres://postgres:test@localhost:5433/goclaw_test?sslmode=disable"

var (
	sharedDB     *sql.DB
	sharedDBOnce sync.Once
	sharedDBErr  error
)

// testDB connects to the test PG instance, runs migrations once, and returns
// a shared *sql.DB. Skips test if PG is unreachable.
func testDB(t *testing.T) *sql.DB {
	t.Helper()

	sharedDBOnce.Do(func() {
		dsn := os.Getenv("TEST_DATABASE_URL")
		if dsn == "" {
			dsn = defaultTestDSN
		}

		db, err := sql.Open("pgx", dsn)
		if err != nil {
			sharedDBErr = err
			return
		}
		if err := db.Ping(); err != nil {
			sharedDBErr = err
			return
		}

		m, err := migrate.New("file://../../migrations", dsn)
		if err != nil {
			sharedDBErr = err
			return
		}
		if err := m.Up(); err != nil && err != migrate.ErrNoChange {
			sharedDBErr = err
			return
		}
		m.Close()

		// Initialize pg package's sqlx wrapper. Without this, any store
		// method that uses pkgSqlxDB.SelectContext panics on nil deref if
		// the test runs before any other test happens to call InitSqlx.
		pg.InitSqlx(db)

		sharedDB = db
	})

	if sharedDBErr != nil {
		t.Skipf("test PG not available: %v", sharedDBErr)
	}
	return sharedDB
}

// seedTenantAgent retains its v3 name for caller compatibility but in v4
// only seeds an `agents` row. The first return value is a throwaway UUID
// that some callers still pass around as a "scope" suffix; v4 has no
// tenants table so it is generated fresh and not persisted.
//
// Returns (scopeID, agentID). Each call uses unique IDs.
func seedTenantAgent(t *testing.T, db *sql.DB) (scopeID, agentID uuid.UUID) {
	t.Helper()

	scopeID = uuid.New()
	agentID = uuid.New()
	agentKey := "test-" + agentID.String()[:8]

	_, err := db.Exec(
		`INSERT INTO agents (id, agent_key, agent_type, status, provider, model, owner_id)
		 VALUES ($1, $2, 'predefined', 'active', 'test', 'test-model', 'test-owner')
		 ON CONFLICT DO NOTHING`,
		agentID, agentKey)
	if err != nil {
		t.Fatalf("seed agent: %v", err)
	}

	t.Cleanup(func() {
		// Children (FK CASCADE handles most, but be explicit for tables that
		// SET NULL rather than CASCADE so rows don't survive past the test).
		db.Exec("DELETE FROM agent_team_members WHERE agent_id = $1", agentID)
		db.Exec("DELETE FROM agent_teams WHERE lead_agent_id = $1", agentID)
		db.Exec("DELETE FROM agent_evolution_suggestions WHERE agent_id = $1", agentID)
		db.Exec("DELETE FROM agent_evolution_metrics WHERE agent_id = $1", agentID)
		db.Exec("DELETE FROM agent_shares WHERE agent_id = $1", agentID)
		db.Exec("DELETE FROM agent_context_files WHERE agent_id = $1", agentID)
		db.Exec("DELETE FROM user_context_files WHERE agent_id = $1", agentID)
		db.Exec("DELETE FROM user_agent_overrides WHERE agent_id = $1", agentID)
		db.Exec("DELETE FROM user_agent_profiles WHERE agent_id = $1", agentID)
		db.Exec("DELETE FROM agents WHERE id = $1", agentID)
	})

	return scopeID, agentID
}

// tenantCtx is a v4 no-op kept for caller signature compatibility. v4 has
// no tenant scope; tests should rely on user-id context via userCtx for
// any per-user filtering they need.
func tenantCtx(_ uuid.UUID) context.Context {
	return context.Background()
}

// userCtx returns a context with the given user ID injected. v4 stores
// authorize per-user via WithUserID; pass the agent owner or an explicit
// member ID depending on the test's intent.
func userCtx(_ uuid.UUID, userID string) context.Context {
	return store.WithUserID(context.Background(), userID)
}

// crossTenantCtx is a v4 no-op kept for caller signature compatibility.
// v3 used a separate context to bypass tenant filtering; v4 has no
// tenant filtering, so a plain background context suffices.
func crossTenantCtx() context.Context {
	return context.Background()
}

func allowLoopbackForTest(t *testing.T) {
	t.Helper()
	security.SetAllowLoopbackForTest(true)
	t.Cleanup(func() {
		security.SetAllowLoopbackForTest(false)
	})
}

// testEncryptionKey is a fixed 32-byte key for stores that require
// AES-256-GCM encryption in tests.
const testEncryptionKey = "0123456789abcdef0123456789abcdef"

// strPtr returns a pointer to s. Convenience helper for the many
// *string fields on test fixtures (nullable columns) where callers want
// to spell the literal inline rather than via a temporary variable.
func strPtr(s string) *string { return &s }
