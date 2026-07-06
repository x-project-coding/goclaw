# Local Patches

Commits this fork (`x-project-coding/goclaw`) carries on top of upstream
(`nextlevelbuilder/goclaw`). Append new entries here when you land a fork-only
change; remove entries when an upstream sync supersedes them.

When syncing upstream (see [`UPSTREAM_SYNC.md`](UPSTREAM_SYNC.md)), walk this
list and re-verify each patch still applies cleanly or has been obsoleted.
Run `tools/check_local_patches.sh` after every merge — it greps for every
entry below and exits non-zero if any patch token has gone missing.

## Conventions

- **One section per patch.** Title = the load-bearing change.
- **Files** lists every file touched by the patch.
- **Why** captures the motivation so a future reader can decide whether the
  patch is still load-bearing.
- **Recovery grep** is the exact `grep` (or set of `grep`s) the check script
  runs to verify the patch survived a merge. Pick tokens that upstream is
  unlikely to introduce on its own.
- **Base upstream commit** records the merge-base from `upstream/dev` when
  the patch was added, so a future archaeologist can locate the conflict
  region quickly.

## Reserved fork-only migration block

Upstream uses migration numbers `000001`..`0009xx`. To avoid collisions on
every upstream merge, **all fork-only SQL migrations live in the `099xxx`
block** (`099000_tenant_cascade`, `099001_…`, etc.). Numbering is monotonic
in append order. Do not place fork migrations below `099000`.

---

## Active patches

### Patch 1 — `feat(subagents): raise maxChildrenPerAgent default 5→30, ceiling 20→50`

- **Base upstream commit:** `a97e5028` (`lite-v3.9.1-1-ga97e5028`)
- **Files:**
  - `internal/tools/subagent_config.go` — `MaxChildrenPerAgent: 30, // raised from TS default of 5`
  - `internal/config/config.go` — `// default 30, range 1-50` doc comment on `SubagentsConfig`
  - `internal/gateway/methods/config_defaults.go` — `min(src.MaxChildrenPerAgent, 50)` ceiling
  - `cmd/gateway_agents.go` — `min(sc.MaxChildrenPerAgent, 50)` ceiling
- **Why:** Orchestrator agents (Roman, etc.) plan multi-task work and fan
  out via `spawn`. Upstream's 5/20 cap drops the 6th+ child silently with no
  user-visible error.
- **Recovery grep:**
  ```
  grep -nE 'MaxChildrenPerAgent: 30|range 1-50|MaxChildrenPerAgent, 50' \
    internal/tools/subagent_config.go internal/config/config.go \
    internal/gateway/methods/config_defaults.go cmd/gateway_agents.go
  ```
  Expects 4 hits.

### Patch 2 — `ci: build fork image to ghcr.io/x-project-coding/goclaw on push to dev`

- **Base upstream commit:** `a97e5028`
- **Files:**
  - `.github/workflows/build-fork.yaml` — adds the `Build fork image` workflow
    that runs on push to `dev` and publishes `ghcr.io/x-project-coding/goclaw:latest`
    and `:<sha>`.
- **Why:** Without this workflow there is no way to ship the fork's other
  patches to `gw-dev-1`. The deployment overlay
  (`x-core/docker-compose.fork-image.yml`) points the running container at
  this image.
- **Recovery grep:**
  ```
  grep -nE 'Build fork image|ghcr.io/\$\{\{ github.repository \}\}' \
    .github/workflows/build-fork.yaml
  ```
  Expects ≥ 2 hits. **This patch must never be removed** — upstream merging
  it would mean upstream adopted our fork's image, which won't happen.

### Patch 3 — `fix(agents): require provider/model on every agent-create ingress`

- **Base upstream commit:** `a97e5028`
- **Files:**
  - `internal/http/agents.go` — `handleCreate` rejects empty `provider`/`model`
    on direct `POST /v1/agents` with `MsgProviderModelRequired`.
  - `internal/http/agents_import_agent.go` — `doImportNewAgent` emits a
    `slog.Warn` (changed from a hard reject in Patch 4) when archives lack
    `provider`/`model` on `POST /v1/agents/import`.
  - `internal/http/teams_import.go` — team-import loop skips member archives
    with empty `provider`/`model`.
  - `internal/agent/resolver.go` — `NewManagedResolver` backfills empty
    `provider`/`model` from `system_config` (`agent.default_provider` /
    `agent.default_model`) at chat time. (See also Patch 5 + Patch 6.)
- **Why:** `buildAgentFromArchive` parses `provider`/`model` from the
  archive's `agent.json`. When those keys are missing, both fields land as
  empty strings — the `NOT NULL` columns accept empty strings silently. At
  chat time the provider adapter sends `{"model":""}` to OpenRouter, which
  responds with `{"error":{"message":"No models provided"}}` — surfacing as
  a cryptic `⚠️ No models provided` with no breadcrumb to the broken row.
- **Recovery grep:** (resolver.go is covered separately by Patch 5 + 6)
  ```
  grep -nE 'MsgProviderModelRequired|archive missing provider/model' \
    internal/http/agents.go internal/http/agents_import_agent.go \
    internal/http/teams_import.go
  ```
  Expects ≥ 3 hits.

### Patch 4 — `fix(agents): warn instead of reject on import archive missing provider/model`

- **Base upstream commit:** `a97e5028`
- **Files:**
  - `internal/http/agents_import_agent.go` — replaces the hard 400 from
    Patch 3's first iteration with `slog.Warn(...)` so workspace signup
    doesn't fail on brand-agent archives that currently omit these fields.
- **Why:** Real production brand-agent archives ship without `provider`/`model`
  today. Rejecting them broke workspace signup; the resolver-side guard
  (Patch 5) still surfaces a clear chat-time error.
- **Recovery grep:**
  ```
  grep -nE 'agents\.import: archive missing provider/model — agent will be unusable' \
    internal/http/agents_import_agent.go
  ```
  Expects 1 hit.

### Patch 5 — `fix(resolver): backfill empty provider/model from system_config defaults`

- **Base upstream commit:** `a97e5028`
- **Files:**
  - `internal/agent/resolver.go` — `NewManagedResolver` reads
    `agent.default_provider` / `agent.default_model` from `system_config`
    and substitutes when the agent row has empty strings.
- **Why:** With Patch 4 letting `provider=""`, `model=""` rows through to
  satisfy signup, the resolver needs a fallback so the user actually gets
  *some* model at chat time. Returns a clear error
  (`agent X has no model configured`) instead of upstream's `No models
  provided` if even the defaults are unset.
- **Recovery grep:**
  ```
  grep -nE 'Backfill empty provider/model from system_config defaults' \
    internal/agent/resolver.go
  ```
  Expects 1 hit.

### Patch 6 — `fix(resolver): explicitly fall back to master tenant for system_config lookup`

- **Base upstream commit:** `a97e5028`
- **Files:**
  - `internal/agent/resolver.go` — when the agent's tenant lacks a
    `system_config` row for `agent.default_provider` / `agent.default_model`,
    fall back to looking those keys up on `MasterTenantID` rather than
    silently returning nothing.
- **Why:** Patch 5 reads `system_config` from the agent's tenant. New
  tenants (workspace signup) don't have those keys set; without the
  fallback, Patch 5 is a no-op for fresh workspaces — which is the exact
  audience that needs it.
- **Recovery grep:**
  ```
  grep -nE 'fall back to master tenant|MasterTenantID' \
    internal/agent/resolver.go
  ```
  Expects ≥ 1 hit.

### Patch 7 — `feat(tenants): hard-delete endpoint + cascade migration for trial cleanup`

- **Base upstream commit:** `a97e5028`
- **Files:**
  - `migrations/099000_tenant_cascade.up.sql` + `.down.sql` — one `DO $$`
    block that rewrites every FK on `tenants(id)` to `ON DELETE CASCADE`,
    so a single `DELETE FROM tenants WHERE id=$1` reclaims all child rows.
  - `internal/upgrade/version.go` — `RequiredSchemaVersion` bumped into the
    fork-only `099xxx` block (currently `99001`) so the migration runner
    picks fork-only files up. Without this bump `CheckSchema` short-circuits
    as "up to date" because `current == required` and golang-migrate is never
    invoked.
  - `internal/store/tenant_store.go` — `TenantStore.DeleteTenant` on the
    interface.
  - `internal/store/pg/tenant_store.go` — Postgres impl.
  - `internal/store/sqlitestore/tenants.go` — SQLite impl (manually deletes
    `tenant_users` first since the cascade migration is PG-only).
  - `internal/http/tenants.go` — `DELETE /v1/tenants/{id}` HTTP handler.
  - `internal/gateway/methods/tenants.go` — `tenants.delete` WS RPC method.
  - `internal/bus/types.go` — `TopicTenantDeleted` + `TenantDeletedPayload`.
  - `internal/http/auth_test.go` + `internal/http/tenant_backup_auth_helpers_test.go`
    — `DeleteTenant` stubs on existing mocks to keep them satisfying the
    interface.
- **Why:** admin-api's trial-cleanup feature (see x-admin spec
  `trial-cleanup`) needs to reclaim disk and DB rows on the gateway when a
  trial workspace expires without ever paying. Upstream has no DELETE path
  for tenants — only `PATCH status='archived'`, which frees zero bytes.
- **Recovery grep:**
  ```
  grep -nE 'DeleteTenant|TopicTenantDeleted|handleDelete' \
    internal/store/tenant_store.go internal/store/pg/tenant_store.go \
    internal/store/sqlitestore/tenants.go internal/http/tenants.go \
    internal/gateway/methods/tenants.go internal/bus/types.go
  grep -nE 'RequiredSchemaVersion uint = 99[0-9]{3}' internal/upgrade/version.go
  ```
  Plus migration presence:
  ```
  test -f migrations/099000_tenant_cascade.up.sql
  test -f migrations/099000_tenant_cascade.down.sql
  ```
  Expects ≥ 6 grep hits on the first grep, 1 hit on the second, and both
  migration files present.

### Patch 8 — `feat(providers): xrouter — route LLM traffic through router.42bucks.com with workspace/agent/user/session identity headers`

- **Base upstream commit:** `a97e5028`
- **Files:**
  - `internal/providers/xrouter.go` — new `XRouterProvider` composing
    `*OpenAIProvider` (x-router speaks OpenAI Chat Completions) and wrapping
    the HTTP client's `Transport` with `xrouterRoundTripper`. The wrapper
    reads per-request identity from `context.Context` (stashed by the
    overridden `Chat`/`ChatStream` methods from `req.Options`) and sets:
    - `X-Router-Agent-Id`   from `req.Options[OptAgentID]`
    - `X-Router-User-Id`    from `req.Options[OptUserID]`
    - `X-Router-Session-Id` from `req.Options[OptSessionKey]`
    Workspace anchor is implicit — bound to the `xrt_*` bearer key on the
    `llm_providers` row. Missing / empty-string identity is silently
    skipped; the request still goes through.
  - `internal/providers/xrouter_test.go` — 5 unit tests using a capture
    transport to assert what headers reach the wire: happy path, missing
    identity, empty-string identity, partial identity (one of three), and
    `Name()` inheritance via composition.
  - `internal/providers/adapter_xrouter.go` + `adapter_xrouter_test.go` +
    `adapter_register.go` — parallel `XRouterAdapter` registered in
    `DefaultAdapterRegistry`. The adapter system is parallel scaffolding
    (`capabilities.go:27`); keeping it in place so when goclaw eventually
    plumbs `ProviderAdapter.ToRequest` into the live request path the
    X-Router headers come along for free. **Not load-bearing today** — the
    live integration is via `XRouterProvider` above. Listed here so the
    patch catalog reflects the full diff.
  - `internal/store/provider_store.go` — adds `ProviderXRouter = "xrouter"`
    constant + `ProviderXRouter: true` entry to `ValidProviderTypes` so the
    `POST /v1/providers` ingress validator accepts the new type.
  - `internal/http/providers.go` — `case store.ProviderXRouter:` branch in
    `registerInMemory` (~ line 211 of the switch) that instantiates
    `NewXRouterProvider` for rows with `provider_type='xrouter'`.
- **Why:** x-router (`router.42bucks.com`, a 42bucks-internal OpenAI-compat
  gateway) records every request to its own `RequestLog` with workspace /
  agent / user / session attribution + per-model cost. To bill 42bucks
  workspaces on actual LLM usage, goclaw needs to POST through x-router and
  surface the identity its agent loop already populates into
  `chatReq.Options` (see
  `internal/agent/loop_pipeline_callbacks.go:221-241`). Upstream goclaw will
  never carry this — it's specific to the 42bucks deployment.
- **Recovery grep:**
  ```
  grep -nE 'X-Router-Agent-Id|X-Router-User-Id|X-Router-Session-Id|NewXRouterProvider|NewXRouterAdapter|ProviderXRouter' \
    internal/providers/xrouter.go \
    internal/providers/xrouter_test.go \
    internal/providers/adapter_xrouter.go \
    internal/providers/adapter_xrouter_test.go \
    internal/providers/adapter_register.go \
    internal/store/provider_store.go \
    internal/http/providers.go
  ```
  Expects ≥ 10 hits (the three `X-Router-*` tokens appear in both `xrouter.go`
  and `adapter_xrouter.go`; plus factory and constant tokens).

### Patch 9 — `feat(chat): per-call model override on both HTTP and WS entry points`

- **Base upstream commit:** `a97e5028`
- **Files:**
  - `internal/gateway/methods/chat.go` — `chatSendParams` gains a
    `modelOverride` JSON field (WS RPC `chat.send`); the handler passes it
    into `agent.RunRequest{ModelOverride: …}`. **Load-bearing for x-api's
    per-session routing** — x-api hits goclaw via WS `chat.send`, not HTTP.
  - `internal/http/chat_completions.go` — `ServeHTTP` reads the
    `X-GoClaw-Model` request header (trimmed) and passes it through to
    `handleStream` / `handleNonStream`, each of which sets the same
    `RunRequest.ModelOverride`. Parallel path for OpenAI-compat external
    callers (Codex / cURL / SDKs hitting `/v1/chat/completions`); not
    exercised by x-api today, kept symmetric with WS.
  - Both paths land on the existing pipeline adapter
    (`internal/agent/loop_pipeline_adapter.go:24-25`) which already prefers
    `ModelOverride` over the agent's stored model. No new state on `Loop` /
    `Pipeline` / `ChatRequest` — just exposes the same lever heartbeat uses
    (`internal/heartbeat/ticker.go:279`) to inbound callers.
- **Why:** x-api's per-session routing (workspace-chat → session
  routingMode/routingModel → Agent.model fallback) needs to pin the LLM
  model for a single chat without PATCHing the agent. Upstream goclaw has
  no override on either surface — chats inherit `Agent.model` with no
  escape hatch. The 42bucks deployment uses this for the
  "auto/fast/complex/custom" mode picker.
- **Recovery grep:**
  ```
  grep -nE 'X-GoClaw-Model|ModelOverride:[[:space:]]+(modelOverride|params\.ModelOverride)|"modelOverride,omitempty"' \
    internal/http/chat_completions.go internal/gateway/methods/chat.go
  ```
  Expects ≥ 5 hits (HTTP header read + 2 HTTP pass-throughs + WS struct
  field + WS pass-through).

### Patch 10 — `fix(chat): modelOverride also swaps provider to tenant xrouter`

- **Base upstream commit:** `cca21fb1` (PR #11 merge)
- **Files:**
  - `internal/gateway/methods/chat.go` — `ChatMethods` gains a
    `providerReg *providers.Registry` field + `SetProviderRegistry` setter.
    In `handleSend`, when `params.ModelOverride != ""`, the handler looks
    up the tenant's `"xrouter"` provider via `providerReg.Get(ctx, ...)`
    and threads it into `agent.RunRequest{ProviderOverride: …}`. Silent
    no-op when no xrouter is registered for the tenant.
  - `cmd/gateway_methods.go` — `registerAllMethods` accepts a
    `providerReg *providers.Registry` arg and calls
    `chatMethods.SetProviderRegistry(providerReg)` after construction.
  - `cmd/gateway.go` — the existing `registerAllMethods` call site
    forwards `providerRegistry` (already in scope).
- **Why:** Patch 9's modelOverride only swaps the *model*, not the
  *provider*. Agents whose stored provider can't serve the requested
  model 400 out — e.g. an agent with `provider=openai-codex` (ChatGPT
  OAuth) responds 400 to `~anthropic/claude-sonnet-latest` because that
  backend only serves `gpt-5.x`. The fix routes through the tenant's
  xrouter whenever a model override is in flight; `/auto` then picks the
  right provider for the model. Upstream wouldn't take this — it's
  42bucks-specific (tenant always has a provider literally named
  `"xrouter"` thanks to x-api auto-provisioning).
- **Recovery grep:**
  ```
  grep -nE 'SetProviderRegistry|ProviderOverride: providerOverride|providerReg\.Get\(runCtx, "xrouter"\)' \
    internal/gateway/methods/chat.go cmd/gateway_methods.go
  ```
  Expects ≥ 3 hits.

### Patch 11 — `feat(chat): thread routingMode through to x-router as X-Router-Mode header`

- **Base upstream commit:** `5a64c5b4` (`origin/dev`)
- **Files:**
  - `internal/gateway/methods/chat.go` — `chatSendParams` gains a
    `RoutingMode string \`json:"routingMode,omitempty"\`` field. The
    provider-swap block (Patch 10) now also fires when
    `params.RoutingMode != ""`, and `handleSend` threads
    `RoutingMode: params.RoutingMode` into `agent.RunRequest`.
  - `internal/agent/loop_types.go` — `RunRequest` gains a `RoutingMode`
    field next to `ModelOverride`.
  - `internal/pipeline/run_state.go` — `RunInput` gains a matching
    `RoutingMode` field so the pipeline adapter forwards it.
  - `internal/agent/loop_pipeline_adapter.go` — `convertRunInput` copies
    `RoutingMode` alongside `ModelOverride`.
  - `internal/agent/loop_pipeline_callbacks.go` — `makeCallLLM` populates
    `chatReq.Options[providers.OptRoutingMode]` from `req.RoutingMode`
    (skipped when empty), alongside the existing identity opts.
  - `internal/providers/claude_cli.go` — new `OptRoutingMode = "routing_mode"`
    options-key constant (sits with `OptAgentID`/`OptUserID`/`OptSessionKey`).
  - `internal/providers/xrouter.go` — `xrouterIdentity` gains a `routingMode`
    field; `injectXRouterIdentity` reads `OptRoutingMode`;
    `xrouterRoundTripper.RoundTrip` sets the `X-Router-Mode` request header
    when `routingMode` is non-empty. OpenAI JSON body untouched.
  - `internal/providers/xrouter_test.go` — adds coverage asserting
    `X-Router-Mode` is set when the routingMode option is present and
    absent when it is not.
- **Why:** x-api's per-session routing exposes an Auto/Fast/Complex/Custom
  mode picker. For 'auto'|'fast'|'complex' the mode value rides the WS
  `chat.send` RPC as `routingMode`; goclaw must hand it to x-router so the
  router picks the upstream model for that mode. Mode 'custom' keeps using
  `modelOverride` (Patch 9) and sends no `X-Router-Mode` header. Upstream
  goclaw has no concept of a routing mode — this is 42bucks-specific.
- **Recovery grep:**
  ```
  grep -nE 'X-Router-Mode|OptRoutingMode|RoutingMode:[[:space:]]+(req|params)\.RoutingMode|"routingMode,omitempty"' \
    internal/gateway/methods/chat.go internal/agent/loop_types.go \
    internal/agent/loop_pipeline_adapter.go internal/agent/loop_pipeline_callbacks.go \
    internal/pipeline/run_state.go internal/providers/claude_cli.go \
    internal/providers/xrouter.go
  ```
  Expects ≥ 6 hits.

### Patch 12 — `feat(skills): opt-in inline SKILL.md body for pinned skills`

- **Base upstream commit:** `223ddd4a` (`origin/dev`)
- **Files:**
  - `internal/skills/loader.go` — adds `Metadata.InlineBody` + `Info.InlineBody`
    fields (parsed from YAML/JSON frontmatter `inline_body: true|false`),
    package-level `inlineBodyMaxBytes = 8192` constant, and extends
    `BuildSummary(ctx, allowList, includeBody ...bool)` to emit a `<body>…</body>`
    XML child inside `<skill>` when the caller passes `true` AND the skill
    opted in via frontmatter. Body is loaded via `LoadSkill` (frontmatter
    stripped, `{baseDir}` placeholder resolved), capped at
    `inlineBodyMaxBytes` with a `"\n\n[… body truncated …]"` marker, and
    `escapeXML`'d. `BuildPinnedSummary` now calls `BuildSummary(ctx, names, true)`
    so pinned skills always inline opted-in bodies. Every new code site
    is tagged with a `// GOCLAW_INLINE_BODY` comment for sync detection.
  - `internal/skills/loader_test.go` — 4 new tests covering YAML+JSON
    frontmatter parsing, opt-in gating, 8KB truncation, and pinned-summary
    body inlining.
  - `internal/agent/preview_prompt.go` — `SkillsLoader` preview interface
    `BuildSummary` signature changed to variadic `(ctx, allowList, includeBody ...bool)`
    to match the loader (Go variadic methods do not satisfy non-variadic
    interface signatures, so the interface had to widen).
  - `internal/agent/preview_prompt_test_helpers_test.go` — `mockSkillsLoader.BuildSummary`
    signature widened to match.
  - `internal/http/agents.go` — `SkillPreviewBuilder` interface `BuildSummary`
    signature widened to variadic (same rationale as above).
  - `internal/agent/systemprompt_sections.go` — doc-comment only update on
    `buildSkillsHybridSection` and `buildPinnedSkillsMinimalSection` noting
    that the pinned summary may now carry inlined `<body>` elements.
- **Why:** Goclaw already inlines `<available_skills>` XML metadata for
  pinned skills, but agents still need a `read_file` round-trip to load
  each SKILL.md body — costly when a pinned skill's body is the actual
  instructions you want the agent to follow. The new `inline_body: true`
  frontmatter flag lets specific skills (e.g. role briefings, response
  contracts, tone guides) ship their full body alongside the metadata.
  Default `false` keeps backward compatibility — existing skills are
  unaffected. Bodies cap at 8KB (~2000 tokens) with a truncation marker
  to bound prompt growth. Pinned skills always honor the opt-in;
  non-pinned skills loaded via `skill_search` never inline bodies.
  Upstream is unlikely to take this — it's a 42bucks-shaped feature
  for our specific agent flow. Design doc:
  `/home/goclaw/workspace/pinned-skills-propagation-design-2026-05-20.md`.
- **Recovery grep:**
  ```
  grep -nE 'GOCLAW_INLINE_BODY|InlineBody|inline_body|inlineBodyMaxBytes' \
    internal/skills/loader.go internal/skills/loader_test.go \
    internal/agent/systemprompt_sections.go
  ```
  Expects ≥ 12 hits.

### Patch 13 — `fix(upgrade): backfill skipped upstream v3.12 migrations after 099000`

- **Base upstream commit:** `392f0fda` (`v3.12.0`)
- **Files:**
  - `migrations/099001_upstream_v3_12_backfill.up.sql` — idempotent copy of
    upstream migrations `000058`..`000067` for databases already at schema
    version `99000`, where golang-migrate will not visit lower-numbered
    upstream migration files.
  - `migrations/099001_upstream_v3_12_backfill.down.sql` — intentional no-op;
    fresh databases already receive upstream `000058`..`000067` normally, so
    the compatibility migration cannot safely know which objects it created.
  - `internal/upgrade/version.go` — `RequiredSchemaVersion` raised to `99001`
    so the upgrade runner applies the compatibility migration.
  - `tools/check_local_patches.sh` — verifies the migration files, schema
    version, and key backfilled table/column tokens.
- **Why:** Patch 7 moved our production database schema version to `99000`.
  Upstream v3.12.0 added lower-numbered migrations `000058`..`000067`, so
  deploying the merged binary directly would skip the new webhook,
  workstation, model fallback, and skill grant schema changes. This patch
  preserves the reserved fork migration block while making existing `99000`
  databases compatible with v3.12.0 code.
- **Recovery grep:**
  ```
  test -f migrations/099001_upstream_v3_12_backfill.up.sql
  test -f migrations/099001_upstream_v3_12_backfill.down.sql
  grep -nE 'RequiredSchemaVersion uint = 99002' internal/upgrade/version.go
  grep -nE 'secure_cli_agent_grants|webhooks|webhook_calls|workstations|workstation_permissions|workstation_activity|model_fallback|skill_agent_grants|can_manage|DELETE FROM skill_agent_grants' \
    migrations/099001_upstream_v3_12_backfill.up.sql
  ```
  Expects both migration files present, 1 schema-version hit, and ≥ 10
  migration-content hits. (`RequiredSchemaVersion` is a single shared constant;
  it now sits at `99002`, bumped by Patch 14 which is the newest fork-only
  backfill on top of this one.)

### Patch 14 — `fix(upgrade): backfill skipped upstream v3.13/v3.14 migrations after 099001`

- **Base upstream commit:** `2f3d68e8` (`v3.14.0`)
- **Files:**
  - `migrations/099002_upstream_v3_13_v3_14_backfill.up.sql` — idempotent copy of
    upstream migrations `000068`..`000080` (v3.13 + v3.14) for databases already
    at schema version `99001`, where golang-migrate will not visit the
    lower-numbered upstream migration files.
  - `migrations/099002_upstream_v3_13_v3_14_backfill.down.sql` — intentional no-op;
    fresh databases already receive upstream `000068`..`000080` normally, so the
    compatibility migration cannot safely know which objects it created (mirrors
    the `099001` down convention).
  - `internal/upgrade/version.go` — `RequiredSchemaVersion` raised to `99002`
    so the upgrade runner applies the compatibility migration.
  - `tools/check_local_patches.sh` — verifies the migration files, the bumped
    schema version, and key backfilled table tokens.
- **Why:** Patch 7 moved our production database schema version into the reserved
  `099xxx` block, and Patch 13 backfilled v3.12's `000058`..`000067`. Upstream
  v3.13/v3.14 then added lower-numbered migrations `000068`..`000080` (Bitrix
  portals, browser cookies, usage pricing/cap/event catalog, run-timeline items,
  MCP context grants, channel-memory extraction runs, secure-CLI agent
  credentials, skill user grants + skill versions, etc.). Deploying the merged
  binary directly would skip every one of those on an already-`99001` database.
  This patch preserves the reserved fork migration block while making existing
  `99001` databases compatible with v3.14.0 code.
- **Recovery grep:**
  ```
  test -f migrations/099002_upstream_v3_13_v3_14_backfill.up.sql
  test -f migrations/099002_upstream_v3_13_v3_14_backfill.down.sql
  grep -nE 'RequiredSchemaVersion uint = 99002' internal/upgrade/version.go
  grep -nE 'bitrix_portals|browser_cookies|usage_pricing_catalog|usage_cap_policies|run_timeline_items|mcp_context_grants|channel_memory_extraction_runs|secure_cli_agent_credentials|skill_user_grants_skill_id_user_id_tenant_id_key|skill_versions|usage_events|usage_event_rollups' \
    migrations/099002_upstream_v3_13_v3_14_backfill.up.sql
  ```
  Expects both migration files present, 1 schema-version hit, and ≥ 10
  migration-content hits.

### Patch 15 — `feat: brand-agent memory baseline (seed-on-import, AGENTS.md taxonomy, auto-index on memory PUT)`

- **Base upstream commit:** `9d86f0ef` (v3.14.0 fork merge)
- **Files:**
  - `internal/http/agents_import_sections.go` — after the context_files section in `doMergeImport`, unconditionally call `bootstrap.SeedToStore` (only-if-missing, warn-and-continue) so archive-imported agents get the AGENTS.md/AGENTS_CORE.md/AGENTS_TASK.md baseline.
  - `internal/bootstrap/templates/AGENTS.md` — adds `### Memory layout` (memory taxonomy: company.md, use-cases.md, people/, projects/, decisions.md) and a `## Files` section (workspace file hygiene).
  - `internal/http/agents.go` — `handleUpdate` seeds baselines on open→predefined transition (provisioning imports as open, then flips type via PUT).
  - `internal/http/memory_handlers.go` — `handlePutDocument` indexes `.md` documents after write (mirrors the memory interceptor), so HTTP-written memory is searchable.
  - Tests: `internal/http/agents_import_seed_test.go` (new), `internal/bootstrap/seed_test.go` (new), `internal/http/api_contracts_test.go` (extended).
- **Why:** 42bucks brand agents are provisioned exclusively via archive import, which never seeded the baseline instruction files — production agents lacked all memory-write and file-hygiene guidance. The Settings Memory editor and onboarding memory-seeding pipeline write via the HTTP PUT endpoint, which did not index.
- **Recovery grep:**
  ```
  grep -n "SeedToStore" internal/http/agents_import_sections.go
  grep -n "Memory layout" internal/bootstrap/templates/AGENTS.md
  grep -nE 'IndexDocument' internal/http/memory_handlers.go
  ```
  Expects >=1 hit in each file.

### Patch 16 — `feat(memory): path-based shared/private scoping for predefined-agent memory + AGENTS.md sharing semantics`

- **Base upstream commit:** `9d86f0ef` (v3.14.0 fork merge, same base as Patch 15)
- **Files:**
  - `internal/tools/memory_interceptor.go` — for **predefined** agents (`store.AgentTypeFromContext(ctx) == store.AgentTypePredefined`), `MemoryInterceptor.ReadFile`/`WriteFile`/`ListFiles` route by path: `memory/company.md`, `memory/company-research.md`, `memory/use-cases.md`, `memory/projects/*`, `memory/decisions.md` are written/read at global scope (`userID=""`) so every workspace member shares the same fact; everything else under `memory/` (`MEMORY.md`, `memory/people/*`, daily notes `memory/YYYY-MM-DD.md`) keeps the existing per-user-private scoping. `ListFiles` additionally merges the global-scope shared docs into a per-user listing so they show up alongside private files. KG extraction after a shared-path write uses the same global scope (`kgUserID=""`), regardless of the ambient `KGUserID`. **Open agents are unaffected** — all memory stays per-user. The pre-existing team-member write-block (`LeaderAgentIDFromCtx`) and leader-fallback reads are untouched.
  - `internal/bootstrap/templates/AGENTS.md` — `### Memory layout` now annotates each file's sharing scope inline (`memory/company.md`, `memory/use-cases.md`, `memory/projects/<slug>.md`, `memory/decisions.md` marked "shared with all workspace members"; `MEMORY.md`, `memory/people/<name>.md`, `memory/YYYY-MM-DD.md` marked "private to the member you're talking to") plus a rule-of-thumb line. `## Files` rewritten around the workspace-as-file-browser model: `tmp/` for scratch/intermediates/verification artifacts, no `-v2`/`-final`/`(1)` duplicate files, build machinery stays inside the project dir, and the deliver-vs-publish-vs-shared-memory distinction for handing files to one member vs. the whole team.
  - `internal/tools/agent_type_resolver.go` (new) — `AgentTypeResolverFunc` + `NewCachedAgentTypeResolver` (TTL-cached `GetByIDUnscoped` lookup). The interceptor resolves the agent type ctx-first, then via this authoritative resolver — ctx wiring alone proved fragile in production: the MCP bridge middleware builds a fresh ctx from HTTP headers and never carried the agent type, so bridged predefined-agent writes fell back to per-user scope.
  - `internal/gateway/server.go` — `bridgeContextMiddleware` now injects `store.WithAgentType(ctx, ag.AgentType)` from the agent record it already loads, so bridged tool calls see the same type-gated interceptor behavior as the main loop.
  - `cmd/gateway_managed.go` — all four `MemoryInterceptor` instances share one `NewCachedAgentTypeResolver(stores.Agents, 5*time.Minute)`.
  - Tests: `internal/tools/memory_interceptor_test.go` (extended — `isSharedMemoryPath` table test, predefined-agent shared/private read+write+KG-scope tests, open-agent/no-agent-type unaffected tests, `ListFiles` shared-doc merge tests, resolver-fallback + cached-resolver tests), `internal/bootstrap/seed_test.go` (extended — asserts the shared/private annotations and the `## Files` conventions are present in the embedded template), `internal/agent/inject_agent_type_test.go` (new — pins that the real `injectContext` puts the agent type into both the ctx key and the RunContext snapshot), `internal/gateway/bridge_context_test.go` (extended — bridge middleware injects agent type).
- **Why:** Memory docs are scoped per `(agent_id, user_id)`, and `MemoryUserID(ctx)` returns the chat user for predefined agents — so *everything* a predefined agent saved in chat landed per-user only. In production this meant one workspace member telling the agent "our launch is Sept 15" left every other member's chat with the same agent unaware of it. Workspace-level facts (company info, projects, decisions) need to be shareable across members; personal facts (people notes, daily logs) must stay private. Path-based routing in the interceptor is the minimal fix: no schema change, no new tool, and the existing per-path storage in `memory/` already matched the desired shared/private split — the interceptor just wasn't honoring it for predefined agents. The first deploy stored shared paths per-user on bridged (claude-cli/MCP) tool calls because only the agent loop's `injectContext` threaded `WithAgentType`; the store-backed resolver removes the dependency on any single entry path's ctx wiring.
- **Recovery grep:**
  ```
  grep -n "isSharedMemoryPath" internal/tools/memory_interceptor.go
  grep -n "NewCachedAgentTypeResolver" internal/tools/agent_type_resolver.go cmd/gateway_managed.go
  grep -n "WithAgentType" internal/gateway/server.go
  grep -n "shared with all workspace members" internal/bootstrap/templates/AGENTS.md
  grep -n "private to the member you're talking to" internal/bootstrap/templates/AGENTS.md
  ```
  Expects >=1 hit in each file.

### Patch 17 — `feat(skill-cli): static `skill` CLI + shared operation catalog`

- **Base upstream commit:** `d03d185f` (call_skill_service merge, PR #46 → dev)
- **Files:**
  - `internal/skillcatalog/catalog.go` (new) — the skill-service operation catalog extracted into a stdlib-only package as the single source of truth. Exports `Operation`, `Catalog`, `Lookup`, `OperationIDs`, `Description`, `BaseURL`. Verbatim move of the Phase-1 hand-curated 14-op set (enum/description output byte-identical).
  - `internal/tools/skill_service_catalog.go` — reduced to thin aliases (`type skillOperation = skillcatalog.Operation`, `var skillServiceCatalog = skillcatalog.Catalog`, and `catalogOperationIDs`/`catalogLookup`/`catalogDescription`/`skillServiceBaseURL` delegating to `skillcatalog`). `call_skill_service.go` and `call_skill_service_test.go` are unchanged — the native tool keeps its exact behavior.
  - `cmd/skill/main.go` (new) — the static CLI baked into the image at `/usr/local/bin/skill`. The code-context twin of `call_skill_service`: reads the runtime-injected `SKILL_RUNTIME_TOKEN` + `GOCLAW_WORKSPACE_ID`/`GOCLAW_USER_ID`/`GOCLAW_AGENT_ID` (no server-side mint available in an exec) and sets the same auth + identity headers. Commands: `skill ls`; `skill call <operation> [json]` (typed, catalog-driven, path-param fill + URL-escape, body from json arg or stdin); `skill raw <METHOD> <path> [--base URL] [--auth NAME] [--skill SLUG] [-H 'H: v']` (escape hatch for the untyped passthrough namespaces + jobs/crm/drive via `--base`/`--auth X-Workspace-Key`). Exit codes 0/1/2/3; upstream `{code,message}` body printed verbatim + a `[skill] HTTP <code>` line to stderr. Supersedes the unversioned shell `xskill` (which dies in a curl-blocked sandbox).
  - `Dockerfile` — builds `/out/skill` (CGO_ENABLED=0 static) in the Go builder stage and `COPY`s it to `/usr/local/bin/skill` in the Alpine runtime image, on PATH for every host-exec skill bash.
- **Why:** Phase 1 covered the chat-turn path (native tool) but a code job / skill bash has no tool loop, so it still hand-wrote curl/python (fragile: quoting, header block, URL assembly, hallucinated routes). The CLI gives those execs the same typed, auth-handled, route-constrained calling surface from one shared catalog, and survives the curl-blocked sandbox where a shell script cannot.
- **Recovery grep:**
  ```
  grep -n "package skillcatalog" internal/skillcatalog/catalog.go
  grep -n "skillcatalog.Catalog" internal/tools/skill_service_catalog.go
  grep -n "SKILL_RUNTIME_TOKEN" cmd/skill/main.go
  grep -n "/out/skill" Dockerfile
  ```
  Expects >=1 hit in each file.

### Patch 18 — `feat(skill-catalog): generated 43-op catalog + per-skill operation gating`

- **Base upstream commit:** `39accff7` (skill CLI merge, PR #47 → dev)
- **Files:**
  - `internal/skillcatalog/catalog.json` (new) — the operation catalog, GENERATED by x-api `npm run catalog:dump` (scripts/generate-skill-catalog.mjs, PR x-api#484) from the live TypeBox route schemas: 43 operations across 13 skills (was 14 hand-written ops / 7 skills). Regenerate + copy here on catalog changes; a listed op whose x-api route drifts fails generation.
  - `internal/skillcatalog/catalog.go` — the Go literal replaced by `//go:embed catalog.json` + parse-at-init (panics on malformed/empty). Adds `OperationIDsFor(allowed map[string]bool)` / `DescriptionFor(allowed)` (nil = full catalog) for per-agent pruning; fixes the `manage-view.set` InputHint to spell out `pills:[{text}]` (the bare-strings 400 loop).
  - `internal/tools/call_skill_service.go` — description preamble extracted to a const, `callSkillServiceParameters(enum)` builder, and exported `FilterCallSkillServiceDef(td, allowed)` which rebuilds a definition's Function wholesale for a pruned skill set (returns false when nothing remains).
  - `internal/agent/loop_tool_filter.go` — after the skill_manage hide: when `l.skillAllowList != nil`, prune call_skill_service's enum+description to `skillAllowList ∪ pinnedSkills`; drop the tool entirely when the agent has none of the catalog's skills. nil allow-list (lookup unavailable) fails open to the full catalog. Execution safety unchanged — Execute still validates ids and x-api enforces workspace scope.
  - Tests: `internal/skillcatalog/catalog_test.go` (new), `internal/tools/call_skill_service_test.go` (filter cases incl. shared-schema non-mutation), `internal/agent/skill_op_gate_test.go` (new — nil/pruned/pinned/drop cases).
- **Why:** the Phase 1 catalog was hand-curated and tiny; hand-curation drifts the moment x-api merges and leaves most high-traffic skill endpoints callable only via hand-written curl. Generation makes x-api's schemas the source of truth. Gating keeps the enlarged catalog from bloating every agent's tool description and stops agents being steered toward operations for skills they don't have.
- **Recovery grep:**
  ```
  grep -n "go:embed catalog.json" internal/skillcatalog/catalog.go
  grep -n "OperationIDsFor" internal/skillcatalog/catalog.go
  grep -n "FilterCallSkillServiceDef" internal/tools/call_skill_service.go internal/agent/loop_tool_filter.go
  ```
  Expects >=1 hit in each file.
