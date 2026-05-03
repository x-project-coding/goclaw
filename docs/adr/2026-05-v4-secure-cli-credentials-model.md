# ADR: Secure CLI Uses Credentials Model, Not Binaries+Grants

**Date:** 2026-05
**Status:** Accepted (implementation diverges from master § 11.2 #17 spec)
**Deciders:** v4 EPIC-04 Phase 14 endpoint gap audit

---

## Context

Master research § 11.2 #17 (Secure CLI) specified an API shape mirroring the
generic skill-grants pattern:

- `GET /v1/secure-cli/binaries` — system binaries list
- `POST /v1/secure-cli/grants` — per-agent grants
- `GET/POST /v1/secure-cli/credentials` — per-user creds

The implementation that landed (pre-v4 → preserved) uses a **single
"credentials" model** under `/v1/cli-credentials/`:

- `GET /v1/cli-credentials` — list registered CLI tools
- `POST /v1/cli-credentials` — register a CLI tool with credentials
- `GET /v1/cli-credentials/{id}` — get a registered CLI tool
- `PUT /v1/cli-credentials/{id}` — update credentials
- `DELETE /v1/cli-credentials/{id}` — remove
- `GET /v1/cli-credentials/{id}/user-credentials/{userId}` — per-user override
- `POST /v1/cli-credentials/{id}/agent-grants` — per-agent grant
- `GET /v1/cli-credentials/presets` — preset binaries (claude, codex, etc.)
- `POST /v1/cli-credentials/{id}/test` — dry-run probe

The two shapes carry the same domain semantics — both grant agents/users access
to a CLI binary with credentials — but the wire format is different.

## Decision

**Keep the credentials-model implementation as-is. Update the spec reference,
not the code.** The diverging endpoint name is shipped, has store layer
support, and is consumed by the frontend. Renaming endpoints purely to match
the master research doc is pure churn for zero behavior gain and breaks the
existing FE.

For Phase 14 e2e tests:
- Tests target `/v1/cli-credentials/*` — not `/v1/secure-cli/*`.
- Coverage matches the credentials model (CRUD + per-user creds + per-agent
  grants + presets + dry-run).

## Rationale

1. **Implementation predates v4.** The credentials model was already shipped
   in v3 with FE consumers; v4 inherited it. The master research § 11 was
   aspirational on this row.
2. **No security difference.** Both shapes enforce the same RBAC: list
   binaries (read), grant access (admin), per-user creds (self or admin).
   Renaming does not improve isolation.
3. **Frontend dependency.** `ui/web/` consumes `/v1/cli-credentials` (verified
   via grep). Renaming the endpoint requires a synchronized FE migration with
   no user-visible benefit.

## Consequences

- **Phase 14 § 11.2 #17 tests** target `/v1/cli-credentials/*` endpoints.
- **Master research § 11.2 row 17** is now historically inaccurate; this ADR
  is the source of truth for the v4 wire format.
- **No code change required** — the implementation is unchanged.
- If a future v5+ wants to align endpoint naming with the rest of the
  grants-model resources (skills/mcp/etc.), a dedicated migration phase with
  versioned endpoints (`/v2/secure-cli/*` parallel to `/v1/cli-credentials/*`)
  would be the way — out of scope for v4.

## References

- Phase 14 endpoint gap audit:
  `plans/reports/researcher-260503-2238-phase-14-endpoint-gap.md` § 17
- Implementation: `internal/http/secure_cli.go`
- FE consumer: `ui/web/src/api/cli-credentials.ts`
