# ADR: Defer Per-User Vault Encryption to v4.x

**Date:** 2026-05  
**Status:** Accepted  
**Deciders:** v4 schema design review

---

## Context

During v4 schema design, an audit raised the question of whether vault documents
should store per-user encryption keys in the schema — allowing each user's vault
content to be encrypted at rest with a key derived from their credentials, so that
even a compromised database host cannot read another user's vault data.

The v3 schema had no vault encryption at the row level (only the global
`GOCLAW_ENCRYPTION_KEY` for API key/credential fields).

The v4 greenfield schema is a clean break. Adding per-user encryption columns now
would require:

1. A `vault_encryption_keys` table (or a key column per vault document row).
2. Key derivation logic in Phase 06 (auth bootstrap).
3. Encrypt/decrypt wrappers in the vault store layer.
4. A migration path for any bootstrapped content written before the feature lands.

The v4 EPIC-04 scope is limited to schema shape + store interface contracts. The
auth bootstrap (Phase 06) and vault store layer (Phase 05) are separate work streams
that are not yet complete.

## Decision

Per-user vault encryption is **deferred to v4.x** (a follow-up EPIC after core v4
ships).

The v4 initial schema (`migrations/000001_initial.up.sql`) does **not** include any
per-user encryption key columns on `vault_documents`, `vault_versions`, or any
related table.

The global `config_secrets` table continues to hold encrypted values using
`GOCLAW_ENCRYPTION_KEY` (AES-256-GCM), which protects credentials and API keys.

## Consequences

**Positive:**
- Simpler schema for v4.0 launch.
- No key-rotation complexity at initial release.
- Phase 05 store contracts are not blocked on crypto primitives.

**Negative / Accepted risks:**
- Vault document content is stored in plaintext in the database. A direct DB read
  by an operator with `SELECT` access can read all vault content.
- This is accepted under the "trust admin model" (Q-14): in a single-tenant
  deployment, the root/admin operator is assumed trusted.

**Follow-up:**
- Vault encryption to be designed as a standalone EPIC, targeting v4.x.
- Design must cover: key derivation (PBKDF2/Argon2), key storage (separate table or
  OS keyring for desktop), and migration of existing plaintext rows.
- Track in project roadmap under "Security Hardening".
