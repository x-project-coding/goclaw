# Phase 04 — v4 Greenfield SQLite Schema

## Context Links

- Master § 3, § 8 (dual-DB parity rule)
- Decisions Q-13 (build tags), Q-14 (schema-from-scratch)
- Phase 03 (PG schema — same logical model)
- v3 schema: `internal/store/sqlitestore/schema.sql` (1665 LOC) + `schema.go` (831 LOC, `SchemaVersion = 26`)

## Overview

- Priority: P0
- Status: completed (2026-05-02)
- Effort: 5 dev-days
- Description: Rewrite `internal/store/sqlitestore/schema.sql` as v4 greenfield. Reset `internal/store/sqlitestore/schema.go` migrations map (drop 26 incremental patches, fresh `SchemaVersion = 1`). Maintain logical parity with PG schema (Phase 03). SQLite types differ — UUID stored as TEXT, vectors stored via sqlite-vec virtual tables.

## Key Insights

- SQLite has NO native UUID type — store as `TEXT NOT NULL` with CHECK constraint.
- SQLite has NO `JSONB` — store as `TEXT` (JSON serialized; existing pattern).
- pgvector → sqlite-vec virtual tables (already in v3 schema lines for memory; preserve pattern).
- Q-13 build tags: `//go:build sqliteonly` for desktop. No conditional schema branching; ALL tables compile under both PG and SQLite.
- v3 had 26 incremental migrations in `schema.go`. v4 resets to single full schema = `SchemaVersion = 1`.
- 88 SQLite store *.go files reference these tables; Phase 05 refactors them.
- Phase 04 can run PARALLEL to Phase 03 (different files, same logical model).

## Tests to write FIRST (TDD red step)

| Test file | Cases (must FAIL until impl green) |
|---|---|
| `tests/e2e/schema/10_sqlite_schema_apply_test.go` | `TestSqliteSchemaApply` — fresh in-memory SQLite + apply `schema.sql` succeeds; `TestSqliteVersionConst` — `SchemaVersion == 1` |
| `tests/e2e/schema/11_sqlite_table_inventory_test.go` | `TestSqliteTableCount` — exactly 65 tables present (sqlite_master query); `TestSqliteRequiredTables` — each of 65 expected names exists; `TestSqliteDroppedTables` — `tenants`, `tenant_users`, `skill_tenant_configs`, `builtin_tool_tenant_configs`, `tenant_hook_budget` MUST NOT exist |
| `tests/e2e/schema/12_sqlite_columns_test.go` | `TestSqliteNoTenantID` — `pragma_table_info` returns 0 columns named `tenant_id` across all tables; `TestSqliteAgentSessionsRenamed` — table `sessions` does not exist; `agent_sessions` exists |
| `tests/e2e/schema/13_sqlite_pg_parity_test.go` | `TestParityTableNames` — set of table names equal between PG (Phase 03) + SQLite (this phase); `TestParityColumnNames` — for each shared table, column names match (types may differ) |

**Red verification:** Tests rely on Phase 03 PG migration applied + SQLite schema applied. Both must be green for parity test 13 to pass.

## Requirements

### Functional

- REWRITE `internal/store/sqlitestore/schema.sql` as v4 greenfield (~1500 lines):
  - Header comment: cite v4 EPIC-04 + Q-13/Q-14.
  - 65 `CREATE TABLE IF NOT EXISTS` statements grouped by domain (mirror Phase 03 layout).
  - All `id` columns: `TEXT NOT NULL PRIMARY KEY` (UUID stored as text).
  - All `tenant_id` columns DROPPED.
  - All `user_id VARCHAR(255)` → `TEXT` (UUID-as-text); FK `REFERENCES users(id)` where applicable.
  - JSON columns stay `TEXT`.
  - Embedding columns reuse v3 pattern (sqlite-vec virtual tables for memory).
  - CHECK constraints: vault_documents.scope, role enum, etc.
- RESET `internal/store/sqlitestore/schema.go`:
  - `SchemaVersion = 1`.
  - Empty `migrations` map (no incremental patches; everything in fresh schema.sql).
  - Keep `Apply()` runner logic; just resets state.
- Update `schema.go` doc-comment to reflect v4 reset.

### Non-functional

- Logical parity with PG schema: same table names, same column names (types may differ per dialect).
- Compiles under both default + `sqliteonly` build tags.
- Schema apply < 500ms on fresh in-memory SQLite.

## Architecture

```
SQLite v4 stack:
  internal/store/sqlitestore/
   ├─ schema.sql       (~1500 LOC, full schema)
   ├─ schema.go        (~600 LOC, SchemaVersion=1, empty migrations map)
   ├─ pool.go          (connection pool — unchanged)
   └─ <88 store impl files>  (refactored in Phase 05)

Build flow:
  default build  → uses PG store/pg/ + ignores sqliteonly files
  -tags sqliteonly → uses store/sqlitestore/ (ALL files; no conditional gating inside)
```

## Related Code Files

### Modify (rewrite)

- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/store/sqlitestore/schema.sql` (full rewrite, ~1500 lines)
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/store/sqlitestore/schema.go` (reset SchemaVersion + empty migrations map)

### Create

- `/Users/viettran/Documents/coding/next-level-builder/goclaw/tests/e2e/schema/10_sqlite_schema_apply_test.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/tests/e2e/schema/11_sqlite_table_inventory_test.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/tests/e2e/schema/12_sqlite_columns_test.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/tests/e2e/schema/13_sqlite_pg_parity_test.go`

### Read for context

- Phase 02 `v3-baseline.md`
- Phase 03 `migrations/000001_initial.up.sql` (parity reference)
- v3 `internal/store/sqlitestore/schema.sql` (column-by-column reference; types only)

### Delete

- None (rewriting in place).

## Implementation Steps

1. Verify Phase 03 schema.up.sql merged (parity reference required).
2. Write 4 SQLite schema test files FIRST (red — schema.sql still v3).
3. Run `go test -tags e2e ./tests/e2e/schema/...` → confirm red on parity test (Test 13).
4. Open Phase 03 `migrations/000001_initial.up.sql` side-by-side with old `internal/store/sqlitestore/schema.sql`.
5. Rewrite `schema.sql` table-by-table:
   - Section 1 (core): `users` + `user_sessions` first (referenced by all FKs).
   - Sections 2-14: domain by domain matching Phase 03 order.
   - For each PG `UUID` → SQLite `TEXT NOT NULL`.
   - For each PG `JSONB` → SQLite `TEXT NOT NULL DEFAULT '{}'`.
   - For each PG `TIMESTAMPTZ` → SQLite `TEXT` (ISO-8601) OR `INTEGER` (Unix epoch — match v3 pattern, document choice).
   - For each PG `vector(1536)` → preserve v3 pattern (regular column + sqlite-vec virtual table).
   - Drop ALL `tenant_id` columns.
   - Add CHECK constraints to enforce role enum, scope enum, source enum.
6. Append same indexes as PG (translated to SQLite syntax).
7. Reset `schema.go`:
   - `const SchemaVersion = 1` (was 26).
   - `var migrations = map[uint]string{}` (empty; everything is in fresh `schema.sql`).
   - Keep `Apply()` + `Migrate()` logic intact.
   - Update doc-comments at top to reflect v4.
8. Run `go build -tags sqliteonly ./...` (desktop build path) — must be clean.
9. Run `go vet -tags sqliteonly ./...` — clean.
10. Run all 4 SQLite schema tests + Phase 03 parity test → green.
11. Run desktop build sanity: `make desktop-build VERSION=0.1.0-dev` (smoke check Wails compiles).
12. Commit: `feat(schema): v4 SQLite schema rewrite (SchemaVersion=1)`.

## Todo List

- [ ] Phase 03 PG schema merged (parity ref)
- [ ] 4 SQLite schema test files written (red verified)
- [ ] Rewrite `schema.sql` (~1500 lines)
- [ ] Reset `schema.go` (SchemaVersion=1, empty migrations map)
- [ ] go build -tags sqliteonly clean
- [ ] go vet -tags sqliteonly clean
- [ ] All 4 SQLite tests + Phase 03 parity test green
- [ ] Desktop build smoke check passes

## Success Criteria

- 65 tables present after `Apply()` on fresh SQLite.
- 0 tenant_id columns.
- `agent_sessions` exists; `sessions` does not.
- Parity test 13 (table+column name set) passes between PG + SQLite.
- `go build -tags sqliteonly ./...` clean.

## Risk Assessment

| Risk | Severity | Mitigation |
|---|---|---|
| SQLite virtual table for embeddings missing → memory tests fail | Med | Re-grep v3 `schema.sql` for `vec0` + `CREATE VIRTUAL TABLE`; copy pattern verbatim |
| Type mismatch (TEXT vs INTEGER) on timestamps causes scan errors | Med | Match v3 pattern exactly per column; cite v3 line numbers in comment |
| CHECK constraint syntax differs PG vs SQLite | Low | Test 13 catches; SQLite supports CHECK identically for our use cases |
| 88 SQLite store files break compile | High | Phase 05 fixes; this phase only changes `schema.sql` + `schema.go` |
| Desktop edition (sqliteonly) breaks at startup | High | Phase 12 has dedicated first-run e2e test; this phase smoke-tests `wails build` only |

## Security Considerations

- SQLite file lives at `~/.goclaw/data/` per CLAUDE.md (Lite edition).
- No tenant boundary in schema (Q-14 audit "trust admin model").
- Argon2id hash stored in `users.password_hash` TEXT (Phase 06 enforces format).
- Refresh-token hash in `user_sessions.refresh_token_hash` (sha256-of-opaque, never raw).

## Cross-phase Gates

- **Entry:** Phase 01 merged + Phase 03 PG schema merged (for parity test).
- **Exit:** SQLite tests + parity test green + `go build -tags sqliteonly ./...` clean. Gates Phase 05 (stores) jointly with Phase 03.

## Next Steps

- Phase 05 — stores refactor consumes both PG + SQLite schemas.
- Phase 12 — desktop edition wires sqliteonly bootstrap; this phase guarantees schema is ready.
