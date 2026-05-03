# ADR: Activity-Logs Retention Cron Deferred to v4.x

**Date:** 2026-05
**Status:** Accepted (deferral)
**Deciders:** v4 schema design review (Q-14 audit MISS-3)

---

## Context

The v4 schema introduces an `activity_logs` table to record audit-grade events
(authentication, agent edits, schema mutations, RBAC changes, sensitive tool
invocations). Audit log tables grow monotonically — without a retention/compaction
strategy they will eventually dominate the database footprint of a long-running
single-user instance.

During v4.0 design review (logged as audit MISS-3), the question was raised whether
v4.0 should ship a built-in retention worker (cron) that deletes or archives rows
older than N days.

## Decision

**Defer the retention cron to a v4.x minor release.** v4.0 ships `activity_logs`
**without** automatic pruning.

For v4.0:
- The table grows unbounded.
- Operators are expected to monitor disk growth manually.
- A documented `psql` snippet (`DELETE FROM activity_logs WHERE created_at < now() - interval '90 days'`)
  is acceptable for ad-hoc cleanup.

## Rationale

1. **v4.0 single-user scope.** A typical single-user instance produces ~10–100
   activity rows per day. At that rate, the table reaches 1M rows after ~30 years
   of continuous use. The growth pressure is an order of magnitude smaller than for
   any team / multi-tenant scenario, so retention is not on the critical path for
   v4.0 ship.
2. **Retention policy is not yet decided.** A cron worker forces a policy choice
   (delete vs archive, fixed window vs size-based, per-event-class vs global). v4.0
   does not have enough operational data to commit to a default. Shipping a cron
   with the wrong policy and reversing it later is more costly than shipping nothing.
3. **No regulatory driver.** v4.0 has no compliance regime that mandates retention
   bounds. When such a driver appears (e.g., a self-hosted enterprise edition with
   GDPR-style erasure obligations), retention design will be re-opened with the
   real constraints in hand.

## Consequences

- **Operators must monitor disk growth.** The desktop edition (Lite) ships SQLite,
  where unbounded `activity_logs` growth is bounded by the SQLite file size — at
  worst causes slow startup. The standard edition ships PostgreSQL, where DBA tools
  surface the growth.
- **No automatic pruning.** Operators who want pruning today MUST run a manual
  `DELETE` or set up an external cron via `psql`. This is documented in
  `env.e2e-tests/README.md` troubleshooting (and will move to ops docs in v4.x).
- **v4.x design open question.** Whether the retention worker is implemented as a
  cron in `internal/cron/`, a background worker in `internal/eventbus/`, or a
  one-shot CLI command (`./goclaw activity-logs prune`) is deferred to v4.x review.

## Re-opening Triggers

This ADR should be revisited (and likely reversed) when ANY of the following occur:

- A user reports `activity_logs` exceeding 1 GB in their deployment.
- A regulatory driver requires retention bounds (GDPR, SOC2).
- v4.x adds high-frequency event sources (e.g., per-tool-call logging) that make
  growth materially faster than the 10–100 rows/day baseline.

Until then, manual operator monitoring is the contract.
