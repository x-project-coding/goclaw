#!/bin/sh
# Verifies no live GoClaw tenant_id / tenantCond code remains in source.
# Allows the header comment in migrations/000001 that states "No tenant_id columns".
# STT proxy external API fields (stt_tenant_id, tenant_id form field) are separate
# from GoClaw multi-tenancy and are allowed by this check — they live in
# audio/, channels/media/, channels/discord/, channels/feishu/, config/.
set -e
matches=$(grep -rn "tenant_id\|WithTenantID\|tenantCond\|TenantID" internal/ migrations/ pkg/ \
  --include='*.go' --include='*.sql' \
  | grep -v '_test.go' \
  | grep -v 'No tenant_id columns' \
  | grep -v 'stt_tenant_id\|STTTenantID\|sttTenantIDField\|tenant identifier' \
  || true)
[ -z "$matches" ] || { echo "tenant residue found:"; echo "$matches"; exit 1; }
echo "tenant purge: OK"
