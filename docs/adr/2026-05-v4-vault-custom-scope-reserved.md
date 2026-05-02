# ADR: Keep 'custom' in vault_documents.scope CHECK Constraint

**Date:** 2026-05  
**Status:** Accepted  
**Deciders:** v4 schema design review

---

## Context

The `vault_documents` table has a `scope` column constrained to one of four values:
`'personal'`, `'team'`, `'shared'`, `'custom'`. This mirrors the v3 schema where
`custom` was introduced to support plugin/integration use-cases that do not fit the
three standard ownership models.

During v4 schema design, a question arose (logged as LOG-2) about whether `'custom'`
should be dropped from the CHECK constraint in the greenfield schema, given that:

- No built-in v4 feature uses `custom` scope at launch.
- Removing it would simplify the scope consistency CHECK constraint.
- It could always be added back later via a migration.

## Decision

**Keep `'custom'` in the `scope` CHECK constraint.**

The `vault_documents.scope` column retains:
```sql
CONSTRAINT vault_documents_scope_check CHECK (
    scope IN ('personal', 'team', 'shared', 'custom')
)
```

The scope consistency constraint intentionally has no structural requirements for
`custom` rows (agent_id and team_id may be NULL or non-NULL), giving integrations
full flexibility.

## Rationale

1. **Backward compatibility surface**: v3 data exports and any integration that
   writes `scope = 'custom'` would fail validation if the value is removed. Although
   v4 is a greenfield schema, keeping `custom` costs nothing.

2. **Extension point by design**: The `custom_scope` column (a free-form text field)
   combined with `scope = 'custom'` forms an intentional plugin boundary. Removing
   `custom` from the CHECK would force future integrations to use `shared` as a
   catch-all, which conflates two distinct concepts.

3. **Zero schema cost**: Adding a string literal to a CHECK constraint has no storage
   or query-plan impact.

4. **Re-adding is a breaking change**: If `custom` were dropped now and an integration
   attempted to insert a `custom`-scoped row before the constraint was restored, it
   would receive a CHECK violation error. Keeping it avoids this window.

## Consequences

**Positive:**
- Integration plugins can use `scope = 'custom'` without a schema migration.
- The `custom_scope` TEXT column remains meaningful.

**Negative / Accepted:**
- Four possible `scope` values slightly increases the surface for incorrect usage
  (e.g., application code using `custom` when `personal` was intended).
- Mitigated by: application-layer validation in store methods; documented convention
  that `custom` is for integrations only.

**Follow-up:**
- Document the `custom` scope contract in the vault store interface (Phase 05).
- Consider adding a `source` or `integration_id` column to `vault_documents` in
  v4.x if the number of integration-scoped use-cases grows.
