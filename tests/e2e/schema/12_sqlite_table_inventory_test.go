//go:build e2e && (sqlite || sqliteonly)

package schema_test

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/nextlevelbuilder/goclaw/internal/store/sqlitestore"
)

// sqliteExpectedTables is the canonical 65-table list for v4 SQLite.
// vault_versions is absent from SQLite (no versioning support in lite edition).
var sqliteExpectedTables = []string{
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
	// Vault (vault_versions absent in SQLite)
	"vault_documents",
	"vault_links",
	// Skills
	"skills",
	"skill_agent_grants",
	"skill_user_grants",
	"skill_versions",
	"curator_runs",
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

// sqliteDroppedTables are v3 tenant-scoped tables that must NOT exist in v4.
var sqliteDroppedTables = []string{
	"tenants",
	"tenant_users",
	"skill_tenant_configs",
	"builtin_tool_tenant_configs",
	"tenant_hook_budget",
}

func sqliteOpenFresh(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:?_foreign_keys=on")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := sqlitestore.EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	return db
}

func TestSqliteTableCount(t *testing.T) {
	db := sqliteOpenFresh(t)
	t.Cleanup(func() { db.Close() })

	var count int
	// Exclude schema_version (infrastructure table, not a domain table).
	err := db.QueryRow(`
		SELECT COUNT(*) FROM sqlite_master
		WHERE type = 'table'
		  AND name NOT LIKE 'sqlite_%'
		  AND name != 'schema_version'`).Scan(&count)
	if err != nil {
		t.Fatalf("query table count: %v", err)
	}
	if count != 65 {
		t.Errorf("expected 65 tables, got %d", count)
	}
}

func TestSqliteRequiredTables(t *testing.T) {
	db := sqliteOpenFresh(t)
	t.Cleanup(func() { db.Close() })

	for _, tbl := range sqliteExpectedTables {
		tbl := tbl
		t.Run(tbl, func(t *testing.T) {
			var count int
			err := db.QueryRow(`
				SELECT COUNT(*) FROM sqlite_master
				WHERE type = 'table' AND name = ?`, tbl).Scan(&count)
			if err != nil {
				t.Fatalf("query: %v", err)
			}
			if count == 0 {
				t.Errorf("required table %q does not exist", tbl)
			}
		})
	}
}

func TestSqliteDroppedTables(t *testing.T) {
	db := sqliteOpenFresh(t)
	t.Cleanup(func() { db.Close() })

	for _, tbl := range sqliteDroppedTables {
		tbl := tbl
		t.Run(tbl, func(t *testing.T) {
			var count int
			err := db.QueryRow(`
				SELECT COUNT(*) FROM sqlite_master
				WHERE type = 'table' AND name = ?`, tbl).Scan(&count)
			if err != nil {
				t.Fatalf("query: %v", err)
			}
			if count > 0 {
				t.Errorf("dropped table %q still exists — must not be present in v4 schema", tbl)
			}
		})
	}
}
