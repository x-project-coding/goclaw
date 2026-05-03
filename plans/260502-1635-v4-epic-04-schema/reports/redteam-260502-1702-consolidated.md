# Red Team Review — v4 EPIC-04 Plan

**Date:** 2026-05-02 17:02
**Plan dir:** `plans/260502-1635-v4-epic-04-schema/`
**Reviewers:** 4 hostile lenses (Security Adversary, Failure Mode Analyst, Assumption Destroyer, Scope & Complexity Critic)
**Raw findings:** 40 (12 Critical, 18 High, 10 Medium)
**Top 15 (deduplicated):** 12 Critical + 3 High

---

## Top 15 Findings (sorted by severity)

### CRITICAL (12) — block plan finalization

| # | Title | Reviewer | Phase | Disposition |
|---|---|---|---|---|
| 1 | JWT secret has no rotation path / no `kid` claim — single key compromise = full system breach | Security | Phase 06 | Accept |
| 2 | Bootstrap rate limit (1 req/sec) trivially bypassed → remote root takeover | Security | Phase 06 | Accept |
| 3 | Bootstrap concurrent race — partial UNIQUE `WHERE role='root'` missing → multi-root | Security + Failure | Phase 03 + 06 | Accept |
| 4 | Refresh token theft undetectable — no token family / parent-chain revocation | Security + Failure | Phase 03 + 06 | Accept |
| 5 | Frontend localStorage tokens — XSS exfiltrates 30-day refresh, no HttpOnly cookie | Security | Phase 11 | Accept |
| 6 | Backup restore reactivates revoked refresh tokens — replay window = retention | Security | Phase 14 | Accept |
| 7 | Channel merge-contact admin can silently hijack accounts (Alice's data → Bob) | Security | Phase 09 | Accept |
| 8 | Password reset flow completely missing — lost root password = bricked install | Failure | Phase 06 | Accept |
| 9 | `dev-v4` long-lived branch over 124 days = irrecoverable divergence from v3 hotfixes | Failure | plan.md | Accept |
| 10 | R1 merge-contact "atomic TX" structurally false — store APIs don't share `*sql.Tx` | Failure + Assumption | Phase 09 | Accept |
| 11 | Phase 11 file count 65 (claimed) vs 693 (real) — estimate basis questionable | Assumption | Phase 11 | Accept (verify) |
| 12 | `uuid_generate_v7()` → `gen_random_uuid()` silent regression — index locality cost, no ADR | Assumption | Phase 03 | Accept |

### HIGH (3)

| # | Title | Reviewer | Phase | Disposition |
|---|---|---|---|---|
| 13 | Argon2id 64MB self-DoS — no `/login` rate limit + no semaphore + always-verify | Security + Scope | Phase 06 | Accept |
| 14 | `ON DELETE CASCADE` blast radius — single admin/SQLi wipes user data irrecoverably | Security | Phase 03 | Accept |
| 15 | MasterTenantID file count 50 (claimed) vs 81 (real) — Phase 13 budget undercount | Assumption | Phase 13 | Accept |

---

## Detailed Findings

### Finding 1: JWT secret has no rotation path
**Severity:** CRITICAL — **Phase 06 § Architecture, Risk Assessment**
**Flaw:** No `kid` claim in JWT header, single HS256 secret read from env, plan defers `GOCLAW_JWT_SECRET_PREVIOUS` as "too complex". Rotation requires gateway restart → forces logout → operators avoid rotating.
**Failure scenario:** Attacker exfiltrates `GOCLAW_JWT_SECRET` via leaked `.env.local`, container env dump, CI artifact, core dump. Forges JWT for any user UUID with `role=root` indefinitely. Refresh revocation does NOT help — forged access tokens never query `user_sessions`.
**Suggested fix:**
1. Add `kid` to JWT header
2. Make secret a slice indexed by `kid` (read from `GOCLAW_JWT_SECRETS_JSON` env)
3. Verifier accepts any key in slice; issuer uses newest
4. Hot-reload via SIGHUP or admin endpoint
5. Promote from "defer" to P0

### Finding 2: Bootstrap remote takeover via 1 req/sec rate limit
**Severity:** CRITICAL — **Phase 06 § Non-functional**
**Flaw:** `/v1/bootstrap/init` is unauthenticated, creates `role=root` user with auto-issued admin API key, only protection is "1 req/sec per IP". Attacker reaching endpoint BEFORE legitimate operator wins. X-Forwarded-For spoofable. Only ONE request needed.
**Failure scenario:** Operator deploys gateway → unbounded race window → attacker hits `/init` first → owns entire installation as root.
**Suggested fix:**
1. Require one-time `GOCLAW_BOOTSTRAP_TOKEN` env var checked at `/init`
2. Token printed by gateway on first start to stderr (operator-only visibility)
3. Without matching token → 403
4. Optionally: bind `/bootstrap/*` to localhost-only on first boot

### Finding 3: Bootstrap concurrent race — partial UNIQUE missing on root role
**Severity:** CRITICAL — **Phase 06 § Architecture L102-110, Phase 03 schema**
**Flaw:** Mitigation = "UNIQUE on `users.email` + transactional check `bootstrap_required` flag" but flag is process-cached boolean, NOT a DB constraint. Two requests with different emails BOTH pass uniqueness check. Cached flag flipped AFTER INSERT (not atomic).
**Failure scenario:** Attacker scripts 50 parallel POSTs with different emails. All 51 INSERTs succeed (different emails). 51 root users created. In-memory flag flip prevents future calls but damage done.
**Suggested fix:**
1. Phase 03: add `CREATE UNIQUE INDEX users_only_one_root ON users(role) WHERE role='root'`
2. Phase 06: wrap bootstrap in TX with `pg_advisory_xact_lock(BOOTSTRAP_LOCK_ID)` at TX start
3. Test: `TestBootstrapConcurrent` — 50 parallel requests, assert exactly 1 succeeds

### Finding 4: Refresh token theft undetectable — no family revocation
**Severity:** CRITICAL — **Phase 06 § session.go, Risk Assessment**
**Flaw:** No token family / parent-chain pattern. Stolen refresh + legit user both rotate independently → both hold valid chains. No invalidation when revoked-but-not-expired token used (RFC 6749 §10.4 violation).
**Failure scenario:** Attacker steals RT1 via XSS. Rotates RT1 → RT2. Legit user later rotates RT1 from another tab → 401. User logs in fresh → RT3. Attacker still holds valid RT2 chain.
**Suggested fix:**
1. Phase 03: `user_sessions` add `family_id UUID NOT NULL` (default = id of root session)
2. Phase 06: on rotation, new row inherits `family_id`. On revoked-but-not-expired token use → `UPDATE user_sessions SET revoked_at=NOW() WHERE family_id=$1` (kill entire family) + audit log security incident
3. Test: `TestRefreshTokenTheftDetection`

### Finding 5: localStorage XSS = full account takeover
**Severity:** CRITICAL — **Phase 11 § Security**
**Flaw:** Plan stores tokens in localStorage with "document XSS risk; CSP header configured" — no CSP directive spec, no Trusted Types, no DOMPurify, no HttpOnly cookie path. Refresh token in localStorage = 30-day persistent XSS impact.
**Failure scenario:** Attacker creates agent with malicious `name` rendered raw. Admin views list → XSS → exfiltrates BOTH access AND refresh tokens.
**Suggested fix:**
1. Refresh token: HttpOnly + SameSite=Strict + Secure cookie (path=`/v1/auth/refresh`)
2. Access token: in-memory only (never localStorage), refreshed via cookie
3. CSP: `default-src 'self'; script-src 'self' 'nonce-{random}'; object-src 'none'; base-uri 'self'`
4. Add Trusted Types policy
5. DOMPurify for any user-rendered HTML contexts

### Finding 6: Backup restore reactivates revoked refresh tokens
**Severity:** CRITICAL — **Phase 14 § Backup/restore round-trip**
**Flaw:** `user_sessions` table is in backup. Restore from pre-revocation backup unrevokes stolen tokens. Plan asserts "row counts match + checksums" — confirms the bug, not protects against it.
**Failure scenario:** Day 1 attacker steals RT1. Day 2 backup taken. Day 3 user reports compromise → admin revokes. Day 4 ops restores backup (DR or rollback) → RT1 active again → attacker resumes for remaining 30d.
**Suggested fix:**
1. On restore: `UPDATE user_sessions SET revoked_at = NOW() WHERE revoked_at IS NULL` — force all users to re-auth
2. Maintain append-only revocation log (hash-chained) outside backed-up DB
3. JWT `iat` < restore timestamp → reject (force re-issuance)

### Finding 7: Channel merge-contact admin can silently hijack accounts
**Severity:** CRITICAL — **Phase 09 § Architecture L110-117, Security L240**
**Flaw:** Merge handler updates `agent_sessions.user_id` + `user_context_files.user_id` + `memory_documents.user_id` based ONLY on admin-supplied target. No source-user or target-user consent. Plan mitigation = "RoleAdmin required" — admin role does NOT prove authorization.
**Failure scenario:** Compromised/rogue admin merges Alice's `channel_contacts` into Bob's user_id → Bob's account silently inherits Alice's chat history, KG memory, uploaded files. No notification.
**Suggested fix:**
1. Restrict merge to: `channel_contacts.merged_id IS NULL` (unauthenticated) → authenticated user only. Block authenticated→authenticated user merge.
2. Email notification to BOTH source + target users on merge
3. Add `channel_contacts.merge_audit JSONB` (who/when/from-where)
4. WhatsApp JID phone+LID dual-format: hard-cap merge depth

### Finding 8: Password reset / forgot-password flow missing
**Severity:** CRITICAL — **Plan-wide gap**
**Flaw:** Phase 06 lists `/v1/auth/{login,refresh,logout,me}` — NO `/forgot` or `/reset`. UI key `login.forgot_password` exists but no implementation. v4 has no email infra. Lost root password = bricked install.
**Failure scenario:** Single-tenant deployment loses root password. No email reset, no admin to reset for them. App requires `psql` intervention.
**Suggested fix:** Pick one:
- (a) Document explicitly "no recovery" + add `goclaw reset-password --email <e>` CLI (operator-level recovery, no email infra needed)
- (b) Add minimal forgot-flow with admin-issued reset token
- (a) preferred for v4.0 KISS

### Finding 9: dev-v4 long-lived branch divergence
**Severity:** CRITICAL — **plan.md L7**
**Flaw:** 124 dev-days on single branch. v3 line still active (v3.11.3 hotfix shipped 2026-04-30). No merge cadence, no v3 hotfix forward-port policy.
**Failure scenario:** Phase 05 (22 days) drops 1131 tenant_id refs. v3 contributor ships CVE patch on `dev` touching `internal/store/pg/agents.go`. Forward-port fails — file structure changed. Forgotten CVE → v4 ships unpatched.
**Suggested fix:**
1. Add "Branch strategy" section to plan.md
2. Weekly `git merge dev` into `dev-v4` with conflict-resolution playbook
3. Decision: freeze v3 development after Phase 09 OR designate v3 maintenance fork
4. Any v3 hotfix author must forward-port to `dev-v4` as part of v3 PR (gate via dual-branch CI)

### Finding 10: R1 merge-contact "atomic TX" structurally false
**Severity:** CRITICAL — **Phase 09 Sub-09A**
**Flaw:** Plan claims TX wraps 4 UPDATEs but current code (`internal/http/contact_merge_handlers.go:90,97`) calls `MergeContacts` and `migrateContextFilesOnMerge` on separate connections; return value of latter dropped. No `BeginTx()` exposed at store layer.
**Failure scenario:** Implementer adds `tx := db.Begin()` superficially → `migrateContextFilesOnMerge` + new `agent_sessions UPDATE` still run on separate connections. Atomicity fictional. R1 "fixed" in name only, regression ships.
**Suggested fix:**
1. Phase 09 Sub-09A explicit step: "Refactor stores to accept `*sql.Tx` for merge path BEFORE adding sessions UPDATE"
2. Add `MergeUserAggregate(ctx context.Context, sourceUsers []uuid.UUID, targetUser uuid.UUID) error` single store method owning the TX
3. Test: `TestMergeContactDuringActiveSession` — concurrent goroutine writing for source while merge runs

### Finding 11: Phase 11 file count 65 vs 693 — estimate basis questionable
**Severity:** CRITICAL (verify) — **Phase 11 Overview**
**Flaw:** Plan claims "65 ui/web FE files / ~9226 LOC". Real: 693 non-test ts/tsx files / 84,833 LOC total. Tenant refs grep: 67 files. If only tenant-ref files matter, count is correct. If broader Zustand/router/api-client cleanup, scope is larger.
**Failure scenario:** 25-day estimate built on possibly-misread premise. Either over- or under-budget by 5-15 days.
**Suggested fix:**
1. Phase 11 Day 0: re-scout `ui/web/src/` properly. Walk call graph from `stores/auth.ts`
2. Adjust estimate based on actual scope (likely 8-18d for tenant sweep + auth, not 25d)
3. Defer non-essential UI work (profile page enhancements, e2e browser test 11D) to v4.0.1

### Finding 12: uuid_generate_v7() → gen_random_uuid() silent regression
**Severity:** CRITICAL — **Phase 03 L141, L190**
**Flaw:** v3 uses custom `uuid_generate_v7()` (defined in `migrations/000001_init_schema.up.sql:8`) producing time-ordered UUIDs. v4 plan switches to `gen_random_uuid()` (UUID v4, pure-random) without ADR. B-tree locality regression on hot tables (`agent_sessions`, `traces`, `spans`).
**Failure scenario:** v4's random UUIDs cause B-tree page splits, write amplification, slower trace/span ingestion. Production users see unexplained latency post-v4.
**Suggested fix:** Pick one:
- (a) Keep `uuid_generate_v7()` in v4 `000001_initial.up.sql` (copy SQL function from v3) — preferred
- (b) Write `docs/adr/2026-05-v4-uuid-v4-vs-v7.md` justifying regression with concrete trade-off numbers

### Finding 13: Argon2id 64MB self-DoS amplifier
**Severity:** HIGH — **Phase 06 Risk Assessment, Non-functional**
**Flaw:** Argon2id m=64MB × concurrent verifies. Plan defers semaphore + login rate-limit. Always-verify (anti-enumeration) means attacker doesn't even need valid emails to DoS.
**Failure scenario:** Attacker sends 1000 concurrent `/v1/auth/login` requests with random passwords for known email. 1000 × 64MB = 64GB → gateway OOM.
**Suggested fix:**
1. Cap concurrent password verifies via `chan struct{}` semaphore (process-level) — REQUIRED in Phase 06
2. Per-IP login rate limit (5/min/IP) — add to Phase 06
3. Optional: per-email rate limit AFTER lookup BEFORE verify (introduces enumeration but bounded)
4. Consider Argon2id m=32MB t=2 for desktop edition (still OWASP-acceptable)

### Finding 14: ON DELETE CASCADE blast radius
**Severity:** HIGH — **Phase 03 § Functional L50, Security L193**
**Flaw:** Every FK to `users(id)` cascades. Single rogue admin or SQLi `DELETE FROM users WHERE id=$x` wipes all `agent_sessions`, `memory_documents`, `vault_documents`, `cron_jobs`, `kg_entities` etc. Irrecoverable without backup.
**Failure scenario:** Compromised admin issues `DELETE /v1/users/<member>` → instant cascade data loss. No soft-delete, no two-person rule, no snapshot-before-delete.
**Suggested fix:**
1. Phase 03: soft-delete users (`users.deleted_at TIMESTAMPTZ NULL`); cascade only on vacuum-after-N-days job
2. Critical FKs (`agent_sessions`, `memory_documents`): use `ON DELETE SET NULL` — orphaned rows survive, retrievable
3. Phase 06: snapshot/audit before any user delete

### Finding 15: MasterTenantID file count 50 vs 81 — Phase 13 undercount
**Severity:** HIGH — **plan.md L79, Phase 13 Architecture**
**Flaw:** Plan claims ~50 non-test files (171 lines correct, file count wrong). Real: 81 non-test files. Missing from Phase 13 list: `cmd/gateway*.go` (7 files), `internal/http/{agents_codex_pool,storage,oauth}.go`, `internal/skills/seeder.go`, `internal/hooks/types.go`, `internal/gateway/router.go`, `internal/gateway/methods/heartbeat.go`, `internal/vault/enrich_worker.go`, `internal/store/context.go`.
**Failure scenario:** 4-day budget on 21-file premise. Real sweep across 81 files takes 2-3x longer. Each missed file = compile break or "ghost tenant" runtime ref.
**Suggested fix:**
1. Re-grep at start of Phase 13 → rebuild explicit list
2. Bump Phase 13 estimate to 8-10d
3. Phase exit gate must enumerate every file in commit message

---

## Findings dropped (medium / overlap)

- (16) OAuth tokens per-user encryption defer (Security F7) — accepted risk per ADR
- (17) Phase 12 desktop separate EPIC (Scope F2) — scope decision, defer to user
- (18) Phase 09 R1 has no v4 use case (Scope F3) — disagrees with Finding 10; Finding 10 prevails
- (19) Phase 10 curator scaffold YAGNI (Scope F4) — defer to user
- (20) Phase 13 ADR docs over-document (Scope F5) — minor
- (21) Phase 02 paper-only as phase (Scope F6 + Assumption F10) — minor structure
- (22) Backup checksum brittle (Scope F7) — overlaps Finding 6 + Failure F4
- (23) e2e tests deferred from CI (Failure F6) — operational
- (24) migrate down destructive (Failure F7) — operational guard
- (25) Desktop OS keyring fallback plaintext (Failure F8) — Phase 12 issue
- (26) pgvector dim hardcoded 1536 (Failure F10) — accepted v4 limit
- (27) PG store 90 vs 107 file count (Assumption F3) — minor
- (28) seedTenantAgent 305 vs 326 (Assumption F4) — minor
- (29) sqliteonly 8 vs 11 file count (Assumption F5) — minor
- (30) Client struct file path wrong (Assumption F8) — corrected by re-verify
- (31) Missing JWT lib in go.mod (Assumption F9) — addressed in Phase 06 todo
- (32) Phase 11 sub-phases not gating (Scope F9) — minor structure
- (33) 14 phases too many (Scope F10) — disagrees with user's plan structure preference

---

## Apply Strategy

Recommended: apply all 15 findings via inline `<!-- RED-TEAM: ... -->` markers in target phase files + summary table in `plan.md`.

Pattern for each finding:
```markdown
<!-- RED-TEAM Finding {N}: {title} (CRITICAL/HIGH) -->
{change/addition to plan}
<!-- /RED-TEAM Finding {N} -->
```

Phase files to edit:
- `plan.md` — add Red Team Review section + Branch strategy section
- `phase-03-v4-greenfield-schema-pg.md` — Findings 3, 4, 12, 14
- `phase-06-auth-bootstrap.md` — Findings 1, 2, 3, 4, 8, 13
- `phase-09-channels-merge-contact.md` — Findings 7, 10
- `phase-11-frontend-bootstrap-login.md` — Findings 5, 11
- `phase-13-cleanup-deferred.md` — Finding 15
- `phase-14-validation-final.md` — Finding 6

Total: 15 findings → 7 files modified.
