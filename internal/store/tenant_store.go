package store

import "github.com/google/uuid"

// MasterTenantID is the legacy default tenant UUID. The v4 schema has no
// tenants table; this constant is kept so deferred call sites still compile.
// Phase 13 removes all references and deletes this var.
var MasterTenantID = uuid.MustParse("0193a5b0-7000-7000-8000-000000000001")

// Tenant status constants — legacy compat for callers that still compare
// against status strings. Phase 13 removes.
const (
	TenantStatusActive    = "active"
	TenantStatusSuspended = "suspended"
	TenantStatusArchived  = "archived"
)

// Tenant role constants — legacy compat for callers comparing role strings.
// Hierarchy (highest to lowest): owner > admin > operator > member > viewer.
// Phase 13 unifies with permissions.Role* and removes these.
const (
	TenantRoleOwner    = "owner"
	TenantRoleAdmin    = "admin"
	TenantRoleOperator = "operator"
	TenantRoleMember   = "member"
	TenantRoleViewer   = "viewer"
)
