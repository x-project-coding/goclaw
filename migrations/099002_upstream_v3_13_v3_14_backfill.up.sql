-- Fork-only compatibility migration for databases that already ran the reserved
-- 099xxx fork block (up to 099001) before upstream v3.13/v3.14 added migrations
-- 000068..000080.
--
-- Those databases report schema version 99001 (see internal/upgrade/version.go),
-- so golang-migrate's single-version watermark will NEVER visit the lower-numbered
-- upstream files 000068..000080. This file re-expresses every schema change in that
-- range in idempotent form so it is safe to apply on a 99001 database where the
-- objects do not yet exist, and safe to run twice.
--
-- Keep this file idempotent: fresh databases run 000068..000080 normally (they are
-- below 099000/099001/099002 in the ordered set), so by the time this file executes
-- on a fresh DB every object below already exists and each statement is a no-op.

-- 000068_bitrix_portals
-- Bitrix24 portal OAuth state. credentials/state are AES-256-GCM ciphertext (BYTEA).
CREATE TABLE IF NOT EXISTS bitrix_portals (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    name         VARCHAR(100) NOT NULL,
    domain       VARCHAR(255) NOT NULL,
    -- credentials: AES-GCM ciphertext of {client_id, client_secret}
    credentials  BYTEA,
    -- state: AES-GCM ciphertext of BitrixPortalState JSON
    state        BYTEA,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- One portal name per tenant. Different tenants may reuse the same name.
CREATE UNIQUE INDEX IF NOT EXISTS idx_bitrix_portals_tenant_name
    ON bitrix_portals (tenant_id, name);

-- Incoming install/event callbacks resolve by domain before tenant scope is known.
CREATE UNIQUE INDEX IF NOT EXISTS idx_bitrix_portals_domain
    ON bitrix_portals (LOWER(TRIM(domain)));

-- 000069_browser_cookies
-- Server-side browser cookie sync. encrypted_value is AES-256-GCM ciphertext.
CREATE TABLE IF NOT EXISTS browser_cookies (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    user_id         VARCHAR(255) NOT NULL,
    agent_id        VARCHAR(255) NOT NULL,
    domain          TEXT NOT NULL,
    name            TEXT NOT NULL,
    path            TEXT NOT NULL DEFAULT '/',
    encrypted_value TEXT NOT NULL,
    secure          BOOLEAN NOT NULL DEFAULT FALSE,
    http_only       BOOLEAN NOT NULL DEFAULT FALSE,
    same_site       VARCHAR(32) NOT NULL DEFAULT '',
    expires_at      TIMESTAMPTZ,
    source          VARCHAR(64) NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT browser_cookies_domain_not_empty CHECK (TRIM(domain) <> ''),
    CONSTRAINT browser_cookies_name_not_empty CHECK (TRIM(name) <> ''),
    CONSTRAINT browser_cookies_path_not_empty CHECK (TRIM(path) <> '')
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_browser_cookies_scope_unique
    ON browser_cookies (tenant_id, user_id, agent_id, domain, path, name);

CREATE INDEX IF NOT EXISTS idx_browser_cookies_scope_domain
    ON browser_cookies (tenant_id, user_id, agent_id, domain);

CREATE INDEX IF NOT EXISTS idx_browser_cookies_expires_at
    ON browser_cookies (expires_at);

-- 000070_usage_caps_pricing
CREATE TABLE IF NOT EXISTS usage_pricing_catalog (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    model_id TEXT NOT NULL UNIQUE,
    canonical_model_id TEXT,
    raw_pricing JSONB NOT NULL DEFAULT '{}'::jsonb,
    raw_model JSONB NOT NULL DEFAULT '{}'::jsonb,
    input_price NUMERIC(30, 18) CHECK (input_price IS NULL OR input_price >= 0),
    output_price NUMERIC(30, 18) CHECK (output_price IS NULL OR output_price >= 0),
    cache_read_price NUMERIC(30, 18) CHECK (cache_read_price IS NULL OR cache_read_price >= 0),
    cache_write_price NUMERIC(30, 18) CHECK (cache_write_price IS NULL OR cache_write_price >= 0),
    reasoning_price NUMERIC(30, 18) CHECK (reasoning_price IS NULL OR reasoning_price >= 0),
    request_price NUMERIC(30, 18) CHECK (request_price IS NULL OR request_price >= 0),
    image_price NUMERIC(30, 18) CHECK (image_price IS NULL OR image_price >= 0),
    web_search_price NUMERIC(30, 18) CHECK (web_search_price IS NULL OR web_search_price >= 0),
    synced_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_usage_pricing_catalog_synced_at
    ON usage_pricing_catalog (synced_at DESC);

CREATE TABLE IF NOT EXISTS usage_pricing_overrides (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    provider_id UUID NOT NULL REFERENCES llm_providers(id) ON DELETE CASCADE,
    provider_type TEXT NOT NULL,
    model_id TEXT NOT NULL,
    input_price NUMERIC(30, 18) CHECK (input_price IS NULL OR input_price >= 0),
    output_price NUMERIC(30, 18) CHECK (output_price IS NULL OR output_price >= 0),
    cache_read_price NUMERIC(30, 18) CHECK (cache_read_price IS NULL OR cache_read_price >= 0),
    cache_write_price NUMERIC(30, 18) CHECK (cache_write_price IS NULL OR cache_write_price >= 0),
    reasoning_price NUMERIC(30, 18) CHECK (reasoning_price IS NULL OR reasoning_price >= 0),
    request_price NUMERIC(30, 18) CHECK (request_price IS NULL OR request_price >= 0),
    image_price NUMERIC(30, 18) CHECK (image_price IS NULL OR image_price >= 0),
    web_search_price NUMERIC(30, 18) CHECK (web_search_price IS NULL OR web_search_price >= 0),
    enabled BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, provider_id, model_id)
);

CREATE INDEX IF NOT EXISTS idx_usage_pricing_overrides_tenant_provider
    ON usage_pricing_overrides (tenant_id, provider_id, model_id)
    WHERE enabled;

-- 000071_usage_cap_policies
CREATE TABLE IF NOT EXISTS usage_cap_policies (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    agent_id UUID REFERENCES agents(id) ON DELETE CASCADE,
    provider_id UUID REFERENCES llm_providers(id) ON DELETE CASCADE,
    provider_type TEXT,
    model_id TEXT,
    window_key TEXT NOT NULL CHECK (window_key IN ('hour', 'day', 'week', 'month')),
    max_tokens BIGINT CHECK (max_tokens IS NULL OR max_tokens >= 0),
    max_cost_micros BIGINT CHECK (max_cost_micros IS NULL OR max_cost_micros >= 0),
    enabled BOOLEAN NOT NULL DEFAULT true,
    priority INTEGER NOT NULL DEFAULT 100,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (max_tokens IS NOT NULL OR max_cost_micros IS NOT NULL)
);

CREATE INDEX IF NOT EXISTS idx_usage_cap_policies_scope
    ON usage_cap_policies (tenant_id, enabled, agent_id, provider_id, provider_type, model_id);

CREATE TABLE IF NOT EXISTS usage_cap_counters (
    policy_id UUID NOT NULL REFERENCES usage_cap_policies(id) ON DELETE CASCADE,
    window_start TIMESTAMPTZ NOT NULL,
    window_end TIMESTAMPTZ NOT NULL,
    used_tokens BIGINT NOT NULL DEFAULT 0,
    reserved_tokens BIGINT NOT NULL DEFAULT 0,
    used_cost_micros BIGINT NOT NULL DEFAULT 0,
    reserved_cost_micros BIGINT NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (policy_id, window_start)
);

CREATE TABLE IF NOT EXISTS usage_cap_reservations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    reservation_key TEXT NOT NULL,
    policy_id UUID NOT NULL REFERENCES usage_cap_policies(id) ON DELETE CASCADE,
    window_start TIMESTAMPTZ NOT NULL,
    reserved_tokens BIGINT NOT NULL DEFAULT 0,
    reserved_cost_micros BIGINT NOT NULL DEFAULT 0,
    actual_tokens BIGINT NOT NULL DEFAULT 0,
    actual_cost_micros BIGINT NOT NULL DEFAULT 0,
    status TEXT NOT NULL DEFAULT 'reserved',
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (reservation_key, policy_id)
);

CREATE INDEX IF NOT EXISTS idx_usage_cap_reservations_key
    ON usage_cap_reservations (reservation_key);

CREATE TABLE IF NOT EXISTS usage_cap_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    policy_id UUID REFERENCES usage_cap_policies(id) ON DELETE SET NULL,
    reservation_key TEXT,
    decision TEXT NOT NULL,
    reason TEXT,
    estimated_tokens BIGINT NOT NULL DEFAULT 0,
    estimated_cost_micros BIGINT NOT NULL DEFAULT 0,
    actual_tokens BIGINT NOT NULL DEFAULT 0,
    actual_cost_micros BIGINT NOT NULL DEFAULT 0,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_usage_cap_events_tenant_created
    ON usage_cap_events (tenant_id, created_at DESC);

-- 000072_agent_budget_usage_cap_bridge
-- Adds a 'source' column to usage_cap_policies and backfills one month-window
-- cost cap policy per agent that carries budget_monthly_cents. Idempotent:
-- the partial unique index + ON CONFLICT DO NOTHING keep re-runs from duplicating.
ALTER TABLE usage_cap_policies
    ADD COLUMN IF NOT EXISTS source TEXT NOT NULL DEFAULT 'manual';

CREATE UNIQUE INDEX IF NOT EXISTS idx_usage_cap_policies_agent_budget_source
    ON usage_cap_policies (tenant_id, agent_id)
    WHERE source = 'agent_budget_monthly_cents';

INSERT INTO usage_cap_policies (
    tenant_id, agent_id, window_key, max_cost_micros, enabled, priority, source
)
SELECT
    tenant_id,
    id,
    'month',
    budget_monthly_cents::BIGINT * 10000,
    true,
    90,
    'agent_budget_monthly_cents'
FROM agents
WHERE deleted_at IS NULL
  AND budget_monthly_cents IS NOT NULL
  AND budget_monthly_cents > 0
ON CONFLICT DO NOTHING;

-- 000073_secure_cli_credential_type
-- Typed-credential columns on existing secure CLI tables; all NULL by default.
ALTER TABLE secure_cli_user_credentials
    ADD COLUMN IF NOT EXISTS credential_type TEXT NULL,
    ADD COLUMN IF NOT EXISTS host_scope TEXT NULL;

ALTER TABLE secure_cli_binaries
    ADD COLUMN IF NOT EXISTS adapter_name TEXT NULL;

-- 000074_run_timeline_items
-- Upstream used bare CREATE TABLE / CREATE INDEX; made idempotent here.
CREATE TABLE IF NOT EXISTS run_timeline_items (
    id           UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    tenant_id    UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    run_id       TEXT NOT NULL,
    session_key  VARCHAR(500) NOT NULL,
    agent_id     UUID REFERENCES agents(id) ON DELETE SET NULL,
    user_id      VARCHAR(255),
    channel      VARCHAR(50),
    chat_id      VARCHAR(255),
    seq          INT NOT NULL,
    item_type    VARCHAR(40) NOT NULL,
    status       VARCHAR(40),
    title        TEXT,
    preview      TEXT,
    content      TEXT NOT NULL DEFAULT '',
    tool_name    VARCHAR(255),
    tool_call_id VARCHAR(255),
    trace_id     UUID REFERENCES traces(id) ON DELETE SET NULL,
    span_id      UUID REFERENCES spans(id) ON DELETE SET NULL,
    metadata     JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, run_id, seq)
);

CREATE INDEX IF NOT EXISTS idx_run_timeline_run_seq ON run_timeline_items(tenant_id, run_id, seq);
CREATE INDEX IF NOT EXISTS idx_run_timeline_session_time ON run_timeline_items(tenant_id, session_key, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_run_timeline_trace ON run_timeline_items(tenant_id, trace_id) WHERE trace_id IS NOT NULL;

-- 000075_channel_context_capabilities
-- Upstream used bare CREATE TABLE / CREATE INDEX; made idempotent here.
CREATE TABLE IF NOT EXISTS mcp_context_grants (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    channel_instance_id UUID NOT NULL REFERENCES channel_instances(id) ON DELETE CASCADE,
    scope_type VARCHAR(32) NOT NULL,
    scope_key VARCHAR(255) NOT NULL DEFAULT '',
    server_id UUID NOT NULL REFERENCES mcp_servers(id) ON DELETE CASCADE,
    enabled BOOLEAN NOT NULL DEFAULT true,
    tool_allow JSONB,
    tool_deny JSONB,
    config_overrides JSONB,
    granted_by VARCHAR(255) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(tenant_id, channel_instance_id, scope_type, scope_key, server_id)
);

CREATE INDEX IF NOT EXISTS idx_mcp_context_grants_scope ON mcp_context_grants(tenant_id, channel_instance_id, scope_type, scope_key);
CREATE INDEX IF NOT EXISTS idx_mcp_context_grants_server ON mcp_context_grants(server_id);

CREATE TABLE IF NOT EXISTS mcp_context_credentials (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    channel_instance_id UUID NOT NULL REFERENCES channel_instances(id) ON DELETE CASCADE,
    scope_type VARCHAR(32) NOT NULL,
    scope_key VARCHAR(255) NOT NULL DEFAULT '',
    server_id UUID NOT NULL REFERENCES mcp_servers(id) ON DELETE CASCADE,
    api_key TEXT,
    headers BYTEA,
    env BYTEA,
    created_by VARCHAR(255) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(tenant_id, channel_instance_id, scope_type, scope_key, server_id)
);

CREATE INDEX IF NOT EXISTS idx_mcp_context_credentials_scope ON mcp_context_credentials(tenant_id, channel_instance_id, scope_type, scope_key);
CREATE INDEX IF NOT EXISTS idx_mcp_context_credentials_server ON mcp_context_credentials(server_id);

CREATE TABLE IF NOT EXISTS secure_cli_context_grants (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    channel_instance_id UUID NOT NULL REFERENCES channel_instances(id) ON DELETE CASCADE,
    scope_type VARCHAR(32) NOT NULL,
    scope_key VARCHAR(255) NOT NULL DEFAULT '',
    binary_id UUID NOT NULL REFERENCES secure_cli_binaries(id) ON DELETE CASCADE,
    deny_args JSONB,
    deny_verbose JSONB,
    timeout_seconds INTEGER,
    tips TEXT,
    encrypted_env BYTEA,
    enabled BOOLEAN NOT NULL DEFAULT true,
    granted_by VARCHAR(255) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(tenant_id, channel_instance_id, scope_type, scope_key, binary_id)
);

CREATE INDEX IF NOT EXISTS idx_secure_cli_context_grants_scope ON secure_cli_context_grants(tenant_id, channel_instance_id, scope_type, scope_key);
CREATE INDEX IF NOT EXISTS idx_secure_cli_context_grants_binary ON secure_cli_context_grants(binary_id);

CREATE TABLE IF NOT EXISTS secure_cli_context_credentials (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    channel_instance_id UUID NOT NULL REFERENCES channel_instances(id) ON DELETE CASCADE,
    scope_type VARCHAR(32) NOT NULL,
    scope_key VARCHAR(255) NOT NULL DEFAULT '',
    binary_id UUID NOT NULL REFERENCES secure_cli_binaries(id) ON DELETE CASCADE,
    encrypted_env BYTEA NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    credential_type TEXT,
    host_scope TEXT,
    created_by VARCHAR(255) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(tenant_id, channel_instance_id, scope_type, scope_key, binary_id)
);

CREATE INDEX IF NOT EXISTS idx_secure_cli_context_credentials_scope ON secure_cli_context_credentials(tenant_id, channel_instance_id, scope_type, scope_key);
CREATE INDEX IF NOT EXISTS idx_secure_cli_context_credentials_binary ON secure_cli_context_credentials(binary_id);

-- 000076_channel_memory_extraction
CREATE TABLE IF NOT EXISTS channel_memory_extraction_runs (
    id UUID PRIMARY KEY,
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    channel_instance_id UUID NOT NULL REFERENCES channel_instances(id) ON DELETE CASCADE,
    channel_name VARCHAR(255) NOT NULL,
    agent_id UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    user_id VARCHAR(255) NOT NULL DEFAULT '',
    history_key VARCHAR(255) NOT NULL,
    trigger VARCHAR(32) NOT NULL DEFAULT 'scheduled',
    status VARCHAR(32) NOT NULL DEFAULT 'pending',
    source_start_id VARCHAR(255) NOT NULL DEFAULT '',
    source_end_id VARCHAR(255) NOT NULL DEFAULT '',
    source_start_at TIMESTAMPTZ,
    source_end_at TIMESTAMPTZ,
    message_count INTEGER NOT NULL DEFAULT 0,
    redaction_count INTEGER NOT NULL DEFAULT 0,
    redaction_types JSONB NOT NULL DEFAULT '[]'::jsonb,
    item_count INTEGER NOT NULL DEFAULT 0,
    error_message TEXT NOT NULL DEFAULT '',
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, channel_instance_id, history_key, source_start_id, source_end_id)
);

CREATE INDEX IF NOT EXISTS idx_channel_memory_runs_channel
    ON channel_memory_extraction_runs(tenant_id, channel_instance_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_channel_memory_runs_status
    ON channel_memory_extraction_runs(tenant_id, status, created_at DESC);

CREATE TABLE IF NOT EXISTS channel_memory_extraction_items (
    id UUID PRIMARY KEY,
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    run_id UUID NOT NULL REFERENCES channel_memory_extraction_runs(id) ON DELETE CASCADE,
    channel_instance_id UUID NOT NULL REFERENCES channel_instances(id) ON DELETE CASCADE,
    agent_id UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    user_id VARCHAR(255) NOT NULL DEFAULT '',
    item_hash VARCHAR(128) NOT NULL,
    item_type VARCHAR(64) NOT NULL,
    summary TEXT NOT NULL,
    topics JSONB NOT NULL DEFAULT '[]'::jsonb,
    entities JSONB NOT NULL DEFAULT '[]'::jsonb,
    confidence DOUBLE PRECISION NOT NULL DEFAULT 0,
    source_id VARCHAR(255) NOT NULL DEFAULT '',
    status VARCHAR(32) NOT NULL DEFAULT 'pending_review',
    approved_by VARCHAR(255) NOT NULL DEFAULT '',
    approved_at TIMESTAMPTZ,
    rejected_by VARCHAR(255) NOT NULL DEFAULT '',
    rejected_at TIMESTAMPTZ,
    deleted_at TIMESTAMPTZ,
    written_at TIMESTAMPTZ,
    episodic_id VARCHAR(64) NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, run_id, item_hash)
);

CREATE INDEX IF NOT EXISTS idx_channel_memory_items_channel_status
    ON channel_memory_extraction_items(tenant_id, channel_instance_id, status, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_channel_memory_items_run
    ON channel_memory_extraction_items(tenant_id, run_id);

-- 000077_secure_cli_agent_credentials
-- Composite unique indexes on pre-existing tables (agents, secure_cli_binaries)
-- must exist before the composite FKs below can reference them. Upstream used
-- bare CREATE TABLE / CREATE INDEX; made idempotent here. The named FK/UNIQUE
-- constraints ride inside CREATE TABLE IF NOT EXISTS, so a re-run that finds the
-- table already present simply skips them (no ADD CONSTRAINT guard needed).
CREATE UNIQUE INDEX IF NOT EXISTS idx_secure_cli_binaries_id_tenant
    ON secure_cli_binaries(id, tenant_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_agents_id_tenant
    ON agents(id, tenant_id);

CREATE TABLE IF NOT EXISTS secure_cli_agent_credentials (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    binary_id       UUID NOT NULL,
    agent_id        UUID NOT NULL,
    encrypted_env   BYTEA NOT NULL,
    metadata        JSONB NOT NULL DEFAULT '{}',
    tenant_id       UUID NOT NULL REFERENCES tenants(id),
    credential_type TEXT NULL,
    host_scope      TEXT NULL,
    created_by      VARCHAR(255) NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(binary_id, agent_id, tenant_id),
    CONSTRAINT fk_scac_binary_tenant
        FOREIGN KEY (binary_id, tenant_id) REFERENCES secure_cli_binaries(id, tenant_id) ON DELETE CASCADE,
    CONSTRAINT fk_scac_agent_tenant
        FOREIGN KEY (agent_id, tenant_id) REFERENCES agents(id, tenant_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_scac_tenant ON secure_cli_agent_credentials(tenant_id);
CREATE INDEX IF NOT EXISTS idx_scac_binary ON secure_cli_agent_credentials(binary_id);
CREATE INDEX IF NOT EXISTS idx_scac_agent ON secure_cli_agent_credentials(agent_id);

-- 000078_skill_user_grants_tenant_unique
-- Swap the (skill_id, user_id) unique constraint for a tenant-scoped one. The
-- DROPs are already idempotent; the ADD CONSTRAINT is guarded via pg_constraint
-- since ALTER TABLE ADD CONSTRAINT has no IF NOT EXISTS form.
ALTER TABLE skill_user_grants
    DROP CONSTRAINT IF EXISTS skill_user_grants_skill_id_user_id_key;

ALTER TABLE skill_user_grants
    DROP CONSTRAINT IF EXISTS skill_user_grants_skill_id_user_id_tenant_id_key;

DO $$ BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'skill_user_grants_skill_id_user_id_tenant_id_key'
    ) THEN
        ALTER TABLE skill_user_grants
            ADD CONSTRAINT skill_user_grants_skill_id_user_id_tenant_id_key
            UNIQUE (skill_id, user_id, tenant_id);
    END IF;
END $$;

-- 000079_skill_self_evolution
-- Upstream used bare CREATE TABLE / CREATE INDEX; made idempotent here.
-- The final INSERT seeds one skill_versions row per live skill and is guarded
-- by ON CONFLICT (skill_id, version) DO NOTHING for safe re-runs.
CREATE TABLE IF NOT EXISTS skill_evolution_settings (
    tenant_id        UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    skill_id         UUID NOT NULL REFERENCES skills(id) ON DELETE CASCADE,
    enabled          BOOLEAN NOT NULL DEFAULT false,
    mode             VARCHAR(32) NOT NULL DEFAULT 'suggest_only',
    last_analyzed_at TIMESTAMPTZ,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (tenant_id, skill_id),
    CONSTRAINT chk_skill_evolution_mode CHECK (mode IN ('suggest_only', 'auto_analyze'))
);

CREATE INDEX IF NOT EXISTS idx_skill_evolution_settings_skill ON skill_evolution_settings(skill_id);

CREATE TABLE IF NOT EXISTS skill_usage_metrics (
    id                UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    tenant_id         UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    skill_id          UUID NOT NULL REFERENCES skills(id) ON DELETE CASCADE,
    skill_slug        VARCHAR(255) NOT NULL,
    skill_version     INT NOT NULL DEFAULT 1,
    agent_id          UUID REFERENCES agents(id) ON DELETE SET NULL,
    user_id           VARCHAR(255),
    session_key       TEXT,
    trace_id          TEXT,
    invocation_id     TEXT,
    invocation_source VARCHAR(32) NOT NULL DEFAULT 'runtime',
    status            VARCHAR(32) NOT NULL DEFAULT 'started',
    failure_reason    TEXT,
    tool_calls_count  INT NOT NULL DEFAULT 0,
    duration_ms       BIGINT NOT NULL DEFAULT 0,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_skill_usage_status CHECK (status IN ('started', 'succeeded', 'failed', 'abandoned'))
);

CREATE INDEX IF NOT EXISTS idx_skill_usage_metrics_skill_created ON skill_usage_metrics(skill_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_skill_usage_metrics_tenant_created ON skill_usage_metrics(tenant_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_skill_usage_metrics_status ON skill_usage_metrics(skill_id, status, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_skill_usage_metrics_invocation ON skill_usage_metrics(invocation_id) WHERE invocation_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS skill_improvement_suggestions (
    id                     UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    tenant_id              UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    skill_id               UUID NOT NULL REFERENCES skills(id) ON DELETE CASCADE,
    skill_slug             VARCHAR(255) NOT NULL,
    suggestion_type        VARCHAR(64) NOT NULL,
    status                 VARCHAR(32) NOT NULL DEFAULT 'pending',
    reason                 TEXT NOT NULL DEFAULT '',
    evidence               JSONB NOT NULL DEFAULT '{}',
    draft_patch            JSONB NOT NULL DEFAULT '{}',
    target_file            TEXT NOT NULL DEFAULT '',
    created_by_actor_type  VARCHAR(32) NOT NULL DEFAULT '',
    created_by_actor_id    VARCHAR(255) NOT NULL DEFAULT '',
    reviewed_by_actor_type VARCHAR(32) NOT NULL DEFAULT '',
    reviewed_by_actor_id   VARCHAR(255) NOT NULL DEFAULT '',
    reviewed_at            TIMESTAMPTZ,
    applied_version        INT,
    created_at             TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at             TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_skill_suggestion_status CHECK (status IN ('pending', 'approved', 'rejected', 'applied'))
);

CREATE INDEX IF NOT EXISTS idx_skill_suggestions_skill_status_created ON skill_improvement_suggestions(skill_id, status, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_skill_suggestions_tenant_created ON skill_improvement_suggestions(tenant_id, created_at DESC);

CREATE TABLE IF NOT EXISTS skill_versions (
    id                         UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    tenant_id                  UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    skill_id                   UUID NOT NULL REFERENCES skills(id) ON DELETE CASCADE,
    version                    INT NOT NULL,
    content_hash               VARCHAR(64) NOT NULL DEFAULT '',
    changed_files              JSONB NOT NULL DEFAULT '[]',
    created_by_actor_type      VARCHAR(32) NOT NULL DEFAULT '',
    created_by_actor_id        VARCHAR(255) NOT NULL DEFAULT '',
    created_from_suggestion_id UUID REFERENCES skill_improvement_suggestions(id) ON DELETE SET NULL,
    created_at                 TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(skill_id, version)
);

CREATE INDEX IF NOT EXISTS idx_skill_versions_tenant_skill ON skill_versions(tenant_id, skill_id, version DESC);

INSERT INTO skill_versions (
    tenant_id, skill_id, version, content_hash, changed_files,
    created_by_actor_type, created_by_actor_id, created_at
)
SELECT tenant_id, id, version, COALESCE(file_hash, ''), '[]'::jsonb,
       'system', 'migration', COALESCE(created_at, NOW())
FROM skills
WHERE status != 'deleted'
ON CONFLICT (skill_id, version) DO NOTHING;

-- 000080_usage_event_analytics
CREATE TABLE IF NOT EXISTS usage_events (
    id            UUID PRIMARY KEY,
    tenant_id     UUID NOT NULL REFERENCES tenants(id),
    event_time    TIMESTAMPTZ NOT NULL,
    bucket_hour   TIMESTAMPTZ NOT NULL,
    event_type    TEXT NOT NULL,
    resource_type TEXT NOT NULL,
    resource_name TEXT NOT NULL,
    resource_id   TEXT NOT NULL DEFAULT '',
    source        TEXT NOT NULL DEFAULT '',
    agent_id      UUID REFERENCES agents(id) ON DELETE SET NULL,
    team_id       UUID,
    trace_id      UUID REFERENCES traces(id) ON DELETE SET NULL,
    span_id       UUID REFERENCES spans(id) ON DELETE SET NULL,
    run_id        TEXT NOT NULL DEFAULT '',
    session_key   TEXT NOT NULL DEFAULT '',
    channel       TEXT NOT NULL DEFAULT '',
    provider      TEXT NOT NULL DEFAULT '',
    model         TEXT NOT NULL DEFAULT '',
    status        TEXT NOT NULL DEFAULT '',
    input_tokens  BIGINT NOT NULL DEFAULT 0,
    output_tokens BIGINT NOT NULL DEFAULT 0,
    total_tokens  BIGINT NOT NULL DEFAULT 0,
    cost_usd      DOUBLE PRECISION NOT NULL DEFAULT 0,
    duration_ms   INTEGER NOT NULL DEFAULT 0,
    call_count    INTEGER NOT NULL DEFAULT 1,
    error_count   INTEGER NOT NULL DEFAULT 0,
    metadata      JSONB,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_usage_events_tenant_time
    ON usage_events(tenant_id, event_time DESC);
CREATE INDEX IF NOT EXISTS idx_usage_events_tenant_resource_time
    ON usage_events(tenant_id, resource_type, resource_name, event_time DESC);
CREATE INDEX IF NOT EXISTS idx_usage_events_tenant_type_time
    ON usage_events(tenant_id, event_type, event_time DESC);
CREATE INDEX IF NOT EXISTS idx_usage_events_tenant_agent_time
    ON usage_events(tenant_id, agent_id, event_time DESC);
CREATE INDEX IF NOT EXISTS idx_usage_events_tenant_channel_time
    ON usage_events(tenant_id, channel, event_time DESC)
    WHERE channel != '';
CREATE UNIQUE INDEX IF NOT EXISTS idx_usage_events_trace_span_type_source
    ON usage_events(trace_id, span_id, event_type, source)
    WHERE trace_id IS NOT NULL AND span_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS usage_event_rollups (
    id            UUID PRIMARY KEY,
    tenant_id     UUID NOT NULL REFERENCES tenants(id),
    bucket_hour   TIMESTAMPTZ NOT NULL,
    event_type    TEXT NOT NULL,
    resource_type TEXT NOT NULL,
    resource_name TEXT NOT NULL,
    source        TEXT NOT NULL DEFAULT '',
    agent_id      UUID REFERENCES agents(id) ON DELETE SET NULL,
    channel       TEXT NOT NULL DEFAULT '',
    provider      TEXT NOT NULL DEFAULT '',
    model         TEXT NOT NULL DEFAULT '',
    status        TEXT NOT NULL DEFAULT '',
    input_tokens  BIGINT NOT NULL DEFAULT 0,
    output_tokens BIGINT NOT NULL DEFAULT 0,
    total_tokens  BIGINT NOT NULL DEFAULT 0,
    cost_usd      DOUBLE PRECISION NOT NULL DEFAULT 0,
    duration_ms   INTEGER NOT NULL DEFAULT 0,
    call_count    INTEGER NOT NULL DEFAULT 0,
    error_count   INTEGER NOT NULL DEFAULT 0,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_usage_event_rollups_unique
    ON usage_event_rollups (
        tenant_id,
        bucket_hour,
        event_type,
        resource_type,
        resource_name,
        source,
        COALESCE(agent_id, '00000000-0000-0000-0000-000000000000'::uuid),
        channel,
        provider,
        model,
        status
    );
CREATE INDEX IF NOT EXISTS idx_usage_event_rollups_tenant_hour
    ON usage_event_rollups(tenant_id, bucket_hour DESC);
CREATE INDEX IF NOT EXISTS idx_usage_event_rollups_resource_hour
    ON usage_event_rollups(tenant_id, resource_type, resource_name, bucket_hour DESC);
