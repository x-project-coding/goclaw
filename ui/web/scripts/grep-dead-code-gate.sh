#!/usr/bin/env bash
# grep-dead-code-gate.sh — CI gate that fails when removed/legacy v3 terms
# leak back into ui/web/src. Wired into .github/workflows/ci.yaml so a PR
# that re-introduces any of these terms blocks merge.
#
# Two passes:
#   1. Hard-removed identifiers — must be 0 hits anywhere in src.
#   2. "tenant"-prefixed strings — line-anchored allowlist for the two
#      cases that are intentionally still present:
#        - tools.json proxy_tenant_id (external API passthrough)
#        - backup.json tenantBackup i18n key (legacy alias kept 1 release
#          for cache-bust window per plan #1 P05; drop in next release)
set -euo pipefail

cd "$(dirname "$0")/../src"

fail() { echo "FAIL: $1" >&2; exit 1; }

# --- Pass 1: identifiers that must not exist at all ---
# Note: each `grep` chain is wrapped in `|| true` because grep returns exit 1
# when no lines match — under `set -o pipefail` that would kill the script
# on the happy path (zero hits is exactly the success case we want).
for term in is_system RequireCrossTenant "HookScopeEnum.tenant" merged_tenant_user_id; do
  matches=$({ grep -rn "$term" . --include="*.ts" --include="*.tsx" --include="*.json" 2>/dev/null || true; } \
            | { grep -v "__tests__" || true; } \
            | { grep -v "/i18n/locales/" || true; })
  if [ -n "$matches" ]; then
    echo "FAIL: '$term' has production hits:" >&2
    echo "$matches" >&2
    exit 1
  fi
done

# --- Pass 2: tenant residue with line-anchored allowlist ---
# File-level allowlists hide regressions in the same file; each allowed
# instance is matched by its full line shape.
allow_re='/tools\.json:.*"proxy_tenant_id"|/i18n/locales/(en|vi|zh)/backup\.json:[0-9]+:[[:space:]]*"tenantBackup"'

unexpected=$({ grep -rEn '"tenant' . --include="*.ts" --include="*.tsx" --include="*.json" 2>/dev/null || true; } \
             | { grep -v "__tests__" || true; } \
             | { grep -vE "$allow_re" || true; })
if [ -n "$unexpected" ]; then
  echo "FAIL: tenant residue found outside allowlist:" >&2
  echo "$unexpected" >&2
  exit 1
fi

echo "OK: dead-code grep gate passes."
