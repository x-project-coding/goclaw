-- GoClaw v4 — drop entire schema in FK-reverse order.
-- Children before parents: leaf tables first, then tables they reference.
-- Extensions are NOT dropped — they are cluster-wide and may be shared.

-- Evolution
DROP TABLE IF EXISTS agent_evolution_suggestions CASCADE;
DROP TABLE IF EXISTS agent_evolution_metrics      CASCADE;

-- Hooks
DROP TABLE IF EXISTS user_hook_budget    CASCADE;
DROP TABLE IF EXISTS hook_executions     CASCADE;
DROP TABLE IF EXISTS hook_agents         CASCADE;
DROP TABLE IF EXISTS hooks               CASCADE;

-- Audit & Config
DROP TABLE IF EXISTS agent_config_permissions CASCADE;
DROP TABLE IF EXISTS config_secrets           CASCADE;
DROP TABLE IF EXISTS system_configs           CASCADE;
DROP TABLE IF EXISTS usage_snapshots          CASCADE;
DROP TABLE IF EXISTS activity_logs            CASCADE;

-- Tools
DROP TABLE IF EXISTS subagent_tasks               CASCADE;
DROP TABLE IF EXISTS secure_cli_user_credentials  CASCADE;
DROP TABLE IF EXISTS secure_cli_agent_grants      CASCADE;
DROP TABLE IF EXISTS secure_cli_binaries          CASCADE;
DROP TABLE IF EXISTS builtin_tools                CASCADE;

-- Tracing
DROP TABLE IF EXISTS spans  CASCADE;
DROP TABLE IF EXISTS traces CASCADE;

-- MCP
DROP TABLE IF EXISTS mcp_user_credentials  CASCADE;
DROP TABLE IF EXISTS mcp_access_requests   CASCADE;
DROP TABLE IF EXISTS mcp_user_grants       CASCADE;
DROP TABLE IF EXISTS mcp_agent_grants      CASCADE;
DROP TABLE IF EXISTS mcp_servers           CASCADE;

-- Heartbeat
DROP TABLE IF EXISTS heartbeat_run_logs CASCADE;
DROP TABLE IF EXISTS agent_heartbeats   CASCADE;

-- Cron
DROP TABLE IF EXISTS cron_run_logs CASCADE;
DROP TABLE IF EXISTS cron_jobs     CASCADE;

-- Channels
DROP TABLE IF EXISTS paired_devices           CASCADE;
DROP TABLE IF EXISTS pairing_requests         CASCADE;
DROP TABLE IF EXISTS channel_contacts         CASCADE;
DROP TABLE IF EXISTS channel_pending_messages CASCADE;
DROP TABLE IF EXISTS channel_instances        CASCADE;

-- Skills
DROP TABLE IF EXISTS curator_events    CASCADE;
DROP TABLE IF EXISTS curator_runs      CASCADE;
DROP TABLE IF EXISTS skill_versions    CASCADE;
DROP TABLE IF EXISTS skill_user_grants CASCADE;
DROP TABLE IF EXISTS skill_agent_grants CASCADE;
DROP TABLE IF EXISTS skills            CASCADE;

-- Vault
DROP TABLE IF EXISTS vault_versions  CASCADE;
DROP TABLE IF EXISTS vault_links     CASCADE;
DROP TABLE IF EXISTS vault_documents CASCADE;

-- Knowledge Graph
DROP TABLE IF EXISTS kg_dedup_candidates CASCADE;
DROP TABLE IF EXISTS kg_relations        CASCADE;
DROP TABLE IF EXISTS kg_entities         CASCADE;

-- Memory
DROP TABLE IF EXISTS episodic_summaries CASCADE;
DROP TABLE IF EXISTS embedding_cache    CASCADE;
DROP TABLE IF EXISTS memory_chunks      CASCADE;
DROP TABLE IF EXISTS memory_documents   CASCADE;

-- Sessions
DROP TABLE IF EXISTS agent_sessions CASCADE;

-- Teams
DROP TABLE IF EXISTS team_task_attachments CASCADE;
DROP TABLE IF EXISTS team_task_events      CASCADE;
DROP TABLE IF EXISTS team_task_comments    CASCADE;
DROP TABLE IF EXISTS team_tasks            CASCADE;
DROP TABLE IF EXISTS team_user_grants      CASCADE;
DROP TABLE IF EXISTS agent_team_members    CASCADE;

-- API Keys & Links
DROP TABLE IF EXISTS agent_links CASCADE;
DROP TABLE IF EXISTS api_keys    CASCADE;

-- Agent context
DROP TABLE IF EXISTS user_agent_overrides  CASCADE;
DROP TABLE IF EXISTS user_agent_profiles   CASCADE;
DROP TABLE IF EXISTS user_context_files    CASCADE;
DROP TABLE IF EXISTS agent_context_files   CASCADE;
DROP TABLE IF EXISTS agent_shares          CASCADE;

-- Teams (parent of many above — drop after children)
DROP TABLE IF EXISTS agent_teams CASCADE;

-- Agents
DROP TABLE IF EXISTS agents CASCADE;

-- LLM Providers
DROP TABLE IF EXISTS llm_providers CASCADE;

-- Auth sessions
DROP TABLE IF EXISTS user_sessions CASCADE;

-- Users (root of FK graph — drop last)
DROP TABLE IF EXISTS users CASCADE;

-- UUID v7 function
DROP FUNCTION IF EXISTS uuid_generate_v7();
