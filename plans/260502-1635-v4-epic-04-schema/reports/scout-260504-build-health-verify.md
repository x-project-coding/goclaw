# GoClaw v4 EPIC-04 Build Health Scout Report

**Date:** 2026-05-04  
**Task:** Verify build/compile health and test discovery for both PG and SQLite build tags

## Executive Summary

All core compile and build health checks **PASS**. Both PG and SQLite builds succeed, all vet checks pass, frontend builds successfully. Test file build-tag discovery is nearly complete but shows 4 integration test files missing the required `//go:build integration` tag. CI workflow exists but lacks E2E-specific jobs (`e2e-fast`, `e2e-full`) referenced in the plan.

---

## 1. Compile Health

### Backend builds

| Build Tag | Command | Exit Code | Status |
|-----------|---------|-----------|--------|
| Default (PG) | `go build ./...` | 0 | ✓ PASS |
| SQLite only | `go build -tags sqliteonly ./...` | 0 | ✓ PASS |

### Vet checks

| Command | Exit Code | Status |
|---------|-----------|--------|
| `go vet ./...` | 0 | ✓ PASS |
| `go vet -tags e2e ./tests/e2e/...` | 0 | ✓ PASS |
| `go vet -tags integration ./tests/integration/...` | 0 | ✓ PASS |

**Finding:** All Go code passes static analysis. No compile or vet errors.

---

## 2. Frontend Build Health

### Verification

- **Package.json:** Present at `/ui/web/package.json`
- **Build scripts:** Confirmed
  - `pnpm tsc --noEmit` → TypeScript type check
  - `pnpm build` → Vite production build
- **pnpm install:** Succeeds silently (no errors)
- **pnpm tsc --noEmit:** Succeeds (no output = no TypeScript errors)
- **pnpm build:** Succeeds, produces `dist/` with full asset tree
  - Build time: 633ms
  - Final bundle size: Main chunk `i18n-...js` = 632 KB (gzip: 204 KB)
  - Warning: Some chunks > 500 KB (normal for this codebase, noted for potential splitting)

**Finding:** Frontend builds cleanly with no TypeScript errors. Build output is healthy.

---

## 3. Makefile E2E Targets

### Targets verified present

| Target | Status | Notes |
|--------|--------|-------|
| `test-invariants` | ✓ Present | `go test -race -timeout=90s -tags integration ./tests/invariants/...` (P0) |
| `test-contracts` | ✓ Present | `go test -race -timeout=90s -tags integration ./tests/contracts/...` (P1) |
| `test-scenarios` | ✓ Present | `go test -race -timeout=180s -tags integration ./tests/scenarios/...` (P2) |
| `test-critical` | ✓ Present | Composite: `test-invariants test-contracts` |
| `test-e2e` | ✓ Present | `go test -tags e2e -v -timeout 30m ./tests/e2e/...` |
| `test-hooks` | ✓ Present | Composite: unit + e2e + chaos + rbac + tracing |

### Targets NOT found (per plan expectations)

| Target | Status | Notes |
|--------|--------|-------|
| `test-e2e-short` | ✗ Missing | Plan Phase 14 step 4: skip LLM version |
| `test-e2e-full` | ✗ Missing | Plan Phase 14 step 5: with LLM version |
| `test-release-gate` | ✗ Missing | Plan Phase 14 step 4-5: combined release gate |

**Finding:** Core layered test targets exist and are well-defined. The plan references `test-e2e-short` and `test-e2e-full` as distinct targets, but the Makefile has only a single `test-e2e` target. This may be intentional (single target with env var gating) or require clarification per EPIC-04 Phase 14.

---

## 4. CI Workflow Check

### File: `.github/workflows/ci.yaml`

#### Jobs present

| Job | Status | Build matrix | Notes |
|-----|--------|--------------|-------|
| `go` | ✓ Present | PG (`go build ./...`) + SQLite (`go build -tags sqliteonly ./...`) | Default + Postgres service container |
| `web` | ✓ Present | Single runner | pnpm install, lint, build |

#### E2E-specific jobs

| Job | Status | Notes |
|-----|--------|-------|
| `e2e-fast` | ✗ **Missing** | Expected per plan V1: skip LLM, blocks PR merge |
| `e2e-full` | ✗ **Missing** | Expected per plan: real LLM, nightly schedule |

#### Build matrix coverage

- PG: ✓ Present (`go build ./...`)
- SQLite: ✓ Present (`go build -tags sqliteonly ./...`)
- Both tags verified in separate steps

**Finding:** CI has solid core coverage (unit tests, integration tests, vet, both build tags). Missing dedicated E2E jobs that the plan specifies for phase-blocking and nightly runs. Current flow relies on `test-e2e` from Makefile (requires env setup).

---

## 5. Test Build-Tag Discovery

### E2E tests

| Metric | Count | Status |
|--------|-------|--------|
| Total `*_test.go` files in `tests/e2e/` | 61 | ✓ |
| Files with `//go:build e2e` tag | 61 | ✓ **100% tagged** |

**Discovery:** All 61 e2e test files carry the correct build tag.

### Integration tests

| Metric | Count | Status |
|--------|-------|--------|
| Total `*_test.go` files in `tests/integration/` | 57 | - |
| Files with `//go:build integration` tag | 53 | ⚠ **93% tagged** |
| Files **missing** tag | 4 | ✗ **CONCERN** |

#### Integration test files missing build tag

1. `sqlite_vault_visibility_matrix_test.go`
2. `tts_dual_read_sqlite_test.go`
3. `sqlite_vault_shared_docs_test.go`
4. `sqlite_smoke_test.go`

**Finding:** All SQLite-specific integration test files (4 total) are missing the `//go:build integration` tag. This could cause them to fail build tag filtering. All PG-focused integration tests (53) are correctly tagged.

---

## Summary Table

| Area | Status | Blockers |
|------|--------|----------|
| Backend compile (default) | ✓ PASS | None |
| Backend compile (sqliteonly) | ✓ PASS | None |
| Go vet (all tags) | ✓ PASS | None |
| Frontend TypeScript | ✓ PASS | None |
| Frontend build (pnpm) | ✓ PASS | None |
| Makefile e2e targets | ⚠ Partial | `test-e2e-short`, `test-e2e-full`, `test-release-gate` missing |
| CI workflow e2e jobs | ⚠ Missing | `e2e-fast`, `e2e-full` jobs not defined |
| Test build-tag discovery (e2e) | ✓ 100% | None |
| Test build-tag discovery (integration) | ⚠ 93% | 4 SQLite test files missing tag |

---

## Concerns & Blockers

### CONCERN 1: Missing E2E Makefile targets
**Severity:** Medium  
**Impact:** Plan Phase 14 references `test-e2e-short` and `test-e2e-full` but Makefile has only `test-e2e`.  
**Action needed:** Clarify if this is intentional (env-var gating of LLM) or if targets need renaming/splitting.

### CONCERN 2: Missing E2E CI jobs
**Severity:** Medium  
**Impact:** Plan specifies `e2e-fast` (blocks PR, skip LLM) and `e2e-full` (nightly, real LLM). CI workflow does not define these.  
**Action needed:** Add job definitions or confirm whether manual runs are intended.

### BLOCKER 3: SQLite integration test files missing build tag
**Severity:** Low-Medium  
**Impact:** 4 SQLite-specific integration test files lack `//go:build integration`, may not be discovered/run by `go test -tags integration`.  
**Action needed:** Add `//go:build integration` to:
- `tests/integration/sqlite_vault_visibility_matrix_test.go`
- `tests/integration/tts_dual_read_sqlite_test.go`
- `tests/integration/sqlite_vault_shared_docs_test.go`
- `tests/integration/sqlite_smoke_test.go`

---

## Recommendation

**Status:** DONE_WITH_CONCERNS

Core compile and build health is solid. Frontend, backend (both PG and SQLite), and vet checks all pass. Test files are nearly 100% correctly tagged (61/61 e2e, 53/57 integration).

**Before Phase 14 phase-gate:** 
1. Verify intent of `test-e2e` (single target with env var) vs plan's `test-e2e-short`/`test-e2e-full` split.
2. Add missing build tags to 4 SQLite integration test files.
3. Define E2E-specific CI jobs if required for PR blocking / nightly runs.

All technical infrastructure is ready; planning alignment is needed.
