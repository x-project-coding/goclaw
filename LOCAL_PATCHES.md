# Local Patches

Commits this fork (`x-project-coding/goclaw`) carries on top of upstream
(`nextlevelbuilder/goclaw`). Append new entries here when you land a fork-only
change; remove entries when an upstream sync supersedes them.

When syncing upstream (see [`UPSTREAM_SYNC.md`](UPSTREAM_SYNC.md)), walk this
list and re-verify each patch still applies cleanly or has been obsoleted.

---

## Active patches

### fix(agents): require provider/model on every agent-create ingress

- **Base upstream commit:** `c651cde5` (Merge branch 'dev' into main)
- **Files:**
  - `internal/http/agents_import_agent.go` — `doImportNewAgent` emits a
    `slog.Warn` when archives lack `provider`/`model` (single-agent import
    via `POST /v1/agents/import`). Rejecting outright (the first version of
    this patch) broke workspace signup because real production brand-agent
    archives currently omit these fields. The resolver-side guard below
    still surfaces a clear chat-time error.
  - `internal/http/agents.go` — `handleCreate` rejects requests with empty
    `provider`/`model` (direct create via `POST /v1/agents`) with the existing
    `MsgProviderModelRequired` i18n key.
  - `internal/http/teams_import.go` — team-import loop skips member archives
    with empty `provider`/`model` (`POST /v1/teams/import`).
  - `internal/agent/resolver.go` — `NewManagedResolver` returns a clear error
    (plus `slog.Warn`) when an existing agent row has empty `model`.
- **Why:** `buildAgentFromArchive` parses `provider`/`model` from the archive's
  `agent.json` (lines 88–90). When those keys are missing, both fields land as
  empty strings — the `NOT NULL` columns accept empty strings, so a broken row
  gets inserted silently. At chat time the provider adapter sends
  `{"model": ""}` to OpenRouter, which responds with
  `{"error":{"message":"No models provided"}}`. x-ui's `parseAgentError`
  extracts that and the user sees `⚠️ No models provided` with no breadcrumb to
  the real cause (the brand-agent archive). The import-time guards prevent new
  broken rows on all three ingress paths; the resolver-time guard makes legacy
  broken rows fail with an actionable message.
