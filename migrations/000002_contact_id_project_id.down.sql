ALTER TABLE agent_config_permissions DROP CONSTRAINT IF EXISTS agent_config_permissions_config_type_check;
ALTER TABLE agent_config_permissions DROP COLUMN IF EXISTS deny_globs;

DROP INDEX IF EXISTS idx_agent_shares_team;
DROP INDEX IF EXISTS idx_agent_shares_user;
DROP INDEX IF EXISTS idx_agent_shares_agent;
DROP TABLE IF EXISTS agent_shares;

DROP INDEX IF EXISTS idx_agents_team;
ALTER TABLE agents DROP COLUMN IF EXISTS team_id;

DROP INDEX IF EXISTS idx_subagent_tasks_project;
ALTER TABLE subagent_tasks DROP COLUMN IF EXISTS project_id;

DROP INDEX IF EXISTS idx_spans_contact;
ALTER TABLE spans DROP COLUMN IF EXISTS contact_id;

DROP INDEX IF EXISTS idx_traces_contact;
ALTER TABLE traces DROP COLUMN IF EXISTS contact_id;
