# Phase 02 — v3 Mental Model (paper-only analysis)

## Context Links

- Master § 4 (Code Layer Impact): `plans/260502-1323-goclaw-v4-brainstorm/reports/master-260502-1555-epic-04-research.md`
- Audit corrections (D-1 ... D-9): `plans/260502-1323-goclaw-v4-brainstorm/reports/audit-260502-1555-master-research.md`
- Live v3 schema: `internal/store/sqlitestore/schema.sql` (1665 lines, 65 tables)
- Live PG migrations: `migrations/000001_init_schema.up.sql` ... `000057_*.up.sql` (114 files)

## Overview

- Priority: P0 (knowledge artifact — blocks Phase 03+04 schema design)
- Status: completed (2026-05-02)
- Effort: 1 dev-day
- Description: Single doc that catalogues v3 baseline (current state) so Phase 03+04 schema authors don't have to grep. NO code change. Pure paper. Catalogue 65 tables, FK graph, drop-list verification, ref counts. Ground truth for delete-scope safety.

## Key Insights

- Audit corrected D-3: v3 has 65 tables (not 62 / 37 — research math wrong). v4 = 65 - 5 drop + 5 new = 65.
- Audit corrected D-1: MasterTenantID has 171 NON-test lines across ~50 files — not 6 locations.
- Audit corrected D-4: `internal/store/pg/tenant_config_store.go` does NOT exist; actual files are `internal/store/tenant_config_store.go` + `internal/store/pg/tenant_configs.go` + `internal/store/sqlitestore/tenant-configs.go`.
- v3 R1 bug (sessions not migrated on merge-contact) MUST be flagged in this doc so Phase 09 author cannot miss it.

## Tests to write FIRST (TDD red step)

This is a paper-only phase. **No code, no tests.** Output is a markdown reference doc. Verification = peer review.

> Discipline: if any "fact" in this doc cannot be backed by `grep`/`ls`/file:line citation, REMOVE the claim or mark it `(unverified)`.

## Requirements

### Functional

Single deliverable: `plans/260502-1635-v4-epic-04-schema/v3-baseline.md` containing:

1. **Table catalogue** — 65 v3 tables grouped by domain (core/teams/sessions/memory/vault/skills/channels/cron/MCP/tracing/audit/hooks/evolution/llm), each with: name, FK count, `tenant_id` y/n, `user_id VARCHAR` y/n, columns to refactor.
2. **FK graph** — Mermaid ERD edges only (boxes minimal). Mark which FK chains break when `tenants` drops.
3. **DROP list verification** — re-grep each candidate file. Confirm path. List ALL refs (file:line) per dropped symbol.
4. **MasterTenantID enumeration** — full file:line list (~50 files / 171 lines). Source for Phase 13 cleanup.
5. **R1 sessions-migration bug evidence** — show v3 merge-contact code path (file:line) where `sessions` is NOT updated after `merged_id` set. Phase 09 fix anchor.
6. **Pool/cache scope cross-ref** — re-cite the 13 pool/cache structures from scout report (file:line each).
7. **Counts table** — definitive: 1131 tenant_id refs, 8 sqliteonly + 6 !sqliteonly build-tag files, 93 cmd/*.go files, 90 PG store + 88 SQLite store files, 65 v3 tables.
8. **CLI drop list** — re-list each `cmd/*.go` to drop with `wc -l` LOC + brief purpose.

### Non-functional

- Doc < 600 lines (KISS; deeper info lives in source reports).
- Every claim has `file:line` citation OR explicit `(unverified)` tag.
- Mermaid ERD renders in markdown viewer.

## Architecture

```
plans/260502-1635-v4-epic-04-schema/
├── plan.md
├── phase-01-...md
├── phase-02-v3-mental-model.md   ← (this phase's spec)
└── v3-baseline.md                ← (this phase's deliverable, peer-reviewable)
```

## Related Code Files

### Create

- `/Users/viettran/Documents/coding/next-level-builder/goclaw/plans/260502-1635-v4-epic-04-schema/v3-baseline.md`

### Read for context (no edit)

- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/store/sqlitestore/schema.sql`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/migrations/` (sample for FK chains)
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/store/pg/` (90 files)
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/http/contact_merge_handlers.go` (R1 evidence)
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/store/pg/contact_resolve.go` (R1 evidence)

### Modify

- None.

### Delete

- None.

## Implementation Steps

1. Run `grep -cE '^CREATE TABLE' internal/store/sqlitestore/schema.sql` → confirm 65. Document.
2. Parse `schema.sql` table-by-table; for each: extract `CREATE TABLE` + FK lines; tag with v4 disposition (keep/drop/rename/refactor).
3. Group tables into 14 domains; produce catalogue table.
4. Generate Mermaid ERD edges via parsing `REFERENCES <table>(<col>)` lines (script optional, can be hand-built since one-time).
5. Verify each DROP candidate exists at expected path (8 paths from master § 6). For non-existent paths (e.g. `tenant_config_store.go` in `pg/`), update master §6 in this doc with audit-corrected path. Re-grep all callsites of dropped symbols.
6. `grep -rn "MasterTenantID" --include="*.go" | grep -v _test.go` → enumerate file:line into a table. Save as appendix.
7. Read `internal/http/contact_merge_handlers.go` + `internal/store/pg/contact_resolve.go` + `internal/store/pg/channel_contacts.go`. Find: where `merged_id` flips. Verify NO `UPDATE sessions SET user_id=...` or `UPDATE agent_sessions SET user_id=...` exists in same code path. Annotate file:line as "R1 evidence".
8. From scout pool-cache report, re-verify each of 13 file:line entries via `Read` (5 mins).
9. Re-grep all numbers in master § 2 to confirm post-audit values stick.
10. Write `v3-baseline.md` with sections: 0 Counts, 1 Catalogue, 2 ERD, 3 Drop list, 4 MasterTenantID enum, 5 R1 evidence, 6 Pool/cache cross-ref, 7 CLI drop list.
11. Lead reviewer (anh) reads — sign off via commit message or Slack.

## Todo List

- [x] Confirm 65 tables (grep)
- [x] Build domain catalogue (14 groups)
- [x] Mermaid ERD edges
- [x] DROP candidate path verification (8 paths)
- [x] MasterTenantID file:line enumeration (171 lines / ~83 files)
- [x] R1 evidence trace (3 files)
- [x] Pool/cache 13-entry cross-ref
- [x] CLI drop list with LOC
- [x] Final review (delivered: v3-baseline.md = 416 lines, 6 discrepancies vs plan flagged)

## Success Criteria

- `v3-baseline.md` < 600 lines, every claim cited.
- Phase 03+04 author can derive v4 schema delta from this doc alone.
- Phase 13 cleanup author has explicit file:line list for MasterTenantID purge.
- Phase 09 author has R1 evidence anchor.

## Risk Assessment

| Risk | Mitigation |
|---|---|
| Doc bit-rots if v3 schema changes mid-phase | freeze on Phase 02 entry; v3 in separate `dev` branch, v4 work on `dev-v4` (already created) |
| Mermaid too complex (65 boxes) | edges-only; group by domain; use 4 sub-diagrams |
| Scope creep into v4 design | strict: paper describes v3 only; v4 lives in Phase 03+04 |

## Security Considerations

- N/A (read-only paper analysis, no code change, no secrets).

## Cross-phase Gates

- **Entry:** Phase 01 merged + harness green (gives reviewer something concrete to validate against).
- **Exit:** `v3-baseline.md` reviewer-approved. Gates Phase 03 + 04.

## Next Steps

- Phase 03 (PG schema) — uses catalogue + FK graph as authoritative input.
- Phase 04 (SQLite schema) — uses same catalogue.
- Phase 13 (cleanup) — uses MasterTenantID enumeration as worklist.
- Phase 09 (channels) — uses R1 evidence to fix bug.
