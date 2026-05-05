//go:build sqliteonly && integration

// Continuation of the 13-entity metadata parity sweep.
// Covers: channel_instances, mcp_servers, cron_jobs, llm_providers,
// system_configs, user_sessions.
package integration

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestFoundation_StoreMetadataParity_B verifies metadata round-trips on both
// PG and SQLite stores for: channel_instances, mcp_servers, cron_jobs,
// llm_providers, system_configs, user_sessions.
func TestFoundation_StoreMetadataParity_B(t *testing.T) {
	pgDB := testDB(t)
	sqliteDB := newSweepSQLiteDB(t)
	ctx := context.Background()

	// Seed a minimal agent in both DBs for FK dependencies.
	pgAgentID := uuid.New()
	pgAgentKey := "pb-" + pgAgentID.String()[:8]
	if _, err := pgDB.ExecContext(ctx,
		`INSERT INTO agents (id, agent_key, status, provider, model, owner_id)
		 VALUES ($1,$2,'active','test','m','owner')`,
		pgAgentID, pgAgentKey); err != nil {
		t.Fatalf("PG seed agent: %v", err)
	}
	t.Cleanup(func() {
		pgDB.Exec("DELETE FROM channel_instances WHERE agent_id = $1", pgAgentID)
		pgDB.Exec("DELETE FROM agents WHERE id = $1", pgAgentID)
	})

	sqlAgentID := uuid.New()
	sqlAgentKey := "sb-" + sqlAgentID.String()[:8]
	if _, err := sqliteDB.ExecContext(ctx,
		`INSERT INTO agents (id, agent_key, status, provider, model, owner_id)
		 VALUES (?,?,'active','test','m','owner')`,
		sqlAgentID.String(), sqlAgentKey); err != nil {
		t.Fatalf("SQLite seed agent: %v", err)
	}

	t.Run("channel_instances", func(t *testing.T) {
		pgID := uuid.New()
		pgName := "ch-" + pgID.String()[:8]
		if _, err := pgDB.ExecContext(ctx,
			`INSERT INTO channel_instances (id, name, channel_type, agent_id, enabled, metadata)
			 VALUES ($1,$2,'telegram',$3,true,$4::jsonb)`,
			pgID, pgName, pgAgentID, paritySweepMetaJSON); err != nil {
			t.Fatalf("PG channel_instances: %v", err)
		}
		t.Cleanup(func() { pgDB.Exec("DELETE FROM channel_instances WHERE id = $1", pgID) })

		sqlID := uuid.New()
		sqlName := "sch-" + sqlID.String()[:8]
		if _, err := sqliteDB.ExecContext(ctx,
			`INSERT INTO channel_instances (id, name, channel_type, agent_id, enabled, metadata)
			 VALUES (?,?,'telegram',?,1,?)`,
			sqlID.String(), sqlName, sqlAgentID.String(), paritySweepMetaJSON); err != nil {
			t.Fatalf("SQLite channel_instances: %v", err)
		}
		assertPGMeta(t, "channel_instances", "channel_instances", "id", pgID, pgDB)
		assertSQLiteMeta(t, "channel_instances", "channel_instances", "id", sqlID.String(), sqliteDB)
	})

	t.Run("mcp_servers", func(t *testing.T) {
		pgID := uuid.New()
		pgName := "mcp-" + pgID.String()[:8]
		if _, err := pgDB.ExecContext(ctx,
			`INSERT INTO mcp_servers (id, name, transport, created_by, metadata)
			 VALUES ($1,$2,'stdio','test-owner',$3::jsonb)`,
			pgID, pgName, paritySweepMetaJSON); err != nil {
			t.Fatalf("PG mcp_servers: %v", err)
		}
		t.Cleanup(func() { pgDB.Exec("DELETE FROM mcp_servers WHERE id = $1", pgID) })

		sqlID := uuid.New()
		sqlName := "smcp-" + sqlID.String()[:8]
		if _, err := sqliteDB.ExecContext(ctx,
			`INSERT INTO mcp_servers (id, name, transport, created_by, metadata)
			 VALUES (?,?,'stdio','test-owner',?)`,
			sqlID.String(), sqlName, paritySweepMetaJSON); err != nil {
			t.Fatalf("SQLite mcp_servers: %v", err)
		}
		assertPGMeta(t, "mcp_servers", "mcp_servers", "id", pgID, pgDB)
		assertSQLiteMeta(t, "mcp_servers", "mcp_servers", "id", sqlID.String(), sqliteDB)
	})

	t.Run("cron_jobs", func(t *testing.T) {
		pgID := uuid.New()
		if _, err := pgDB.ExecContext(ctx,
			`INSERT INTO cron_jobs (id, name, schedule_kind, payload, metadata)
			 VALUES ($1,$2,'every','{}',$3::jsonb)`,
			pgID, "cron-"+pgID.String()[:8], paritySweepMetaJSON); err != nil {
			t.Fatalf("PG cron_jobs: %v", err)
		}
		t.Cleanup(func() { pgDB.Exec("DELETE FROM cron_jobs WHERE id = $1", pgID) })

		sqlID := uuid.New()
		if _, err := sqliteDB.ExecContext(ctx,
			`INSERT INTO cron_jobs (id, name, schedule_kind, payload, metadata)
			 VALUES (?,?,'every','{}',?)`,
			sqlID.String(), "cron-"+sqlID.String()[:8], paritySweepMetaJSON); err != nil {
			t.Fatalf("SQLite cron_jobs: %v", err)
		}
		assertPGMeta(t, "cron_jobs", "cron_jobs", "id", pgID, pgDB)
		assertSQLiteMeta(t, "cron_jobs", "cron_jobs", "id", sqlID.String(), sqliteDB)
	})

	t.Run("llm_providers", func(t *testing.T) {
		pgID := uuid.New()
		pgProvName := "prov-" + pgID.String()[:8]
		if _, err := pgDB.ExecContext(ctx,
			`INSERT INTO llm_providers (id, name, provider_type, metadata)
			 VALUES ($1,$2,'openai_compat',$3::jsonb)`,
			pgID, pgProvName, paritySweepMetaJSON); err != nil {
			t.Fatalf("PG llm_providers: %v", err)
		}
		t.Cleanup(func() { pgDB.Exec("DELETE FROM llm_providers WHERE id = $1", pgID) })

		sqlID := uuid.New()
		sqlProvName := "sprov-" + sqlID.String()[:8]
		if _, err := sqliteDB.ExecContext(ctx,
			`INSERT INTO llm_providers (id, name, provider_type, metadata)
			 VALUES (?,?,'openai_compat',?)`,
			sqlID.String(), sqlProvName, paritySweepMetaJSON); err != nil {
			t.Fatalf("SQLite llm_providers: %v", err)
		}
		assertPGMeta(t, "llm_providers", "llm_providers", "id", pgID, pgDB)
		assertSQLiteMeta(t, "llm_providers", "llm_providers", "id", sqlID.String(), sqliteDB)
	})

	t.Run("system_configs", func(t *testing.T) {
		pgKey := "meta-parity-pg-" + uuid.New().String()[:8]
		if _, err := pgDB.ExecContext(ctx,
			`INSERT INTO system_configs (key, value, metadata) VALUES ($1,'v',$2::jsonb)`,
			pgKey, paritySweepMetaJSON); err != nil {
			t.Fatalf("PG system_configs: %v", err)
		}
		t.Cleanup(func() { pgDB.Exec("DELETE FROM system_configs WHERE key = $1", pgKey) })

		sqlKey := "meta-parity-sq-" + uuid.New().String()[:8]
		if _, err := sqliteDB.ExecContext(ctx,
			`INSERT INTO system_configs (key, value, metadata) VALUES (?,?,?)`,
			sqlKey, "v", paritySweepMetaJSON); err != nil {
			t.Fatalf("SQLite system_configs: %v", err)
		}
		assertPGMeta(t, "system_configs", "system_configs", "key", pgKey, pgDB)
		assertSQLiteMeta(t, "system_configs", "system_configs", "key", sqlKey, sqliteDB)
	})

	t.Run("user_sessions", func(t *testing.T) {
		pgUID := uuid.New()
		pgUKey := "pus-" + pgUID.String()[:8]
		pgDB.ExecContext(ctx,
			`INSERT INTO users (id, email, password_hash, role, status, user_key, kind)
			 VALUES ($1,$2,'h','member','active',$3,'human')`,
			pgUID, pgUKey+"@x.com", pgUKey)
		t.Cleanup(func() {
			pgDB.Exec("DELETE FROM user_sessions WHERE user_id = $1", pgUID)
			pgDB.Exec("DELETE FROM users WHERE id = $1", pgUID)
		})

		sqlUID := uuid.New()
		sqlUKey := "sus-" + sqlUID.String()[:8]
		sqliteDB.ExecContext(ctx,
			`INSERT INTO users (id, email, password_hash, role, status, user_key, kind)
			 VALUES (?,?,'h','member','active',?,'human')`,
			sqlUID.String(), sqlUKey+"@x.com", sqlUKey)

		pgID := uuid.New()
		pgFam := uuid.New()
		expires := time.Now().Add(time.Hour)
		if _, err := pgDB.ExecContext(ctx,
			`INSERT INTO user_sessions (id, user_id, family_id, refresh_token_hash, expires_at, metadata)
			 VALUES ($1,$2,$3,$4,$5,$6::jsonb)`,
			pgID, pgUID, pgFam, "tok-pg-"+pgID.String(), expires, paritySweepMetaJSON); err != nil {
			t.Fatalf("PG user_sessions: %v", err)
		}

		sqlID := uuid.New()
		sqlFam := uuid.New()
		if _, err := sqliteDB.ExecContext(ctx,
			`INSERT INTO user_sessions (id, user_id, family_id, refresh_token_hash, expires_at, metadata)
			 VALUES (?,?,?,?,?,?)`,
			sqlID.String(), sqlUID.String(), sqlFam.String(), "tok-sq-"+sqlID.String(),
			expires.Format(time.RFC3339), paritySweepMetaJSON); err != nil {
			t.Fatalf("SQLite user_sessions: %v", err)
		}
		assertPGMeta(t, "user_sessions", "user_sessions", "id", pgID, pgDB)
		assertSQLiteMeta(t, "user_sessions", "user_sessions", "id", sqlID.String(), sqliteDB)
	})
}
