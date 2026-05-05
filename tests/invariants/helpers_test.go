//go:build integration

package invariants

import (
	"context"
	"database/sql"
	"os"
	"reflect"
	"sync"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"

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

		pg.InitSqlx(db)
		sharedDB = db
	})

	if sharedDBErr != nil {
		t.Skipf("test PG not available: %v", sharedDBErr)
	}
	return sharedDB
}

// seedAgent creates a minimal agent for FK satisfaction. v4 is single-tenant,
// so no tenant scaffolding is needed. Returns the agent UUID.
func seedAgent(t *testing.T, db *sql.DB) uuid.UUID {
	t.Helper()

	agentID := uuid.New()
	agentKey := "inv-" + agentID.String()[:8]

	_, err := db.Exec(
		`INSERT INTO agents (
			id, agent_key, display_name, owner_id, provider, model, status
		) VALUES ($1, $2, $3, $4, $5, $6, 'active')
		 ON CONFLICT DO NOTHING`,
		agentID, agentKey, "Test Agent "+agentKey, "test-owner", "test", "test-model")
	if err != nil {
		t.Fatalf("seed agent: %v", err)
	}

	t.Cleanup(func() {
		cleanupAgent(db, agentID)
	})

	return agentID
}

// seedTwoAgents creates 2 independent agents for isolation testing.
func seedTwoAgents(t *testing.T, db *sql.DB) (agentA, agentB uuid.UUID) {
	t.Helper()
	return seedAgent(t, db), seedAgent(t, db)
}

// emptyCtx returns a background context (v4 single-tenant, no scoping).
func emptyCtx() context.Context {
	return context.Background()
}

// userCtx returns a context with user ID set.
func userCtx(userID string) context.Context {
	return store.WithUserID(context.Background(), userID)
}

// agentCtx returns a context with the agent ID set.
func agentCtx(agentID uuid.UUID) context.Context {
	return store.WithAgentID(context.Background(), agentID)
}

// assertAccessDenied verifies that a cross-agent access returns nil or error.
// INVARIANT: Cross-agent access MUST NOT return data.
func assertAccessDenied(t *testing.T, result any, err error, msg string) {
	t.Helper()
	// Access denied can manifest as:
	// 1. Non-nil error (explicit denial)
	// 2. Nil result with nil error (no data found - implicit denial)
	// Use reflection to check for nil because interface{} with typed nil pointer is not nil.
	isNil := result == nil || (reflect.ValueOf(result).Kind() == reflect.Ptr && reflect.ValueOf(result).IsNil())
	if err == nil && !isNil {
		t.Errorf("INVARIANT VIOLATION: %s - expected nil or error, got data", msg)
	}
}

// assertNotEmpty verifies that the result is not nil/empty (valid access).
func assertNotEmpty(t *testing.T, result any, msg string) {
	t.Helper()
	if result == nil {
		t.Errorf("%s: expected non-nil result", msg)
	}
}

// cleanupAgent removes all agent-scoped data in FK order.
func cleanupAgent(db *sql.DB, agentID uuid.UUID) {
	// Knowledge stores
	db.Exec("DELETE FROM episodic_summaries WHERE agent_id = $1", agentID)
	db.Exec("DELETE FROM kg_relations WHERE agent_id = $1", agentID)
	db.Exec("DELETE FROM kg_entities WHERE agent_id = $1", agentID)
	db.Exec("DELETE FROM memory_chunks WHERE agent_id = $1", agentID)
	db.Exec("DELETE FROM memory_documents WHERE agent_id = $1", agentID)

	// Sessions
	db.Exec("DELETE FROM sessions WHERE agent_id = $1", agentID)

	// Agent-scoped permissions / shares / context
	db.Exec("DELETE FROM agent_config_permissions WHERE agent_id = $1", agentID)
	db.Exec("DELETE FROM agent_shares WHERE agent_id = $1", agentID)
	db.Exec("DELETE FROM agent_context_files WHERE agent_id = $1", agentID)
	db.Exec("DELETE FROM user_context_files WHERE agent_id = $1", agentID)
	db.Exec("DELETE FROM user_agent_overrides WHERE agent_id = $1", agentID)
	db.Exec("DELETE FROM agent_user_profiles WHERE agent_id = $1", agentID)
	db.Exec("DELETE FROM agent_evolution_suggestions WHERE agent_id = $1", agentID)
	db.Exec("DELETE FROM agent_evolution_metrics WHERE agent_id = $1", agentID)
	db.Exec("DELETE FROM agents WHERE id = $1", agentID)
}
