---
title: "GoClaw v4 EPIC-04 — Schema Foundation (greenfield rebuild)"
description: "Drop multi-tenant, introduce real users + password auth, refactor 1131 tenant_id refs across PG+SQLite stores. TDD-first, e2e-validated."
status: pending
priority: P1
effort: "115-160 dev-days (1 dev: 5-7mo, 2 devs BE+FE: 3-4mo, 3 devs: 2-3mo)"
branch: dev-v4
tags: [epic-04, v4, schema, auth, multi-user, tdd, greenfield]
created: 2026-05-02
---

# v4 EPIC-04 — Schema Foundation

Greenfield rebuild of GoClaw schema layer. Drop multi-tenant primitives. Introduce real `users` (Argon2id + JWT/refresh). Refactor every `user_id VARCHAR(255)` → `UUID FK`. Wipe v3 migrations, fresh `000001_initial`.

## Source of truth

- Master research: `plans/260502-1323-goclaw-v4-brainstorm/reports/master-260502-1555-epic-04-research.md`
- Locked decisions (35): `plans/260502-1323-goclaw-v4-brainstorm/reports/decisions-260502-1504-epic-04-locked.md`
- Audit corrections: `plans/260502-1323-goclaw-v4-brainstorm/reports/audit-260502-1555-master-research.md`
- Pool/cache scope (13 structures): `plans/260502-1323-goclaw-v4-brainstorm/reports/scout-260502-1555-pool-cache-tenant-scope.md`

## TDD methodology (--tdd)

Each impl phase MUST follow: **(1) write failing tests → (2) impl → (3) verify**. Real PG18+pgvector port 5435, real LLM (Bailian + OpenRouter) from `env.e2e-tests/.env`. NO mocks for critical paths. Build tag: `//go:build e2e`.

## Phase status

| # | Phase | Status | Effort | Gate |
|---|---|---|---|---|
| 01 | Test harness + e2e bootstrap | completed | 5d | none |
| 02 | v3 mental model (paper-only) | completed | 1d | 01 |
| 03 | v4 PG schema (000001_initial) | completed | 6d | 01 — commit `fabc2a61` |
| 04 | v4 SQLite schema rewrite | completed | 5d | 01 (parallel to 03) — commit `9f43e672` |
| 05 | Stores refactor (PG+SQLite) | **completed 2026-05-03** | 22d | 03+04 green — PR-05A merged + L1 (`02c9a3fb` + L2 commits) + L3 (PR-05C-2 21 commits B0..E2) |
| 06 | Auth + bootstrap + JWT/refresh | **completed 2026-05-03** (e2e deferred → Phase 14) | 18d | 05 green |
| 07 | Pool/cache refactor (13 structs) | **completed 2026-05-03** | 5d | 06 (parallel to 08) |
| 08 | CLI prune (drop ~25, keep 7) | **completed 2026-05-03** | 3d | 06 (parallel to 07) |
| 09 | Channels + merge-contact R1 fix | **completed 2026-05-03** — commit `26d64a6f` | 12d | 06 + 07 |
| 10 | Skills + skill_versions + curator | **completed 2026-05-03** | 6d | 05 (S9 prep, can run after 09) |
| 11 | Frontend bootstrap + login + refresh | **completed 2026-05-03** (Sub-11A+B+C+D done; browser e2e → Phase 14) | 15-18d (re-scouted) | 06 + 09 |
| ~~12~~ | ~~Desktop edition first-run (sqliteonly)~~ | **DEFERRED → EPIC-05-desktop** | ~~6d~~ | — |
| 13 | Cleanup deferred (MasterTenantID purge, dead code) | **completed 2026-05-03** (5 batches A-E + Final: 73 files purged, 4 ADRs, 4 e2e tests, README polished) | 8-10d | 09+10 |
| 14 | Validation final (full e2e + RBAC) | **in-progress 2026-05-03** (14A impl done; 14B 16/16 e2e files committed; 2 intentionally-red tests gate Finding 6 + KG per-user filter) | 6d + 2d (14A impl) | all phases |

**Total:** 122-124 dev-days (Phase 12 → EPIC-05; Phase 13 bumped per F15 audit).

## Critical dependency graph

```
01 (harness) ──┬──> 02 (paper)
               ├──> 03 (PG schema) ──┐
               └──> 04 (SQLite schema) ──┴──> 05 (stores) ──> 06 (auth/bootstrap)
                                                                │
                              ┌─────────────────────────────────┤
                              ▼                                 ▼
                       07 (pool/cache)              08 (CLI prune)
                              │                                 │
                              └──────────┬──────────────────────┘
                                         ▼
                        09 (channels + merge-contact R1) ──> 10 (skills+S9)
                                         │                          │
                                         └────────┬─────────────────┘
                                                  ▼
                              11 (FE bootstrap+login)
                                         │
                                  13 (cleanup MasterTenantID)
                                         │
                                  14 (full e2e + RBAC matrix)

(Phase 12 desktop edition → DEFERRED to EPIC-05-desktop per Validation V2)
```

## Key decisions snapshot

- 65 v3 tables → drop 5, add 5, rename `sessions`→`agent_sessions` → 65 v4 tables
- Argon2id (m=64MB, t=3, p=4) + JWT (15min) + opaque refresh (30d, rotate-on-use)
- Roles: `root / admin / member / viewer` (rename from owner/admin/operator/viewer)
- 1131 tenant_id refs (verified) across 90 PG + 88 SQLite store files
- 8 sqliteonly + 6 !sqliteonly build-tag files (audit-corrected; not 110)
- 171 lines / ~50 files MasterTenantID NON-test (audit; v3 R1 sessions migrate bug fix)

## Cross-phase gates

- Phase N starts only when N-1 merged + tests green (explicit per phase)
- Phase 03+04 can run parallel (PG vs SQLite independent)
- Phase 07+08 can run parallel (after 06)
- Phase 11 has no parallel pair (Phase 12 deferred to EPIC-05 per V2)
- Phase 14 (final validation) blocks merge to `main`

<!-- RED-TEAM Finding 9: dev-v4 long-lived branch divergence (CRITICAL) -->
## Branch Strategy

- **Weekly merge cadence:** `git merge dev` into `dev-v4` every Sunday — short, frequent merges beat one massive end-of-line conflict resolution.
- **v3 hotfix policy decision (REQUIRED before Phase 03 starts):**
  - **Option A (preferred):** freeze v3 development after Phase 09 merge (~Day 60) — only critical CVE patches accepted, must forward-port to `dev-v4` in same PR.
  - **Option B:** designate v3 maintenance fork (e.g., `v3-maintenance`) — any v3 hotfix author MUST forward-port to `dev-v4` as part of their v3 PR, gated via dual-branch CI check.
- **Conflict resolution playbook:** Phase 05 (22 days, 1131 tenant_id refs) is highest collision risk vs. v3 store changes. If a v3 store hotfix collides during weekly merge, escalate to Phase 05 owner same-day.
- **Rationale:** 124 dev-day branch divergence is irrecoverable without policy. v3 line still active (v3.11.3 hotfix shipped 2026-04-30); without forward-port gate, CVE patches risk being lost in the v4 cutover.
<!-- /RED-TEAM Finding 9 -->

## Risks (R1-R5 from master)

- R1 sessions-not-migrated-on-merge (fix in P09)
- R2 EventBus UserID validation (P05/P09)
- R3 Episodic worker UUID parse (P05)
- R4 Cache key collision (P07)
- R5 Test parallel UNIQUE collision → random-suffix (P01)

## YAGNI / KISS / DRY discipline

- NO load tests / benchmarks (per CLAUDE.md rule)
- NO new abstractions unless replacing 3+ duplicate sites
- Defer per-user vault encryption + activity_logs retention to v4.x (Q-14 audit)
- Defer license tiering to EPIC-02 (audit MISS-1)

<!-- RED-TEAM Findings 1-15: Consolidated review summary (CRITICAL/HIGH) -->
## Validation Log

### Session 1 — 2026-05-02 17:37 (post red-team apply)

| # | Question | Decision | Propagated to |
|---|---|---|---|
| V1 | E2E in CI cadence | **e2e-fast (skip LLM) per PR + e2e-full nightly**. Block merge on e2e-fast fail. | Phase 14, Phase 01 (CI workflow scaffold) |
| V2 | Phase 12 desktop scope | **Deferred to EPIC-05-desktop**. EPIC-04 ends at Phase 11. | plan.md Phase status, Phase 12 marked DEFERRED |
| V3 | Web/desktop FE auth sharing | **Option B (copy)**. Build-isolated. Refactor to workspace if duplication painful. | Phase 11 (already chose B), Phase 12 (deferred) |
| V4 | UUID generator | **uuid_generate_v7() globally — ALL data generation uses UUID v7, no v4 anywhere** (user lock 2026-05-02 17:37) | Phase 03 (PG schema), Phase 04 (SQLite helper), Phase 05 (Go: uuid.NewV7), Phase 06 (user_sessions, family_id) |

**V4 implementation note (UUID v7 universal):**
- **PG**: define `uuid_generate_v7()` SQL function in `000001_initial.up.sql`, set as DEFAULT for all `id UUID PK` columns
- **SQLite**: SQLite has no native v7 → generate in Go layer at INSERT time. Schema column has no DEFAULT
- **Go code**: use `github.com/google/uuid` v1.6+ `uuid.NewV7()` (NOT `uuid.New()` which is v4)
- **Migration**: any existing `gen_random_uuid()` reference in plan files = MUST CHANGE to `uuid_generate_v7()`
- **Test fixtures**: `seedUser`, `seedAgent`, etc. all use v7

## Red Team Review

- **Date:** 2026-05-02
- **Reviewers:** 4 hostile lenses (Security Adversary, Failure Mode Analyst, Assumption Destroyer, Scope & Complexity Critic)
- **Total findings:** 40 raw → 15 accepted (12 Critical, 3 High); 25 dropped as overlap/medium/scope decisions
- **Disposition:** all 15 accepted — applied inline as `<!-- RED-TEAM Finding {N}: ... -->` markers in target phase files
- **Source report:** `reports/redteam-260502-1702-consolidated.md`

### Severity breakdown × phase ownership

| # | Severity | Title | Phase |
|---|---|---|---|
| 1 | CRITICAL | JWT secret has no rotation path / no `kid` claim | Phase 06 |
| 2 | CRITICAL | Bootstrap remote takeover via 1 req/sec rate limit | Phase 06 |
| 3 | CRITICAL | Bootstrap concurrent race — partial UNIQUE missing on root role | Phase 03 + 06 |
| 4 | CRITICAL | Refresh token theft undetectable — no family revocation | Phase 03 + 06 |
| 5 | CRITICAL | Frontend localStorage tokens — XSS exfiltrates 30-day refresh | Phase 11 |
| 6 | CRITICAL | Backup restore reactivates revoked refresh tokens | Phase 14 |
| 7 | CRITICAL | Channel merge-contact admin can silently hijack accounts | Phase 09 |
| 8 | CRITICAL | Password reset / forgot-password flow missing | Phase 06 |
| 9 | CRITICAL | `dev-v4` long-lived branch over 124 days = divergence | plan.md |
| 10 | CRITICAL | R1 merge-contact "atomic TX" structurally false | Phase 09 |
| 11 | CRITICAL | Phase 11 file count 65 (claimed) vs 693 (real) | Phase 11 |
| 12 | CRITICAL | `uuid_generate_v7()` → `gen_random_uuid()` silent regression | Phase 03 |
| 13 | HIGH | Argon2id 64MB self-DoS — no rate limit + no semaphore | Phase 06 |
| 14 | HIGH | `ON DELETE CASCADE` blast radius — single admin/SQLi wipes data | Phase 03 |
| 15 | HIGH | MasterTenantID file count 50 (claimed) vs 81 (real) | Phase 13 |

Each finding's specific change is inlined in the target phase file under matching `<!-- RED-TEAM Finding N -->` markers.
<!-- /RED-TEAM Findings 1-15 -->
