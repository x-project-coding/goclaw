# Phase 03 — v4 Greenfield PG Schema (000001_initial)

## Context Links

- Master § 3 (v4 Schema Final Shape), § 8 (Build & Migration Strategy)
- Decisions Q1-Q14, Q-A, Q-E, Q-10
- Phase 02 deliverable: `plans/260502-1635-v4-epic-04-schema/v3-baseline.md`
- Phase 04 (parallel SQLite work)

## Overview

- Priority: P0
- Status: completed (2026-05-02)
- Effort: 6 dev-days
- Description: Wipe v3 migrations (114 files), write fresh `migrations/000001_initial.up.sql` + `.down.sql`. 65 tables = 60 keep + 5 new (`users`, `user_sessions`, `skill_versions`, `curator_runs`, `user_hook_budget`). 1 rename (`sessions` → `agent_sessions`). All `tenant_id` columns dropped. All `user_id VARCHAR(255)` → `UUID FK users(id)`. Bump `RequiredSchemaVersion = 1`.

## Key Insights

- Q-E mandates single migration pair; reuse `golang-migrate/v4` file:// source unchanged.
- Q-14 — schema-from-scratch, no in-place upgrade; v3 → v4 deferred to EPIC-07.
- Q-10 — rename `sessions` → `agent_sessions` to avoid collision with new `user_sessions`.
- Q-7 — root user owns system defaults: `system_configs`/`config_secrets` PK = key only (no tenant_id).
- pgcrypto extension required for `digest()` (used by custom `uuid_generate_v7()` impl). pgvector required for memory + KG embeddings.
- 1131 tenant_id refs from v3 store layer; each must be reflected in schema by ABSENCE of column.
- Defer (per Q-14 audit): per-user vault encryption + activity_logs retention cron.

## Tests to write FIRST (TDD red step)

| Test file | Cases (must FAIL until impl green) |
|---|---|
| `tests/e2e/schema/01_pg_migration_round_trip_test.go` | `TestPgUpDown` — `migrate up → down → up` produces same schema (compare via `pg_dump --schema-only` checksums) |
| `tests/e2e/schema/02_pg_table_inventory_test.go` | `TestPgTableCount` — exactly 65 tables present after `up`. `TestPgRequiredTables` — assert each of 65 expected tables exists by name (pg_catalog query). `TestPgDroppedTables` — `tenants`, `tenant_users`, `skill_tenant_configs`, `builtin_tool_tenant_configs`, `tenant_hook_budget` MUST NOT exist |
| `tests/e2e/schema/03_pg_columns_test.go` | `TestNoTenantIDColumns` — query `information_schema.columns WHERE column_name='tenant_id'` returns 0. `TestUserIDIsUUID` — every `user_id`/`owner_user_id` column has type `uuid`. `TestSessionsRenamed` — table `sessions` does not exist; `agent_sessions` exists with same `session_key` column |
| `tests/e2e/schema/04_pg_fk_constraints_test.go` | `TestForeignKeysOnUsers` — `agents.owner_user_id`, `api_keys.owner_user_id`, `agent_teams.owner_user_id`, `cron_jobs.user_id` all have FK to `users(id)`. `TestNullableUserFKs` — `kg_entities.user_id`, `kg_relations.user_id`, `paired_devices.user_id`, `activity_logs.user_id`, `memory_documents.user_id` are NULLABLE |
| `tests/e2e/schema/05_pg_extensions_test.go` | `TestExtensions` — `pgcrypto` + `pgvector` enabled |
| `tests/e2e/schema/06_pg_indexes_test.go` | `TestUniqueEmail` — `users.email` has unique index. `TestVaultPathUnique` — `vault_documents` unique on `(scope, custom_scope, path, owner_user_id)` matches v3 semantics minus tenant. <!-- RED-TEAM Finding 3 --> `TestUsersOnlyOneRootIndex` — partial UNIQUE `users_only_one_root` on `(role) WHERE role='root'` exists. <!-- RED-TEAM Finding 4 --> `TestUserSessionsFamilyIndex` — index on `user_sessions(family_id)` exists |
| `tests/e2e/schema/07_pg_required_schema_version_test.go` | `TestRequiredSchemaVersion` — `internal/upgrade/version.go` constant = `1` |
| <!-- RED-TEAM Finding 4 --> `tests/e2e/schema/08_pg_user_sessions_family_test.go` | `TestUserSessionsFamilyIDColumn` — `user_sessions.family_id` is `uuid` type, NOT NULL |
| <!-- RED-TEAM Finding 12 --> `tests/e2e/schema/09_pg_uuid_v7_function_test.go` | `TestUUIDGenerateV7Exists` — `pg_proc` query confirms `uuid_generate_v7()` exists. `TestHotTablesUseV7Default` — `agent_sessions.id`, `traces.id`, `spans.id`, `memory_documents.id`, `memory_chunks.id` column_default = `uuid_generate_v7()` |
| <!-- RED-TEAM Finding 14 --> `tests/e2e/schema/10_pg_fk_set_null_test.go` | `TestCriticalFKsSetNull` — `agent_sessions.user_id`, `memory_documents.user_id`, `vault_documents.user_id`, `kg_entities.user_id`, `kg_relations.user_id` FK delete_rule = `SET NULL` (queried via `information_schema.referential_constraints`). `TestUsersDeletedAtColumn` — `users.deleted_at` column exists, type `timestamptz`, nullable |

**Red verification:** `go test -tags e2e ./tests/e2e/schema/...` fails (migrations dir empty / RequiredSchemaVersion=57). After impl: green.

## Requirements

### Functional

- DELETE all 114 v3 migration files in `migrations/` (228 .sql files = up+down pairs).
- CREATE `migrations/000001_initial.up.sql` (~1100 lines):
  - `CREATE EXTENSION IF NOT EXISTS pgcrypto;`
  - `CREATE EXTENSION IF NOT EXISTS vector;`
  - 65 `CREATE TABLE` statements grouped by domain (core/teams/sessions/memory/vault/skills/channels/cron/MCP/tracing/audit/hooks/evolution/llm).
  - <!-- RED-TEAM Finding 14: ON DELETE CASCADE blast radius (HIGH) -->
    **Critical user-data FKs use `ON DELETE SET NULL`, NOT CASCADE:** `agent_sessions.user_id`, `memory_documents.user_id`, `vault_documents.user_id`, `kg_entities.user_id`, `kg_relations.user_id`, `cron_jobs.user_id`, `paired_devices.user_id`, `activity_logs.user_id`. Orphaned rows survive a user delete and remain retrievable.
    Other FKs (`agent_team_members`, `team_user_grants`, `agent_shares`, `agent_links`, `user_context_files`, `user_agent_profiles`, `user_agent_overrides`, `mcp_user_grants`, `mcp_user_credentials`, `secure_cli_user_credentials`, `skill_user_grants`, `oauth_*`) MAY cascade since their existence is tied to the user.
    Add `users.deleted_at TIMESTAMPTZ NULL` column for soft-delete.
    Hard-cascade vacuum-after-N-days job DEFERRED to v4.x (out of scope here).
    <!-- /RED-TEAM Finding 14 -->
  - Indexes per v3 baseline (re-verified post tenant_id removal).
  - CHECK constraints preserved (e.g., `vault_documents.scope IN (...)` — Q-3 keeps `'custom'`).
  - <!-- RED-TEAM Finding 3: Bootstrap concurrent race — partial UNIQUE missing (CRITICAL) -->
    **Partial UNIQUE on root role:** after `users` table creation, add `CREATE UNIQUE INDEX users_only_one_root ON users(role) WHERE role='root';`. This is the DB-level atomicity guarantee Phase 06 bootstrap relies on. Without it, two parallel inits with different emails BOTH succeed.
    <!-- /RED-TEAM Finding 3 -->
  - <!-- RED-TEAM Finding 4: Refresh token theft — no family revocation (CRITICAL) -->
    **`user_sessions.family_id UUID NOT NULL`:** add `family_id` column (NOT NULL). Token family for theft detection per RFC 6749 §10.4. Every rotation within a chain inherits the same `family_id` from its parent session. Theft detection (Phase 06) revokes the entire family in a single UPDATE: `UPDATE user_sessions SET revoked_at=NOW() WHERE family_id=$1`.
    Schema: `family_id UUID NOT NULL` + index `CREATE INDEX user_sessions_family_idx ON user_sessions(family_id);`.
    <!-- /RED-TEAM Finding 4 -->
- CREATE `migrations/000001_initial.down.sql` (~80 lines):
  - `DROP TABLE IF EXISTS ... CASCADE` for all 65 tables, reverse-FK order.
  - `DROP EXTENSION` not included (system-wide; safer to leave).
- BUMP `internal/upgrade/version.go` `RequiredSchemaVersion = 1`.
- DEFER: per-user vault encryption (Q-14 audit) — ADR doc only, no schema field.

### Non-functional

- File length policy waiver: schema migration intentionally large (~1100 lines OK; trades modularity for atomicity).
- Round-trip safety: `up → down → up` deterministic; no data leakage between cycles.
- Idempotent on fresh DB: `CREATE TABLE IF NOT EXISTS` everywhere.
- Statement order respects FK dependencies (parent tables first: `users` → all FKs depending on it).

<!-- RED-TEAM Finding 12: uuid_generate_v7() → gen_random_uuid() silent regression (CRITICAL) -->
## UUID Generation Strategy

**Restore v3's `uuid_generate_v7()` PG function — DO NOT switch to `gen_random_uuid()`.**

- v3 defines a custom `uuid_generate_v7()` SQL function in `migrations/000001_init_schema.up.sql:8` (UUID v7 — time-ordered).
- v4 must copy this function verbatim into `migrations/000001_initial.up.sql` (after extension creation, before table CREATE statements).
- **ALL tables MUST default `id` to `uuid_generate_v7()`** — no exceptions, no `gen_random_uuid()` anywhere (user lock V4 2026-05-02 17:37: "toàn bộ phải là uuid v7 khi phát sinh data"):
  - Hot-write: `agent_sessions`, `traces`, `spans`, `memory_documents`, `memory_chunks`, `kg_entities`, `kg_relations`, `episodic_summaries`, `cron_run_logs`, `heartbeat_run_logs`, `hook_executions`
  - Cold tables: `users`, `agents`, `agent_teams`, `skills`, `system_configs`, `user_sessions`, `paired_devices`, `vault_documents`, `mcp_servers` — **all use `uuid_generate_v7()` for consistency**
- **Rationale:** UUID v7 is time-ordered → B-tree locality on insert → minimal page splits + write amplification. UUID v4 (random) regresses ingestion latency by ~30-50% on hot tables. User mandates universal v7 for fleet-wide consistency.
- **Go code:** All ID generation uses `uuid.NewV7()` (NOT `uuid.New()` which returns v4). Required: `github.com/google/uuid` v1.6+ in `go.mod`.
- **SQLite:** SQLite has no native v7 → schema columns have NO DEFAULT; Go layer generates v7 at INSERT time via `uuid.NewV7()` and passes as parameter.
- **family_id** (`user_sessions` per Finding 4) also uses v7. **merge_audit** (per Finding 7) uses v7 if has its own UUID column.
<!-- /RED-TEAM Finding 12 -->

## Architecture

```
v4 PG schema layout (domain → tables):
  Core (8):       users[NEW], user_sessions[NEW], agents, agent_shares,
                  agent_context_files, user_context_files,
                  user_agent_profiles, user_agent_overrides
  API/Links (2): api_keys, agent_links
  Teams (7):      agent_teams, agent_team_members, team_user_grants,
                  team_tasks, team_task_comments, team_task_events,
                  team_task_attachments
  Sessions (1):   agent_sessions[RENAMED from sessions]
  Memory (4):     memory_documents, memory_chunks, embedding_cache,
                  episodic_summaries
  KG (3):         kg_entities, kg_relations, kg_dedup_candidates
  Vault (3):      vault_documents, vault_links, vault_versions
  Skills (5):     skills, skill_agent_grants, skill_user_grants,
                  skill_versions[NEW], curator_runs[NEW]
  Channels (5):   channel_instances, channel_pending_messages,
                  channel_contacts, pairing_requests, paired_devices
  Cron (2):       cron_jobs, cron_run_logs
  Heartbeat (2):  agent_heartbeats, heartbeat_run_logs
  MCP (5):        mcp_servers, mcp_agent_grants, mcp_user_grants,
                  mcp_access_requests, mcp_user_credentials
  Tracing (2):    traces, spans
  Tools (4):      builtin_tools, secure_cli_binaries,
                  secure_cli_agent_grants, secure_cli_user_credentials,
                  subagent_tasks
  Audit (4):      activity_logs, system_configs, config_secrets,
                  usage_snapshots
  Hooks (4):      hooks, hook_agents, hook_executions, user_hook_budget[NEW]
  Evolution (2):  agent_evolution_metrics, agent_evolution_suggestions
  LLM (1):        llm_providers
TOTAL: 65 (verified vs decisions.md L74-145 + master § 3)
```

## Related Code Files

### Create

- `/Users/viettran/Documents/coding/next-level-builder/goclaw/migrations/000001_initial.up.sql`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/migrations/000001_initial.down.sql`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/tests/e2e/schema/01_pg_migration_round_trip_test.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/tests/e2e/schema/02_pg_table_inventory_test.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/tests/e2e/schema/03_pg_columns_test.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/tests/e2e/schema/04_pg_fk_constraints_test.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/tests/e2e/schema/05_pg_extensions_test.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/tests/e2e/schema/06_pg_indexes_test.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/tests/e2e/schema/07_pg_required_schema_version_test.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/docs/adr/2026-05-v4-vault-no-encryption-defer.md` (ADR explaining Q-14 audit defer)
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/docs/adr/2026-05-v4-vault-custom-scope-reserved.md` (ADR for LOG-2)

### Modify

- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/upgrade/version.go` (`RequiredSchemaVersion = 1`)

### Delete

- `/Users/viettran/Documents/coding/next-level-builder/goclaw/migrations/000001_init_schema.up.sql` ... `migrations/000057_*.up.sql` (114 files = 57 pairs)
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/migrations/000001_init_schema.down.sql` ... `migrations/000057_*.down.sql`

### Read for context

- Phase 02 `v3-baseline.md` (catalogue + FK graph)
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/store/sqlitestore/schema.sql` (structural reference; types differ but column intent same)

## Implementation Steps

1. Verify Phase 02 baseline doc finalized + reviewed.
2. Verify branch `dev-v4` clean (no v3 work in flight).
3. Write all 7 schema test files FIRST (red — they fail because migrations not applied yet). Each covers a specific assertion (table count, FK, NULL semantics, etc.).
4. Run `go test -tags e2e ./tests/e2e/schema/...` → confirm red.
5. `git rm migrations/000001_*.up.sql migrations/000001_*.down.sql ... migrations/000057_*.up.sql migrations/000057_*.down.sql` — delete all 114 v3 files in one commit.
6. Author `migrations/000001_initial.up.sql`:
   a. Header: extensions + comment block citing Phase 02 doc + Q-decisions.
   <!-- RED-TEAM Finding 12 -->
   a2. After extensions, COPY `uuid_generate_v7()` SQL function from v3 `migrations/000001_init_schema.up.sql:8` verbatim. All hot-write tables (`agent_sessions`, `traces`, `spans`, `memory_documents`, `memory_chunks`, `kg_entities`, `kg_relations`, `episodic_summaries`, `cron_run_logs`, `heartbeat_run_logs`, `hook_executions`) use `DEFAULT uuid_generate_v7()`. Cold tables also use v7 for consistency.
   <!-- /RED-TEAM Finding 12 -->
   b. Section 1 — Core: `users` (id UUID PK DEFAULT uuid_generate_v7(), email UNIQUE NOT NULL, display_name, password_hash TEXT, role VARCHAR(20) NOT NULL DEFAULT 'member', status VARCHAR(20) DEFAULT 'active', <!-- RED-TEAM Finding 14 --> deleted_at TIMESTAMPTZ NULL, metadata JSONB DEFAULT '{}', created_at TIMESTAMPTZ DEFAULT NOW(), updated_at TIMESTAMPTZ DEFAULT NOW()) + CHECK role IN ('root','admin','member','viewer').
   <!-- RED-TEAM Finding 3 -->
   b2. After `users` table: `CREATE UNIQUE INDEX users_only_one_root ON users(role) WHERE role='root';` — partial UNIQUE for bootstrap atomicity.
   <!-- /RED-TEAM Finding 3 -->
   c. Section 1 cont — `user_sessions` (id UUID PK DEFAULT uuid_generate_v7(), user_id UUID FK ON DELETE CASCADE, <!-- RED-TEAM Finding 4 --> family_id UUID NOT NULL, <!-- /RED-TEAM Finding 4 --> refresh_token_hash TEXT NOT NULL UNIQUE, expires_at TIMESTAMPTZ NOT NULL, revoked_at TIMESTAMPTZ NULL, created_at). Add `CREATE INDEX user_sessions_family_idx ON user_sessions(family_id);` (Finding 4 — fast family-revoke).
   d. Sections 2-14 — domain by domain per architecture diagram. For each existing v3 table: copy CREATE TABLE from v3 schema, drop tenant_id column, swap user_id type to UUID, add owner_user_id FK where Q-decisions mandate. <!-- RED-TEAM Finding 14 --> Use `ON DELETE SET NULL` for: `agent_sessions.user_id`, `memory_documents.user_id`, `vault_documents.user_id`, `kg_entities.user_id`, `kg_relations.user_id`, `cron_jobs.user_id`, `paired_devices.user_id`, `activity_logs.user_id`. Other FKs may CASCADE.
   e. Section "RENAME": `agent_sessions` table (was v3 `sessions`). Same columns minus tenant_id, plus `user_id UUID NULL` FK `ON DELETE SET NULL` (Finding 14).
   f. Indexes — re-derive each from v3 schema, dropping any tenant_id partial indexes; add unique on (`users.email`).
7. Author `migrations/000001_initial.down.sql` — drops in FK-reverse order; uses `DROP TABLE IF EXISTS ... CASCADE`.
8. Bump `internal/upgrade/version.go` `RequiredSchemaVersion = 1`.
9. Write 2 ADR docs (`docs/adr/...`) per LOG-2 + Q-14 audit defer.
10. Run `migrate up` against e2e DB → run all 7 schema tests → expect green.
11. Run `migrate down` → schema empty → `migrate up` again → re-run tests → green (round-trip).
12. `go vet ./...` + `go build ./...` (PG default build) — clean.
13. Commit: `feat(schema): v4 PG initial migration (000001_initial)` + amend with deletion of v3 files.

## Todo List

- [x] Phase 02 baseline reviewed + signed off
- [x] 10 schema test files written (red verified) — split 7 base + Findings 4/12/14
- [x] Delete 114 v3 migration files
- [x] migrations/000001_initial.up.sql (1396 lines, 66 CREATE TABLE)
- [x] migrations/000001_initial.down.sql (116 lines)
- [x] Bump RequiredSchemaVersion to 1
- [x] ADR docs (vault-no-encryption, vault-custom-scope)
- [x] All 20 PG schema tests green
- [x] Round-trip up→down→up green
- [x] go vet + go build clean (default + e2e + sqliteonly)

## Completion log

- **Date:** 2026-05-02
- **Commit:** `fabc2a61` — feat(schema): v4 greenfield PG initial migration (000001_initial)
- **Reviewer:** code-reviewer score 8/10, all 5 critical RED-TEAM findings (F3 partial UNIQUE root, F4 family_id, F7 merge_audit, F12 uuid_v7 universal, F14 ON DELETE SET NULL ×8) verified inline
- **Reviewer report:** `plans/reports/code-reviewer-260502-1815-phase03-04-schema.md`
- **Test count:** 20/20 PG schema tests green; round-trip up→down→up identical schema
- **Verified counts:** 60 `uuid_generate_v7()` defaults, 0 `gen_random_uuid()`, 46 `ON DELETE SET NULL`, 6 `vector(1536)` columns (dimension consistent w/ v3)
- **Note for Phase 05:** PG=66 tables (plan said 65; Tools section labeled "(4)" but listed 5 items — agent counted correctly; test 02 expects 66)
- **Note for Phase 05:** ADR-locked `vault_documents.scope IN ('personal','team','shared','custom')` — NOT the values the plan prose mentioned; trust ADR `2026-05-v4-vault-custom-scope-reserved.md`

## Success Criteria

- 7 schema tests green.
- `pg_dump --schema-only` produces stable output across `up→down→up` cycles (checksum match).
- 0 tables match `tenant%`; 65 tables exist post-up.
- 0 columns match `column_name='tenant_id'`.
- All `user_id`/`owner_user_id` columns are `uuid` type.
- `users.email` unique constraint present.
- `RequiredSchemaVersion = 1`.

## Risk Assessment

| Risk | Severity | Mitigation |
|---|---|---|
| FK ordering wrong → migration fails partial | High | Topological sort of CREATE TABLE statements: `users` first; tables referencing only `users` next; etc. |
| Missing index causes slow queries post-launch | Med | Phase 14 adds indexed query benchmarks (smoke level, NOT load test) |
| pgvector dimension drift between v3 and v4 | Med | Match v3 `vector(1536)` for memory + KG; document in migration comments |
| Hidden v3 column lost in copy-paste | Med | Phase 02 catalogue is source of truth; pair-review checklist per table |
| 114 v3 file delete blocks downstream `git bisect` | Low | Single commit "DROP v3 migrations" before "ADD 000001_initial" — bisectable |
| Greenfield breaks any v3 e2e fixtures | Med | v3 fixtures use `tests/integration/`, v4 uses `tests/e2e/` — segregated |

## Security Considerations

- `pgcrypto` provides `digest()` building block. `uuid_generate_v7()` defined via SQL function (copied verbatim from v3 `migrations/000001_init_schema.up.sql:8`); used as DEFAULT for ALL `id` columns. **No `gen_random_uuid()` anywhere** (user lock V4 — universal v7).
- `users.password_hash TEXT NOT NULL` — Argon2id encoded string (Phase 06 enforces format).
- `user_sessions.refresh_token_hash` — opaque 32-byte random hex hashed (sha256), NEVER store raw refresh tokens (Phase 06).
<!-- RED-TEAM Finding 4 -->
- `user_sessions.family_id UUID NOT NULL` — token family for theft detection (RFC 6749 §10.4). Phase 06 propagates parent's family_id on rotation; revokes entire family on theft signal.
<!-- /RED-TEAM Finding 4 -->
<!-- RED-TEAM Finding 14 -->
- Critical user-data FKs use `ON DELETE SET NULL` (NOT CASCADE) — `agent_sessions`, `memory_documents`, `vault_documents`, `kg_entities`, `kg_relations`, `cron_jobs`, `paired_devices`, `activity_logs`. Single rogue admin or SQLi `DELETE FROM users` no longer destroys orphaned rows. Soft-delete via `users.deleted_at` is preferred path; hard-cascade vacuum job deferred to v4.x.
<!-- /RED-TEAM Finding 14 -->
<!-- RED-TEAM Finding 3 -->
- `users_only_one_root` partial UNIQUE index — DB-level atomicity guarantee for bootstrap. Phase 06 bootstrap relies on this index + `pg_advisory_xact_lock` to prevent multi-root creation under concurrent POST `/v1/bootstrap/init`.
<!-- /RED-TEAM Finding 3 -->
- No tenant boundary in schema → all rows accessible to root/admin (intentional, Q-14 audit "trust admin model").

## Cross-phase Gates

- **Entry:** Phase 01 + Phase 02 merged + green. Branch `dev-v4` clean.
- **Exit:** All schema tests green (7 original + 3 new from Findings 3/4/12/14) + go vet/build clean. Gates Phase 05 (stores) and Phase 06 (bootstrap relies on `users_only_one_root` + `family_id`).
<!-- RED-TEAM Finding 7: schema ripple from Phase 09 -->
- **Schema ripple:** Phase 09 (Finding 7) adds `channel_contacts.merge_audit JSONB` column. Either: (a) include in this phase's `000001_initial.up.sql` now, OR (b) add as a follow-up migration `000002_add_merge_audit.up.sql` introduced in Phase 09. Decision: add to `000001_initial.up.sql` here (cheaper than introducing 2nd migration mid-cycle).
<!-- /RED-TEAM Finding 7 -->

## Next Steps

- Phase 04 (SQLite schema) — runs parallel; same logical schema, SQLite types.
- Phase 05 (stores refactor) — consumes schema as source-of-truth column list.
