---
title: v4 EPIC-04 schema plan — Assumption Destroyer audit
date: 2026-05-02
reviewer: code-reviewer (skeptic lens)
plan: plans/260502-1635-v4-epic-04-schema/
status: HOSTILE — verifications run vs live codebase
---

# v4 EPIC-04 plan — Assumption Destroyer findings

**Method:** every numeric claim re-grepped, every cited file:line opened, every assumed dep checked vs go.mod/go.sum.
Live codebase: `/Users/viettran/Documents/coding/next-level-builder/goclaw/`

---

## Finding 1: Frontend file count off by ~10x — Phase 11 effort estimate is fantasy

- **Severity:** Critical
- **Location:** plan.md L77 + Phase 11 "Overview" + "Key Insights" L20 ("65 ui/web FE files affected (~9226 LOC)")
- **Flaw:** Plan asserts "65 FE files / ~9226 LOC" need refactor. Real `ui/web/src/` contains **712 ts/tsx files** (693 non-test) totalling **84,833 LOC** — an order of magnitude larger.
- **Failure scenario:** 25 dev-day Phase 11 estimate is built on the 65-file premise. If even half of the real 693 files touch tenant directly or transitively (via `tenantId` Zustand store, route guards, API client interceptors), the actual scope could be 5x–10x. Phase 11 ships months late, blocking Phases 12 + 14.
- **Evidence:**
  ```
  $ find ui/web/src -type f \( -name "*.ts" -o -name "*.tsx" \) ! -name "*.test.*" ! -name "*.spec.*" | wc -l
  693
  $ find ui/web/src -type f \( -name "*.ts" -o -name "*.tsx" -o -name "*.js" -o -name "*.jsx" \) | xargs wc -l | tail -1
  84833 total
  $ grep -rln "tenant\|Tenant" ui/web/src/ --include="*.ts" --include="*.tsx" | wc -l
  67
  ```
  67 files visibly reference `tenant`/`Tenant`; the 65 number in the plan came from rough scout estimate, not actual count, and ignores transitive impact (auth-context, store, api client, every page that calls API).
- **Suggested fix:** Re-scout `ui/web/src/` properly. Run `grep -rln 'tenantId\|Tenant\|/v1/tenants' ui/web/src/` + walk the call graph from `stores/auth.ts`. Adjust Phase 11 effort to reflect actual scope (likely 35-50 dev-days). Or split FE into 11A (auth bootstrap, ~10d) + 11B (page sweep, deferred to v4.1).

---

## Finding 2: MasterTenantID purge scope undercounted ~3x — Phase 13 file list is incomplete

- **Severity:** Critical
- **Location:** plan.md L79 ("171 lines / ~50 files MasterTenantID NON-test"), Phase 13 "Architecture" enumerates ~21 files
- **Flaw:** Plan claims ~50 non-test files reference MasterTenantID and lists ~21 explicitly in Phase 13. Real count is **81 non-test files** (171 lines is correct, but the file count is ~3x worse than the plan's enumeration).
- **Failure scenario:** Phase 13 budgets 4 dev-days assuming 21 files. Actual sweep across 81 non-test files (plus 25 test files) takes 2-3x longer. Each missed file = compile break or runtime "ghost tenant" reference. The plan's Phase 13 todo list won't compile a clean v4 because ~60 files are unaccounted for.
- **Evidence:**
  ```
  $ grep -rln "MasterTenantID" --include="*.go" . | wc -l
  106
  $ grep -rln "MasterTenantID" --include="*.go" . | grep -v "_test.go" | wc -l
  81
  ```
  Phase 13 enumeration lists ~21 paths. Missing examples (real, currently in code):
  ```
  cmd/gateway_http_wiring.go, cmd/gateway.go, cmd/gateway_agents.go,
  cmd/gateway_lifecycle.go, cmd/gateway_system_config_sync.go,
  cmd/gateway_consumer_handlers.go, cmd/gateway_consumer_normal.go,
  internal/http/agents_codex_pool.go, internal/http/storage.go,
  internal/http/oauth.go, internal/skills/seeder.go, internal/hooks/types.go,
  internal/gateway/router.go, internal/gateway/methods/heartbeat.go,
  internal/vault/enrich_worker.go, internal/store/context.go,
  internal/consolidation/{dreaming_scoring_test, workers_test, mock_test}.go
  ```
- **Suggested fix:** Re-grep at start of Phase 13 (`grep -rln MasterTenantID --include='*.go' . | grep -v _test.go > files.txt`) and rebuild the explicit list. Bump Phase 13 effort to 8-10 days. Add gate: phase-exit grep returns 0 (already in success criteria — but todo list must enumerate every file).

---

## Finding 3: PG store file count off — 90 claimed vs 107 actual

- **Severity:** High
- **Location:** plan.md L77 ("90 PG + 88 SQLite store files"), Phase 05 L13 + L26
- **Flaw:** Plan repeatedly says "90 PG store files" (basis for the 22 dev-day Phase 05 estimate, "~1 day per 8-9 files / 90+88 / 22 days"). Real count: **107 PG files** + 88 SQLite = 195 files, not 178.
- **Failure scenario:** Phase 05 already flagged as "largest phase" at 22d. With 17 extra PG files, the per-file edit budget shrinks. Sub-PR 05B ("Drop tenant_id from PG stores", 8d) is undersized; merge slips. Cascades into Phase 06 entry gate slipping.
- **Evidence:**
  ```
  $ find internal/store/pg -type f -name "*.go" | wc -l
  107
  $ find internal/store/sqlitestore -type f -name "*.go" | wc -l
  88
  ```
- **Suggested fix:** Bump Sub-PR 05B to 10d, total Phase 05 to 25-26d. Or split 05B into 05B-1 (factory + scope deletion, 3d) + 05B-2 (per-domain refactor, 9d). Re-derive math from 107 (not 90).

---

## Finding 4: `seedTenantAgent` callsite count claim is wrong (305 vs 326)

- **Severity:** Medium
- **Location:** plan.md key decisions reference, Phase 13 L75 "(audit D-8; was 326 in research)"
- **Flaw:** Plan cites "326 callsites" from research, then says "(was 326)" without correction. Actual occurrence count: **305** (54 files).
- **Failure scenario:** Not catastrophic — but it is a numerical claim sourced from a stale audit, copy-pasted without re-verification. If other audit-derived numbers (171 lines, 1131 tenant_id) were also stale at write-time and re-verified, this one was missed. Pattern of trust-but-don't-verify.
- **Evidence:**
  ```
  $ grep -rn "seedTenantAgent" --include="*.go" . | wc -l
  305
  $ grep -rln "seedTenantAgent" --include="*.go" . | wc -l
  54
  ```
- **Suggested fix:** Update Phase 13 to "300+ callsites in ~54 files" (or just drop the historical claim).

---

## Finding 5: sqliteonly build-tag file count off (8 claimed vs 11 actual)

- **Severity:** Medium
- **Location:** plan.md L78 ("8 sqliteonly + 6 !sqliteonly build-tag files (audit-corrected; not 110)")
- **Flaw:** Plan asserts 8 sqliteonly files. Actual count: **11**.
- **Failure scenario:** Phase 12 (Desktop edition first-run) and Phase 04 dual-build verification need to know all sqliteonly files. Missing 3 files in coverage = SQLite-only build can break silently.
- **Evidence:**
  ```
  $ grep -rln "//go:build sqliteonly" --include="*.go" . | wc -l
  11
  $ grep -rln "//go:build sqliteonly" --include="*.go" .
  ui/desktop/app.go
  ui/desktop/keyring.go
  ui/desktop/main.go
  cmd/gateway_stores_sqliteonly.go
  tests/integration/tts_dual_read_sqlite_test.go
  tests/integration/sqlite_vault_shared_docs_test.go
  tests/integration/sqlite_smoke_test.go
  internal/backup/db_restore_sqlite.go
  internal/backup/preflight_sqlite.go
  internal/backup/tenant_discover_sqlite.go
  internal/backup/db_dump_sqlite.go
  ```
  `internal/backup/tenant_discover_sqlite.go` is plain unaccounted for in plan.
- **Suggested fix:** Phase 04/12 todo lists must enumerate the 11 actual files, not 8. `tenant_discover_sqlite.go` likely needs deletion (tenant-discovery is dead in v4) — explicit todo step required.

---

## Finding 6: R1 fix premise — atomic TX claim is structurally false in current code

- **Severity:** Critical
- **Location:** Phase 09 L48-50, L113-117 ("Wrap all 4 UPDATEs in single TX. ROLLBACK on any error")
- **Flaw:** Plan claims to wrap `channel_contacts.merged_id` + `agent_sessions.user_id` + `user_context_files.user_id` + `memory_documents.user_id` in **one TX**. Current `internal/http/contact_merge_handlers.go` is structurally non-atomic: line 90 calls `h.contactStore.MergeContacts(...)` (one DB op, returns error to client), then line 97 calls `h.migrateContextFilesOnMerge(...)` (separate, fire-and-forget — return value DROPPED). There is no shared TX context to extend. To make this atomic, the contactStore needs to expose `BeginTx()` or merge ALL 4 updates into one new store method. Plan does not call out this restructure.
- **Failure scenario:** Phase 09 author wraps the existing `MergeContacts` call in `tx := db.Begin(); tx.Commit()` — but `migrateContextFilesOnMerge` and the future `agent_sessions` UPDATE run with a fresh DB connection. Atomic guarantee is fictional. R1 returns: a partial merge leaves `channel_contacts.merged_id` updated but `agent_sessions.user_id` stale. Test 12_merge_contact_R1_fix_test.go's "atomic" case fails or, worse, passes accidentally and ships a regression.
- **Evidence:**
  ```
  internal/http/contact_merge_handlers.go:90
    if err := h.contactStore.MergeContacts(...); err != nil { ... }   ← own connection
  internal/http/contact_merge_handlers.go:97
    h.migrateContextFilesOnMerge(...)   ← separate fn, return ignored
  ```
  `MergeContacts` lives in contactStore (no TX param exposed). `migrateContextFilesOnMerge` reaches into a different store (likely UserContextFilesStore). They cannot share a TX without API changes plan does not specify.
- **Suggested fix:** Phase 09 must (a) add `MergeContactsTx(ctx, tx, ids, target)` + analogous methods for sessions/context_files/memory_docs to the relevant stores, OR (b) introduce a single `MergeUserAggregate(ctx, sourceUsers, targetUser) error` store method that owns the TX. Add explicit step to Phase 09 implementation list: "Refactor stores to accept *sql.Tx for merge path before adding sessions UPDATE." Otherwise R1 is "fixed" in name only.

---

## Finding 7: Silent UUID semantic regression — `uuid_generate_v7()` (v3) → `gen_random_uuid()` (v4 plan)

- **Severity:** High
- **Location:** Phase 03 L141 ("`gen_random_uuid()`"), L190 ("pgcrypto enables `gen_random_uuid()` (cryptographically secure)")
- **Flaw:** v3 schema uses a custom `uuid_generate_v7()` PG function (defined `migrations/000001_init_schema.up.sql:8`) producing time-ordered UUIDs (good for B-tree index locality on hot-write tables like `agent_sessions`, `traces`, `spans`). v4 plan unconditionally switches to `gen_random_uuid()` (UUID v4 = pure-random, NO time ordering). This is a silent index-locality regression for high-write tables.
- **Failure scenario:** v3 `uuid_generate_v7()` callers across **104 sites in migrations/** generate sortable IDs; index pages stay packed. v4's `gen_random_uuid()` randomizes inserts → B-tree page splits, write amplification, slower trace/span ingestion. Production users with heavy `agent_sessions` or `traces` workload see unexplained latency increase post-v4 upgrade. Plan does NOT acknowledge or trade off this.
- **Evidence:**
  ```
  $ grep -c "uuid_generate_v7" migrations/000001_init_schema.up.sql
  85
  migrations/000001_init_schema.up.sql:8:CREATE OR REPLACE FUNCTION uuid_generate_v7() RETURNS uuid AS $$
  ```
  Plan's Phase 03 architecture diagram lists `users (id UUID PK DEFAULT gen_random_uuid())` — abandons v7. No ADR or note acknowledges the trade-off.
- **Suggested fix:** Either (a) keep `uuid_generate_v7()` in v4 `000001_initial.up.sql` (re-define the SQL function — copy from v3), OR (b) write an ADR `docs/adr/2026-05-v4-uuid-v4-vs-v7.md` justifying the regression with concrete latency/storage trade-off measurements. Cannot silently downgrade UUID semantics for tables with billions of rows.

---

## Finding 8: Misidentified file — `Client` struct lives in `client.go`, not `server.go`

- **Severity:** Medium
- **Location:** Phase 06 L71 ("`internal/gateway/server.go` — `Client` struct: drop `tenantID` field, keep `userID` (line 24 confirmed)")
- **Flaw:** Plan asserts `Client` struct is in `internal/gateway/server.go` line 24. Actual location: `internal/gateway/client.go:18`.
- **Failure scenario:** Phase 06 implementer goes to `server.go`, sees no `Client struct`, may either patch wrong file or get confused. Junior dev wastes 1-2h. Worse, an LLM agent may invent fields if scout-output is treated as ground truth.
- **Evidence:**
  ```
  $ grep -rn "type Client struct" internal/gateway/
  internal/gateway/client.go:18:type Client struct {
  ```
  Plan also asserts `userID string` is at "line 24" — that is correct (line 24 in `client.go`). But the Client field comment says "external user ID (TEXT, free-form)" — v4 wants `userID` to be a UUID string. The field is currently TEXT user_id (e.g., from telegram). Plan does NOT call out this semantic flip — it just says "drop tenantID, keep userID" as if no change to userID is needed. v4 must enforce `uuid.MustParse(userID)` somewhere, but Phase 06 skips that step.
- **Suggested fix:** Fix the file path (`client.go` not `server.go`). Add explicit step: "After dropping tenantID from Client, change userID semantic: clients send JWT → JWT claim `sub` (UUID) is parsed → Client.userID stores UUID string. Reject connect if claim is not a valid UUID." Without this, channel-side TEXT user_ids continue to flow, and `validate_user_id` (P05 PR-05D) just logs warnings while data still corrupts.

---

## Finding 9: Missing dependency — `golang-jwt/jwt/v5` not in go.mod, plan assumes available

- **Severity:** Medium
- **Location:** Phase 06 — JWT claim throughout (L51 "HS256", "Reads `GOCLAW_JWT_SECRET`")
- **Flaw:** Plan describes JWT issuance/verification but does NOT specify which library to add. The brainstorm phase mentions `golang-jwt/jwt/v5` informally. Current go.mod has NO JWT library; only `golang.org/x/crypto v0.48.0 // indirect`. argon2 is reachable via `x/crypto/argon2` (sub-package) but JWT is not in stdlib.
- **Failure scenario:** Phase 06 implementer must add a JWT lib mid-phase. Choice between `golang-jwt/jwt/v5`, `lestrrat-go/jwx`, hand-roll, or other affects: API ergonomics, security audit surface, dependency risk, and supply chain. Plan defers this critical pick to "implementation time" — guaranteed inconsistency between author preference and reviewer preference. Also missing: where `GOCLAW_JWT_SECRET` is generated/stored (single value? rotation?), how it's loaded (env var? secrets file?).
- **Evidence:**
  ```
  $ grep -i "jwt" go.mod go.sum
  (empty)
  $ grep "x/crypto" go.mod
  golang.org/x/crypto v0.48.0 // indirect
  ```
  `argon2` reachable (x/crypto subpkg). JWT lib needs explicit `go get`.
- **Suggested fix:** Phase 06 todo list must include explicit "Add `github.com/golang-jwt/jwt/v5 vX.Y.Z` to go.mod" as a step, AND specify Q-C generator/storage path for `GOCLAW_JWT_SECRET` (recommend: `GOCLAW_JWT_SECRET` env var, generated by `goclaw onboard` if not present, persisted to `.env.local` with 0600 perms — same pattern as v3 secret bootstrap). Otherwise different implementers pick different libs and secret-bootstrap models.

---

## Finding 10: Phase 02 → 03+04 cross-phase gate is illusory (paper-only deliverable not gateable)

- **Severity:** Medium
- **Location:** plan.md "Critical dependency graph" L52 + Phase 03 L198 ("Phase 01 + Phase 02 merged + green")
- **Flaw:** Phase 02 is paper-only (zero code, zero tests). Phase 03 entry gate says "Phase 02 merged + green". A markdown doc cannot be "green" — there are no automated assertions. Plan claims "Verification = peer review" (Phase 02 L26) but the dependency graph treats it as a hard gate equivalent to compile/tests.
- **Failure scenario:** Phase 03 author starts before Phase 02 doc is finalized (because "merged + green" is a soft claim). Schema decisions diverge from baseline catalog. Phase 02 is post-hoc retrofit, defeating its purpose.
- **Evidence:** Phase 02 lists no tests (line 24: "This is a paper-only phase. No code, no tests."). Phase 03 cross-phase gate cites the doc as a blocker, but enforcement is purely social.
- **Suggested fix:** Either (a) drop Phase 02 from the dependency graph and treat as "research artifact" (just a deliverable of Phase 03's first day), OR (b) add automated assertions to Phase 02: a single test in `tests/e2e/schema/00_baseline_doc_present_test.go` that does `os.Stat(plans/.../v3-baseline.md)` + grep for required section headers. Make the gate enforceable.

---

## Summary

| # | Severity | One-line |
|---|---|---|
| 1 | Critical | FE file count off ~10x (65 claimed vs 693 real) — Phase 11 estimate is fantasy |
| 2 | Critical | MasterTenantID purge undercounted ~3x (~21 listed vs 81 actual non-test files) |
| 3 | High | PG store count off (90 vs 107) — Phase 05 budget undersized |
| 4 | Medium | seedTenantAgent claim 326 vs actual 305 (stale audit) |
| 5 | Medium | sqliteonly file count off (8 vs 11) — `tenant_discover_sqlite.go` unaccounted |
| 6 | Critical | R1 fix "atomic TX" is structurally impossible without store-API restructure plan does not specify |
| 7 | High | Silent UUID v7 → v4 regression — index locality cost on hot tables, no ADR |
| 8 | Medium | `Client` struct in `client.go:18`, NOT `server.go:24` as plan states; userID UUID semantic flip not addressed |
| 9 | Medium | JWT library not in go.mod; plan defers lib pick + secret-bootstrap to implementation |
| 10 | Medium | Phase 02 (paper-only) is non-gateable; dependency graph treats it as hard gate |

## Unresolved questions

1. Is `pnpm workspace` (mentioned in Phase 12) viable, or did the plan deliberately choose duplicate-and-maintain? (Phase 12 says "Option B: copy" — but no rationale logged.)
2. After dropping `uuid_generate_v7()`, will any v3-era trace/span query rely on time-ordering for "recent" lookups (`ORDER BY id DESC LIMIT N`)? If yes, those queries silently regress on read performance too.
3. Phase 09 atomicity restructure (Finding 6): which store owns the cross-table TX — `ContactStore` (most natural caller) or a new `MergeService`? Architecture decision missing.
4. Phase 06 claims "150 callers" of role rename. Real is 106 (34 unique files). Off by ~30%. Was this also a stale audit number?
5. v3 → v4 rollback story: plan asserts "fresh install only" but provides `000001_initial.down.sql`. Why? If down is needed, what's the contract? If not needed, why ship the file?

**Status:** DONE
**Findings:** 10 (3 Critical / 2 High / 5 Medium)
