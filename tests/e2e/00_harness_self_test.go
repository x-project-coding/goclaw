//go:build e2e

// Package e2e_test is the v4 end-to-end harness self-test entry point.
// All e2e suites live under tests/e2e/ with build tag `e2e`.
package e2e_test

import (
	"context"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/tests/e2e/helpers"
)

// TestE2EEnvLoaded verifies env.e2e-tests/.env exposes the keys later phases need.
// Doesn't touch DB / network — pure config sanity.
func TestE2EEnvLoaded(t *testing.T) {
	t.Parallel()
	helpers.MustLoadEnv()

	for _, k := range []struct{ name, val string }{
		{"BAILIAN_API_KEY", helpers.BailianKey()},
		{"OPENROUTER_API_KEY", helpers.OpenRouterKey()},
		{"GOCLAW_DATABASE_URL", helpers.DatabaseURL()},
		{"GOCLAW_ENCRYPTION_KEY", helpers.EncryptionKey()},
		{"GOCLAW_JWT_SECRET", helpers.JWTSecret()},
		{"E2E_ROOT_EMAIL", helpers.RootEmail()},
		{"E2E_ROOT_PASSWORD", helpers.RootPassword()},
	} {
		if k.val == "" {
			t.Errorf("env %s empty — check env.e2e-tests/.env", k.name)
		}
	}
}

// TestPgConnect dials the e2e Postgres on port 5435 and pings it.
// Fails clearly if the dev-pgvector container isn't up.
func TestPgConnect(t *testing.T) {
	t.Parallel()
	db := helpers.MustDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("ping e2e PG: %v (is dev-pgvector up on :5435?)", err)
	}
}

// TestResetDB ensures TRUNCATE leaves zero rows in any seeded table that exists.
// Pre-Phase-03 the users/agents tables don't exist; ResetDB is a no-op then.
func TestResetDB(t *testing.T) {
	helpers.ResetDB(t) // not parallel — mutates global DB state.

	db := helpers.MustDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	for _, table := range []string{"agents"} {
		var count int
		row := db.QueryRowContext(ctx, "SELECT count(*) FROM "+table)
		if err := row.Scan(&count); err != nil {
			// Pre-Phase-03 the table doesn't exist — that's expected, skip.
			t.Logf("count %s skipped (table missing pre-Phase-03): %v", table, err)
			continue
		}
		if count != 0 {
			t.Errorf("%s expected 0 rows after ResetDB, got %d", table, count)
		}
	}
}
