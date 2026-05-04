//go:build e2e

package schema_test

import (
	"testing"

	"github.com/nextlevelbuilder/goclaw/tests/e2e/helpers"
)

// expectedTables is the canonical v4 table list. TestPgTableCount asserts
// the actual `pg_tables` count matches `len(expectedTables)` exactly — so
// any future schema migration must update this slice to stay green.
var expectedTables = []string{
	// Core
	"users",
	"user_sessions",
	"agents",
	"agent_shares",
	"agent_context_files",
	"user_context_files",
	"user_agent_profiles",
	"user_agent_overrides",
	// API / Links
	"api_keys",
	"agent_links",
	// Teams
	"agent_teams",
	"agent_team_members",
	"team_user_grants",
	"team_tasks",
	"team_task_comments",
	"team_task_events",
	"team_task_attachments",
	// Sessions (renamed)
	"agent_sessions",
	// Memory
	"memory_documents",
	"memory_chunks",
	"embedding_cache",
	"episodic_summaries",
	// Knowledge Graph
	"kg_entities",
	"kg_relations",
	"kg_dedup_candidates",
	// Vault
	"vault_documents",
	"vault_links",
	"vault_versions",
	// Skills
	"skills",
	"skill_agent_grants",
	"skill_user_grants",
	"skill_versions",
	"curator_runs",
	"curator_events",
	// Channels
	"channel_instances",
	"channel_pending_messages",
	"channel_contacts",
	"pairing_requests",
	"paired_devices",
	// Cron
	"cron_jobs",
	"cron_run_logs",
	// Heartbeat
	"agent_heartbeats",
	"heartbeat_run_logs",
	// MCP
	"mcp_servers",
	"mcp_agent_grants",
	"mcp_user_grants",
	"mcp_access_requests",
	"mcp_user_credentials",
	// Tracing
	"traces",
	"spans",
	// Tools
	"builtin_tools",
	"secure_cli_binaries",
	"secure_cli_agent_grants",
	"secure_cli_user_credentials",
	"subagent_tasks",
	// Audit
	"activity_logs",
	"system_configs",
	"config_secrets",
	"usage_snapshots",
	// Hooks
	"hooks",
	"hook_agents",
	"hook_executions",
	"user_hook_budget",
	// Evolution
	"agent_evolution_metrics",
	"agent_evolution_suggestions",
	// LLM
	"llm_providers",
	// Auth
	"agent_config_permissions",
}

// droppedTables are the 5 v3 tenant-scoped tables that must NOT exist in v4.
var droppedTables = []string{
	"tenants",
	"tenant_users",
	"skill_tenant_configs",
	"builtin_tool_tenant_configs",
	"tenant_hook_budget",
}

func TestPgTableCount(t *testing.T) {
	helpers.MustLoadEnv()
	helpers.MigrateUp(t)

	db := helpers.MustDB(t)
	var count int
	// Exclude migration-infrastructure tables managed by golang-migrate and
	// our own data-migration tracking — these are not part of the domain schema.
	err := db.QueryRow(`
		SELECT COUNT(*) FROM pg_catalog.pg_tables
		WHERE schemaname = 'public'
		  AND tablename NOT IN ('schema_migrations', 'data_migrations')`).Scan(&count)
	if err != nil {
		t.Fatalf("query table count: %v", err)
	}
	if count != len(expectedTables) {
		t.Errorf("expected %d tables, got %d", len(expectedTables), count)
	}
}

func TestPgRequiredTables(t *testing.T) {
	t.Parallel()
	helpers.MustLoadEnv()
	db := helpers.MustDB(t)

	for _, tbl := range expectedTables {
		tbl := tbl
		t.Run(tbl, func(t *testing.T) {
			t.Parallel()
			var exists bool
			err := db.QueryRow(`
				SELECT EXISTS (
					SELECT 1 FROM pg_catalog.pg_tables
					WHERE schemaname = 'public' AND tablename = $1)`,
				tbl).Scan(&exists)
			if err != nil {
				t.Fatalf("query: %v", err)
			}
			if !exists {
				t.Errorf("required table %q does not exist", tbl)
			}
		})
	}
}

func TestPgDroppedTables(t *testing.T) {
	t.Parallel()
	helpers.MustLoadEnv()
	db := helpers.MustDB(t)

	for _, tbl := range droppedTables {
		tbl := tbl
		t.Run(tbl, func(t *testing.T) {
			t.Parallel()
			var exists bool
			err := db.QueryRow(`
				SELECT EXISTS (
					SELECT 1 FROM pg_catalog.pg_tables
					WHERE schemaname = 'public' AND tablename = $1)`,
				tbl).Scan(&exists)
			if err != nil {
				t.Fatalf("query: %v", err)
			}
			if exists {
				t.Errorf("dropped table %q still exists — must not be present in v4 schema", tbl)
			}
		})
	}
}
