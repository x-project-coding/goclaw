-- Fork-only compatibility migration for databases that already ran
-- 099000_tenant_cascade before upstream v3.12.0 added migrations 000058..000067.
--
-- Those databases report schema version 99000, so golang-migrate will never
-- visit lower-numbered upstream migrations. Keep this file idempotent so fresh
-- databases, which do run 000058..000067 before 099000/099001, are unaffected.

-- 000058_agent_grants_env_override
ALTER TABLE secure_cli_agent_grants
    ADD COLUMN IF NOT EXISTS encrypted_env BYTEA;

-- 000059_webhooks + 000060_webhook_calls_lease_token
-- + 000061_webhooks_encrypted_secret
CREATE TABLE IF NOT EXISTS webhooks (
    id                  uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id           uuid        NOT NULL,
    agent_id            uuid        REFERENCES agents(id) ON DELETE SET NULL,
    name                text        NOT NULL,
    kind                text        NOT NULL CHECK (kind IN ('llm', 'message')),
    secret_prefix       text,
    secret_hash         text        NOT NULL,
    encrypted_secret    text        NOT NULL DEFAULT '',
    scopes              text[]      NOT NULL DEFAULT '{}',
    channel_id          uuid,
    rate_limit_per_min  int         NOT NULL DEFAULT 60,
    ip_allowlist        text[]      NOT NULL DEFAULT '{}',
    require_hmac        boolean     NOT NULL DEFAULT false,
    localhost_only      boolean     NOT NULL DEFAULT false,
    revoked             boolean     NOT NULL DEFAULT false,
    created_by          text,
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now(),
    last_used_at        timestamptz
);

ALTER TABLE webhooks
    ADD COLUMN IF NOT EXISTS encrypted_secret TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_webhooks_tenant
    ON webhooks (tenant_id);
CREATE INDEX IF NOT EXISTS idx_webhooks_tenant_agent
    ON webhooks (tenant_id, agent_id);
CREATE UNIQUE INDEX IF NOT EXISTS uq_webhooks_secret
    ON webhooks (secret_hash) WHERE revoked = false;

CREATE TABLE IF NOT EXISTS webhook_calls (
    id               uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id        uuid        NOT NULL,
    webhook_id       uuid        NOT NULL REFERENCES webhooks(id) ON DELETE CASCADE,
    agent_id         uuid,
    idempotency_key  text,
    mode             text        NOT NULL CHECK (mode IN ('sync', 'async')),
    callback_url     text,
    status           text        NOT NULL DEFAULT 'queued' CHECK (status IN ('queued', 'running', 'done', 'failed', 'dead')),
    attempts         int         NOT NULL DEFAULT 0,
    delivery_id      uuid        NOT NULL DEFAULT gen_random_uuid(),
    lease_token      text,
    next_attempt_at  timestamptz,
    started_at       timestamptz,
    request_payload  jsonb,
    response         jsonb,
    last_error       text,
    created_at       timestamptz NOT NULL DEFAULT now(),
    completed_at     timestamptz
);

ALTER TABLE webhook_calls
    ADD COLUMN IF NOT EXISTS lease_token TEXT;

CREATE INDEX IF NOT EXISTS idx_webhook_calls_tenant_created
    ON webhook_calls (tenant_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_webhook_calls_status_attempt
    ON webhook_calls (status, next_attempt_at);
CREATE UNIQUE INDEX IF NOT EXISTS uq_webhook_calls_idempotency
    ON webhook_calls (webhook_id, idempotency_key)
    WHERE idempotency_key IS NOT NULL;

-- 000062_workstations
CREATE TABLE IF NOT EXISTS workstations (
    id              UUID PRIMARY KEY,
    workstation_key VARCHAR(100) NOT NULL,
    tenant_id       UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    name            VARCHAR(255) NOT NULL,
    backend_type    VARCHAR(20) NOT NULL CHECK (backend_type IN ('ssh','docker')),
    metadata        BYTEA NOT NULL,
    default_cwd     VARCHAR(500) NOT NULL DEFAULT '',
    default_env     BYTEA NOT NULL,
    active          BOOLEAN NOT NULL DEFAULT TRUE,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by      VARCHAR(255) NOT NULL DEFAULT '',
    UNIQUE (tenant_id, workstation_key)
);
CREATE INDEX IF NOT EXISTS idx_workstations_tenant_active
    ON workstations(tenant_id, active) WHERE active = TRUE;

CREATE TABLE IF NOT EXISTS agent_workstation_links (
    agent_id        UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    workstation_id  UUID NOT NULL REFERENCES workstations(id) ON DELETE CASCADE,
    tenant_id       UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    is_default      BOOLEAN NOT NULL DEFAULT FALSE,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (agent_id, workstation_id)
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_agent_workstation_default
    ON agent_workstation_links(agent_id) WHERE is_default = TRUE;
CREATE INDEX IF NOT EXISTS idx_agent_workstation_tenant
    ON agent_workstation_links(tenant_id);

-- 000063_workstation_permissions
CREATE TABLE IF NOT EXISTS workstation_permissions (
    id             UUID PRIMARY KEY,
    workstation_id UUID NOT NULL REFERENCES workstations(id) ON DELETE CASCADE,
    tenant_id      UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    pattern        VARCHAR(500) NOT NULL,
    enabled        BOOLEAN NOT NULL DEFAULT TRUE,
    created_by     VARCHAR(255) NOT NULL DEFAULT '',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (workstation_id, pattern)
);
CREATE INDEX IF NOT EXISTS idx_workstation_perms_ws
    ON workstation_permissions(workstation_id) WHERE enabled = TRUE;
CREATE INDEX IF NOT EXISTS idx_workstation_perms_tenant
    ON workstation_permissions(tenant_id);

-- 000064_workstation_activity
CREATE TABLE IF NOT EXISTS workstation_activity (
    id             UUID PRIMARY KEY,
    tenant_id      UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    workstation_id UUID NOT NULL REFERENCES workstations(id) ON DELETE CASCADE,
    agent_id       VARCHAR(255) NOT NULL DEFAULT '',
    action         VARCHAR(20)  NOT NULL,
    cmd_hash       VARCHAR(64)  NOT NULL DEFAULT '',
    cmd_preview    VARCHAR(200) NOT NULL DEFAULT '',
    exit_code      INTEGER,
    duration_ms    INTEGER,
    deny_reason    VARCHAR(200) NOT NULL DEFAULT '',
    created_at     TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_ws_activity_ws_time
    ON workstation_activity(workstation_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_ws_activity_tenant_time
    ON workstation_activity(tenant_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_ws_activity_retention
    ON workstation_activity(created_at);

-- 000065_agent_model_fallback
ALTER TABLE agents
    ADD COLUMN IF NOT EXISTS model_fallback JSONB NOT NULL DEFAULT '{}'::jsonb;

-- 000066_skill_agent_manage_grants
ALTER TABLE skill_agent_grants
    ADD COLUMN IF NOT EXISTS can_manage BOOLEAN NOT NULL DEFAULT FALSE;

-- 000067_skill_agent_grants_scope_cleanup
DELETE FROM skill_agent_grants sag
USING skills s, agents a
WHERE sag.skill_id = s.id
  AND sag.agent_id = a.id
  AND (
    sag.tenant_id <> a.tenant_id
    OR (s.is_system = false AND sag.tenant_id <> s.tenant_id)
  );
