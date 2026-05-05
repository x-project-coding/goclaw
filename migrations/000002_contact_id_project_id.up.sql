-- Add contact_id to traces and spans for channel-contact attribution.
-- contact_id references the channel_contacts row that triggered the trace/span.
-- Nullable: populated only when an agent run originates from a channel message.
ALTER TABLE traces ADD COLUMN IF NOT EXISTS contact_id UUID REFERENCES channel_contacts(id) ON DELETE SET NULL;
CREATE INDEX IF NOT EXISTS idx_traces_contact ON traces(contact_id) WHERE contact_id IS NOT NULL;

ALTER TABLE spans ADD COLUMN IF NOT EXISTS contact_id UUID REFERENCES channel_contacts(id) ON DELETE SET NULL;
CREATE INDEX IF NOT EXISTS idx_spans_contact ON spans(contact_id) WHERE contact_id IS NOT NULL;

-- Add project_id to subagent_tasks so sub-agents remain scoped to the same
-- project as the parent agent run that spawned them.
ALTER TABLE subagent_tasks ADD COLUMN IF NOT EXISTS project_id UUID REFERENCES projects(id) ON DELETE SET NULL;
CREATE INDEX IF NOT EXISTS idx_subagent_tasks_project ON subagent_tasks(project_id) WHERE project_id IS NOT NULL;

-- Add team_id to agents so an agent can be owned by a team (for team-layer
-- permission resolution without an intermediate agent_team_members lookup).
ALTER TABLE agents ADD COLUMN IF NOT EXISTS team_id UUID REFERENCES agent_teams(id) ON DELETE SET NULL;
CREATE INDEX IF NOT EXISTS idx_agents_team ON agents(team_id) WHERE team_id IS NOT NULL;

-- Create agent_shares if it doesn't exist yet (fresh DBs get it from 000001;
-- existing DBs that were bootstrapped before this table was added need it here).
CREATE TABLE IF NOT EXISTS agent_shares (
    id                  UUID         NOT NULL PRIMARY KEY DEFAULT uuid_generate_v7(),
    agent_id            UUID         NOT NULL REFERENCES agents(id)       ON DELETE CASCADE,
    shared_with_user_id UUID         NULL     REFERENCES users(id)        ON DELETE CASCADE,
    shared_with_team_id UUID         NULL     REFERENCES agent_teams(id)  ON DELETE CASCADE,
    role                VARCHAR(20)  NOT NULL CHECK (role IN ('viewer','member','editor')),
    metadata            JSONB        NOT NULL DEFAULT '{}',
    created_by          UUID         NOT NULL REFERENCES users(id)        ON DELETE RESTRICT,
    created_at          TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    CONSTRAINT agent_shares_target_mutex CHECK (
        (shared_with_user_id IS NOT NULL AND shared_with_team_id IS NULL) OR
        (shared_with_user_id IS NULL     AND shared_with_team_id IS NOT NULL)
    )
);
CREATE INDEX IF NOT EXISTS idx_agent_shares_agent ON agent_shares(agent_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_agent_shares_user
    ON agent_shares(agent_id, shared_with_user_id)
    WHERE shared_with_user_id IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS idx_agent_shares_team
    ON agent_shares(agent_id, shared_with_team_id)
    WHERE shared_with_team_id IS NOT NULL;

-- Tighten agent_config_permissions: add CHECK constraint on config_type and
-- the deny_globs array column that were defined in the schema but absent from
-- the existing DB due to iterative development.
ALTER TABLE agent_config_permissions
    ADD COLUMN IF NOT EXISTS deny_globs TEXT[] NOT NULL DEFAULT ARRAY['.env*','secrets/**','.git/**','*.key','*.pem']::TEXT[];

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'agent_config_permissions_config_type_check'
          AND conrelid = 'agent_config_permissions'::regclass
    ) THEN
        ALTER TABLE agent_config_permissions
            ADD CONSTRAINT agent_config_permissions_config_type_check
            CHECK (config_type IN ('write_file','edit_file','delete_file','cron','heartbeat','*'));
    END IF;
END$$;
