package store

import "github.com/google/uuid"

// MasterTenantID is the well-known UUID used by the gateway WS event filter
// and client.tenantID routing. In v4 single-tenant every client is assigned
// this ID at connect time so event scoping still works correctly.
// Phase 13 removes all references once per-user event routing replaces it.
var MasterTenantID = uuid.MustParse("0193a5b0-7000-7000-8000-000000000001")

// Tenant status constants — legacy compat. Phase 13 removes.
const (
	TenantStatusActive    = "active"
	TenantStatusSuspended = "suspended"
	TenantStatusArchived  = "archived"
)

// Tenant role constants — legacy compat. Phase 13 removes.
const (
	TenantRoleOwner    = "owner"
	TenantRoleAdmin    = "admin"
	TenantRoleOperator = "operator"
	TenantRoleMember   = "member"
	TenantRoleViewer   = "viewer"
)
