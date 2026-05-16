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
  - `internal/upgrade/version.go` — `RequiredSchemaVersion` bumped to
    `99000` so the migration runner picks the file up. Without this bump
    `CheckSchema` short-circuits as "up to date" because `current ==
    required (57)` and golang-migrate is never invoked.
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
  grep -nE 'RequiredSchemaVersion uint = 99000' internal/upgrade/version.go
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
