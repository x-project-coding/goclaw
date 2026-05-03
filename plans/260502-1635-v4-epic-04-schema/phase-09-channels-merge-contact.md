# Phase 09 — Channels + Merge-Contact Fix (R1) + Pairing

## Context Links

- Master § 4.3 (Channels), § 4.8 (Merge-contact 8 critical files), § 5 R1 risk
- Decisions Q2, Q-A, Q11 (paired_devices)
- Phase 02 v3-baseline.md R1 evidence (sessions-not-migrated bug)
- Phase 05 (agent_sessions store renamed)
- Phase 07 (ContactCollector key refactored)

## Overview

- Priority: P0 (R1 bug fix)
- Status: pending
- Effort: 12 dev-days
- Description: Refactor 8 channel platforms (Telegram, WhatsApp, Discord, Feishu, Slack, Facebook, Zalo, Pancake) to UUID user_id. Fix R1 bug — merge-contact MUST migrate `agent_sessions` (v3 only migrated `user_context_files` + `memory_documents`). Refactor `paired_devices.user_id` (drop tenant_id, add user_id NULL FK). Add EventBus UserID validation at channel ingress (already wired in P05 PR-05D — use here).

## Key Insights

- R1 evidence in Phase 02 doc: v3 `internal/http/contact_merge_handlers.go` flips `merged_id` but does NOT call `UPDATE agent_sessions SET user_id=...`.
- Merge flow files: `contact_merge_handlers.go` (HTTP), `pg/channel_contacts.go` + `sqlitestore/contacts.go` (UPDATE merged_id), `pg/contact_resolve.go` + `sqlitestore/contacts.go` (60s TTL cache), `agents_context.go` (per-user data migration).
- 580 user_id refs across channels (master § 2 audit-corrected D-2: 580, not 526; broader regex incl userID/UserID/senderID).
- `InboundMessage.UserID` type stays string (UUID-as-string at channel boundary; convert to typed UUID inside store layer).
- WhatsApp uses native whatsmeow library — JID normalization (phone vs LID); same UUID conversion at ContactStore boundary.
- `paired_devices` schema (Phase 03+04): drop tenant_id, add `user_id UUID FK NULL` (NULL = pre-pair).

## Tests to write FIRST (TDD red step)

| Test file | Cases (must FAIL until impl green) |
|---|---|
| `tests/e2e/12_channels_test.go` | `TestTelegramChannelCRUD`, `TestDiscordChannelCRUD`, `TestFeishuChannelCRUD`, `TestZaloChannelCRUD`, `TestWhatsAppChannelCRUD` — create/list/update/delete via HTTP. `TestChannelInstanceNoTenantID` — channel_instances row written without tenant_id |
| `tests/e2e/12_pairing_test.go` | `TestPairingFlow` — generate pairing_request → device pairs → paired_devices row has user_id NULL initially → after first message → user_id populated. `TestPairedDevicesUserNullable` — schema permits NULL |
| `tests/e2e/12_pending_messages_test.go` | `TestPendingMessageBuffering` — message arrives BEFORE user paired → buffered in channel_pending_messages. `TestPendingMessageDelivery` — after pairing → pending messages flushed in order |
| `tests/e2e/12_merge_contact_R1_fix_test.go` | **CRITICAL** — `TestMergeContactMigratesSessions` — after merge, `agent_sessions.user_id` updated to merged user (R1 fix). `TestMergeContactMigratesContextFiles` — `user_context_files.user_id` migrated. `TestMergeContactMigratesMemoryDocuments` — `memory_documents.user_id` migrated. `TestMergeContactAtomic` — if any of the 3 UPDATEs fails, all roll back (transaction-safe) |
| <!-- RED-TEAM Finding 10 --> `tests/e2e/12_merge_atomic_concurrent_test.go` | `TestMergeContactDuringActiveSession` — start merge in goroutine A; concurrently goroutine B writes for source user (e.g., new `agent_sessions` row). Assert: B's write either fully lands in source (merge ran after) OR fully lands in target (merge ran before) — no row split. Asserts atomic TX shared across all 4 UPDATEs. |
| <!-- RED-TEAM Finding 7 --> `tests/e2e/12_merge_security_test.go` | `TestMergeRejectsUserToUserMerge` — source contact has `merged_id` already set (i.e., authenticated user) → 409. `TestMergeRequiresUnmergedSource` — only `merged_id IS NULL` sources accepted. `TestMergeAuditColumnPopulated` — post-merge, `channel_contacts.merge_audit` JSONB has `merged_by_user_id`, `merged_at`, `from_channel`. `TestMergeChainDepthCapped` — target user has `merged_id` set → reject (no chained merges). |
| `tests/e2e/12_inbound_message_uuid_test.go` | `TestInboundMessageHasUUIDUserID` — bus event from channel emits `DomainEvent.UserID` valid UUID; non-UUID logs warning (validate_user_id from P05) |
| `tests/e2e/12_contact_collector_v4_key_test.go` | `TestContactCollectorKeyV4Format` — seen-set key matches `channel:instance:sender:thread` (no tenant prefix; uses Phase 07 refactor) |
| `tests/e2e/12_whatsapp_jid_normalization_test.go` | `TestWhatsAppJIDToUUID` — phone-format JID + LID-format JID both resolve to same `users.id` UUID (via `channel_contacts.merged_id` lookup) |

**Red verification:** Tests fail because R1 bug still present + UUID conversion at channel boundary missing.

## Requirements

### Functional

#### R1 fix — merge-contact migrates `agent_sessions`

<!-- RED-TEAM Finding 10: R1 merge-contact "atomic TX" structurally false (CRITICAL) -->
**MANDATORY refactor BEFORE any new merge code:** v3 store layer does NOT expose `*sql.Tx` at the merge boundary. `MergeContacts` and `migrateContextFilesOnMerge` run on separate connections (verified in `internal/http/contact_merge_handlers.go:90,97`). Naive "wrap in TX" will be fictional unless the store layer is refactored first.

**Required steps (in order):**
1. **First step of Sub-09A** — extend store interface:
   - Add `MergeUserAggregate(ctx context.Context, sourceUsers []uuid.UUID, targetUser uuid.UUID) error` to `store.ContactStore` (or a new `MergeStore` interface). This single method owns the entire TX.
   - Implementations in `internal/store/pg/channel_contacts.go` and `internal/store/sqlitestore/contacts.go` must `BeginTx()` themselves and run all 4 UPDATEs on the same `*sql.Tx`.
2. Refactor `internal/http/contact_merge_handlers.go` to call ONLY `store.MergeUserAggregate(...)` — no per-table calls from HTTP layer.
3. Inside `MergeUserAggregate`:
   - `tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})`
   - `UPDATE channel_contacts SET merged_id=$1 WHERE id IN (...)`
   - `UPDATE agent_sessions SET user_id=$1 WHERE user_id = ANY($2)` ← R1 FIX
   - `UPDATE user_context_files SET user_id=$1 WHERE user_id = ANY($2)`
   - `UPDATE memory_documents SET user_id=$1 WHERE user_id = ANY($2)`
   - `tx.Commit()` (rollback on any error inside).
4. After commit: `ContactResolve.InvalidateCache(...)` for source user IDs.
5. Test `TestMergeContactDuringActiveSession` — concurrent goroutine writing for source user while merge runs. Atomicity proof: source-user write either lands in source-user data (merge happened after) OR in target-user data (merge happened before) — never split.
<!-- /RED-TEAM Finding 10 -->

<!-- RED-TEAM Finding 7: Channel merge-contact admin can silently hijack accounts (CRITICAL) -->
**Security constraints on merge:**
- **Restrict merge direction:** `channel_contacts.merged_id IS NULL` source (unauthenticated/unmerged contact) → authenticated user (target). NEVER merge user→user (cannot move data between two authenticated users).
- **Pre-merge guard in handler:** load all source contacts; assert each has `merged_id IS NULL`. If any source already merged → 409 with explicit error.
- **Notification:** email both source contact's last-known address (from channel metadata if present) AND target user on merge completion. Best-effort — if no email infra (Phase 06 limitation), log to `activity_logs` with `event='contact.merge_executed'`. Document email-fallback in v4.0.
- **Audit column (Phase 03 schema ripple):** `channel_contacts.merge_audit JSONB` — populated on merge with `{"merged_by_user_id": "<admin>", "merged_at": "<ts>", "from_channel": "<id>", "from_sender": "<raw>"}`.
- **WhatsApp JID merge:** phone-format and LID-format JIDs may both map to same user via `channel_contacts`. Hard-cap merge depth = 1 (no chained merges where contact A is merged into B is merged into C). Enforce by: at merge time, target user MUST NOT have `merged_id IS NOT NULL` (i.e., target itself was merged from elsewhere). Reject as 409 if violated.
<!-- /RED-TEAM Finding 7 -->

- Verify `internal/store/pg/contact_resolve.go` 60s TTL cache invalidates after merge (re-grep cache invalidate path).
- Update `internal/backup/tenant_restore_helpers.go` (Phase 08 refactored) — if it has migrate logic, ensure includes agent_sessions.

#### Channel user_id UUID conversion (~580 refs)

For each of 8 channel platforms (`internal/channels/{telegram,whatsapp,discord,feishu,slack,facebook,zalo,pancake}/`):

- At channel boundary (inbound message handler), after `channel_contacts` lookup yields `merged_id` (UUID), use UUID as `event.UserID` everywhere downstream.
- Drop tenant_id from `InboundMessage`, `OutboundMessage`, channel-specific structs.
- Update bus event publishers — populate `DomainEvent.UserID` from resolved UUID.

High-impact files (master § 4.3 verified):
- `internal/channels/manager.go`
- `internal/channels/telegram/channel.go` (41 refs)
- `internal/channels/discord/...`
- `internal/channels/slack/...`
- `internal/channels/feishu/...`
- `internal/channels/whatsapp/...` (whatsmeow JID path)
- `internal/channels/facebook/...`
- `internal/channels/zalo/...`
- `internal/channels/pancake/...`

#### Paired devices

- Schema (Phase 03+04 already): drop tenant_id, add `user_id UUID FK NULL`.
- Refactor `internal/store/pg/pairing.go` + `internal/store/sqlitestore/pairing.go`:
  - `Pair(deviceFingerprint, userID UUID NULL)` — accept NULL pre-pair.
  - `Bind(deviceID, userID UUID)` — set user_id once user authenticated.
- HTTP handler for `POST /v1/channels/:id/pair` returns pairing_request token.
- Channel manager (`internal/channels/manager.go`) calls `Bind` after first message resolves to user.

#### EventBus UserID at channel ingress

- Channel inbound handler emits `DomainEvent` with UserID = resolved UUID string.
- `validateUserID` (wired in P05 PR-05D) logs warning if non-UUID slips through.
- Episodic worker entry parses UUID — if fail, skip + metric (P05 PR-05D).

### Non-functional

- 8 platforms refactored sequentially OR parallelized (low-risk per platform; 12 days = ~1.5 days each).
- `go build ./...` after each platform.
- All channel-specific tests gated by `//go:build e2e` to avoid running offline.
- Real channel API calls SKIPPED in CI (gated by `testing.Short()` + env var presence).

## Architecture

```
Channel ingress flow (v4):
  External msg → channel adapter (e.g., Telegram bot)
   ├─ extract sender_raw (e.g., telegram_user_id "12345")
   ├─ ContactStore.ResolveByChannelSender(channelID, sender_raw)
   │    ├─ check 60s TTL cache
   │    ├─ if miss: SELECT FROM channel_contacts WHERE channel_id=$1 AND sender_id=$2
   │    └─ → merged_id (UUID) OR NULL (unmerged)
   ├─ if merged_id NULL → buffer in channel_pending_messages
   ├─ else → emit DomainEvent{UserID: merged_id (UUID), AgentID: ..., Type: "channel.inbound"}
   │    └─ validateUserID logs if malformed
   └─ pipeline consumes event

Merge-contact (R1 FIX + Findings 7, 10):
  Admin selects contacts {C1, C2, ...} + target user U
  → store.MergeUserAggregate(ctx, [C1,C2,...].user_ids, U.id)  // SINGLE store call (Finding 10)
  → Inside store: BeginTx → all 4 UPDATEs on same *sql.Tx
   ├─ Pre-check: source contacts merged_id IS NULL (Finding 7); target.merged_id IS NULL (no chained merges)
   ├─ UPDATE channel_contacts SET merged_id=$1, merge_audit=$audit WHERE id IN (...)  // Finding 7
   ├─ UPDATE agent_sessions SET user_id=$1 WHERE user_id = ANY($2)   ← R1 FIX
   ├─ UPDATE user_context_files SET user_id=$1 WHERE user_id = ANY($2)
   ├─ UPDATE memory_documents SET user_id=$1 WHERE user_id = ANY($2)
   ├─ COMMIT
  → After commit:
   ├─ ContactResolve.InvalidateCache(channelID, sender_ids)
   ├─ Notify source contact's last-known channel + target user (best-effort) (Finding 7)
   └─ Audit log activity_logs event='contact.merge_executed'
```

## Related Code Files

### Modify

- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/http/contact_merge_handlers.go` (R1 fix — add agent_sessions UPDATE in TX)
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/store/pg/channel_contacts.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/store/pg/contact_resolve.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/store/sqlitestore/contacts.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/store/contact_collector.go` (Phase 07 already; verify final shape)
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/store/contact_store.go` (interface)
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/store/pg/pairing.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/store/sqlitestore/pairing.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/store/pairing_store.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/channels/manager.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/channels/telegram/channel.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/channels/discord/*.go` (verify file list)
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/channels/feishu/*.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/channels/slack/*.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/channels/whatsapp/*.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/channels/facebook/*.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/channels/zalo/*.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/channels/pancake/*.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/bus/*.go` (InboundMessage struct — drop tenant_id)
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/agent/agents_context.go` (verify if used; refactor merge-side migration helper)

### Create

- `/Users/viettran/Documents/coding/next-level-builder/goclaw/tests/e2e/12_channels_test.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/tests/e2e/12_pairing_test.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/tests/e2e/12_pending_messages_test.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/tests/e2e/12_merge_contact_R1_fix_test.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/tests/e2e/12_inbound_message_uuid_test.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/tests/e2e/12_contact_collector_v4_key_test.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/tests/e2e/12_whatsapp_jid_normalization_test.go`

### Delete

- None (refactor in-place).

## Implementation Steps

### Sub-phase 09A — Merge-contact R1 fix (3 days, expanded for Findings 7+10)

1. Verify Phase 02 v3-baseline.md R1 evidence section exists.
2. Write `12_merge_contact_R1_fix_test.go` + `12_merge_atomic_concurrent_test.go` (Finding 10) + `12_merge_security_test.go` (Finding 7) — must fail.
<!-- RED-TEAM Finding 10 -->
3. **Refactor store BEFORE handler:** add `MergeUserAggregate(ctx, sourceUsers []uuid.UUID, targetUser uuid.UUID) error` to `store.ContactStore` interface.
4. Implement in `internal/store/pg/channel_contacts.go` — owns `BeginTx`, all 4 UPDATEs share `*sql.Tx`. Same in `internal/store/sqlitestore/contacts.go`.
5. Verify all 4 UPDATEs use parameterized `user_id = ANY($N)` against source user UUID list.
<!-- /RED-TEAM Finding 10 -->
<!-- RED-TEAM Finding 7 -->
6. Inside `MergeUserAggregate` pre-checks (BEFORE any UPDATE):
   - `SELECT merged_id FROM channel_contacts WHERE id = ANY($1)` — fail if any non-NULL.
   - `SELECT merged_id FROM channel_contacts WHERE id = $target_contact_id` — fail if non-NULL (no chained merges).
7. Build merge_audit JSONB: `{"merged_by_user_id": ctx.UserID, "merged_at": now, "from_channel_id": <id>, "from_sender_raw": <raw>}`.
8. Include `merge_audit` in the `UPDATE channel_contacts` SET clause.
9. After COMMIT: enqueue best-effort notification to source-contact-last-known + target user. If no email infra, write `activity_logs` row with `event='contact.merge_executed'` payload.
<!-- /RED-TEAM Finding 7 -->
10. Refactor `internal/http/contact_merge_handlers.go` to call `store.MergeUserAggregate(...)` only — drop direct DB calls.
11. Add `ContactResolve.InvalidateCache(...)` call post-commit (HTTP layer or store hook).
12. Verify HTTP handler returns 500 on TX rollback (test asserts).
13. All 3 R1/atomic/security tests green.

### Sub-phase 09B — Channel UUID boundary (6 days)

For each of 8 platforms in order (Telegram → Discord → Feishu → Slack → WhatsApp → Facebook → Zalo → Pancake):

1. Read inbound handler. Locate `sender_id` → `user_id` conversion (or absence).
2. Refactor: call `ContactStore.ResolveByChannelSender(channelID, senderRaw)` → UUID.
3. If no merged_id → buffer in `channel_pending_messages`; emit no DomainEvent yet.
4. If merged_id → emit `DomainEvent{UserID: merged_id, ...}`.
5. Drop tenant_id from InboundMessage/OutboundMessage.
6. `go build ./...` clean per platform.
7. After all 8: run `12_channels_test.go` + `12_inbound_message_uuid_test.go` + `12_whatsapp_jid_normalization_test.go` → green.

### Sub-phase 09C — Paired devices + pending messages (3 days)

1. Refactor `internal/store/pg/pairing.go` + SQLite mirror — accept `userID UUID NULL`.
2. Refactor `internal/store/pairing_store.go` interface signatures.
3. HTTP handler `POST /v1/channels/:id/pair` returns pairing_request token (existing endpoint; verify intact).
4. Channel manager `Bind(deviceID, userID UUID)` called after first message resolves.
5. Run `12_pairing_test.go` + `12_pending_messages_test.go` → green.
6. ContactCollector cache key v4 format already (Phase 07); run `12_contact_collector_v4_key_test.go` → green.

### Phase exit verification

- `go build ./...` + `go build -tags sqliteonly ./...` + `go vet ./...` clean.
- All 7 e2e channel tests green.
- Earlier phase tests still green.

## Todo List

- [ ] Sub-09A: R1 fix — sessions migration in merge TX
- [ ] Sub-09A: cache invalidation post-merge
- [ ] Sub-09A: e2e R1 test green
<!-- RED-TEAM Findings 7 + 10 todos -->
- [ ] (Finding 10) `MergeUserAggregate` store interface added — single TX owner
- [ ] (Finding 10) PG + SQLite impls share `*sql.Tx` across 4 UPDATEs
- [ ] (Finding 10) Atomic concurrent test green
- [ ] (Finding 7) Source contacts must have `merged_id IS NULL` (no user→user merge)
- [ ] (Finding 7) Target contact `merged_id IS NULL` (no chained merges)
- [ ] (Finding 7) `merge_audit` JSONB column populated on merge
- [ ] (Finding 7) Best-effort notification to source + target users
- [ ] (Finding 7) Merge security tests green (4 cases)
- [ ] Sub-09B: Telegram UUID boundary
- [ ] Sub-09B: Discord
- [ ] Sub-09B: Feishu
- [ ] Sub-09B: Slack
- [ ] Sub-09B: WhatsApp (JID normalization)
- [ ] Sub-09B: Facebook
- [ ] Sub-09B: Zalo
- [ ] Sub-09B: Pancake
- [ ] Sub-09C: paired_devices nullable user_id wiring
- [ ] Sub-09C: pairing token flow
- [ ] Sub-09C: pending message buffer + flush
- [ ] go build (PG + sqliteonly) + go vet clean
- [ ] All 7 e2e channel tests green
- [ ] Earlier phase tests still green

## Success Criteria

- All 7 e2e channel tests green.
- R1 bug VERIFIED FIXED — `agent_sessions.user_id` migrates atomically with `merged_id`.
- 8 channel platforms emit `DomainEvent.UserID` as valid UUID.
- `paired_devices.user_id` accepts NULL pre-pair, populated post-pair.
- ContactCollector cache key v4 format (no tenant prefix).
- WhatsApp JID phone+LID variants resolve to same UUID via channel_contacts.

## Risk Assessment

| Risk | Severity | Mitigation |
|---|---|---|
| R1 fix breaks existing merge data | High | Atomic TX rollback safe; e2e test asserts row counts pre+post |
| Channel platforms 8x refactor too parallel-unsafe | Med | Per-platform commit; build between each |
| WhatsApp whatsmeow library API change | Low | Library pinned via go.mod; test isolates JID normalization |
| External channel API rate limits during e2e | Med | Tests gated by `testing.Short()` + env var; CI runs `-short` |
| ContactResolve cache invalidation missed → stale merged_id | Med | Test 12_merge_contact asserts immediate read post-merge yields new user |
| Sessions migration overshoots (migrates wrong user's sessions) | High | UPDATE filtered by `user_id IN (source_user_ids)`; test asserts target user's pre-existing sessions untouched |

## Security Considerations

- Merge-contact handler requires `RoleAdmin` (Phase 06 middleware enforces).
<!-- RED-TEAM Finding 7 -->
- **Merge hijack prevention (CRITICAL):** RoleAdmin alone is NOT authorization — admin can compromise user data trivially without these guards:
  - Source contact MUST have `merged_id IS NULL` — no user→user data movement.
  - Target user's contact MUST have `merged_id IS NULL` — no chained merges (depth cap = 1).
  - `merge_audit JSONB` records who/when/from-where for forensics.
  - Best-effort notification to source + target on merge.
<!-- /RED-TEAM Finding 7 -->
<!-- RED-TEAM Finding 10 -->
- **Atomic merge transaction (CRITICAL):** all 4 UPDATEs (`channel_contacts.merged_id`+`merge_audit`, `agent_sessions.user_id`, `user_context_files.user_id`, `memory_documents.user_id`) MUST share single `*sql.Tx`. R1 "fix" without store-layer refactor = fictional atomicity.
<!-- /RED-TEAM Finding 10 -->
- ContactStore.ResolveByChannelSender uses parameterized queries (`$1, $2`).
- Pairing token = 32-byte cryptographic random; expires 5min (existing pattern).
- WhatsApp credentials per-user (Q-2 channel sender raw → channel_contacts mapping).
- No user enumeration via channel sender raw lookups (404 vs 200 distinction tightened in handler).

## Cross-phase Gates

- **Entry:** Phase 06 merged (auth) + Phase 07 merged (ContactCollector key) + Phase 05 PR-05D merged (validate_user_id observer + episodic UUID parse).
- **Exit:** All 7 channel e2e tests green + go build/vet clean. Gates Phase 11 (FE channels page) + Phase 14 final.

## Next Steps

- Phase 10 — skills + skill_versions + curator (S9 prep) can run after this OR parallel with 11.
- Phase 11 — FE channels page consumes channel API.
