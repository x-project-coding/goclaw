package http

import (
	"log/slog"
	"net/http"

	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// requireUserAdmin verifies the caller has owner or admin role. Replaces the
// legacy requireTenantAdmin for v4 single-tenant model — role lookup uses
// context only, no TenantStore round-trip.
func requireUserAdmin(w http.ResponseWriter, r *http.Request) bool {
	ctx := r.Context()
	role := store.RoleFromContext(ctx)
	if role == store.TenantRoleOwner || role == store.TenantRoleAdmin {
		return true
	}
	locale := store.LocaleFromContext(ctx)
	writeJSON(w, http.StatusForbidden, map[string]string{
		"error": i18n.T(locale, i18n.MsgPermissionDenied, "admin access"),
	})
	return false
}

// requireMasterScope guards endpoints that write to global (non-user-scoped)
// tables or execute server-wide side effects (shell, filesystem). Rejects
// callers whose ctx is not master-scope. System owners bypass.
//
// Returns true on allow, false on deny (in which case a 403 response has
// already been written). Emits security.tenant_scope_violation slog on deny.
func requireMasterScope(w http.ResponseWriter, r *http.Request) bool {
	ctx := r.Context()
	if store.IsMasterScope(ctx) {
		return true
	}
	slog.Warn("security.tenant_scope_violation",
		"path", r.URL.Path,
		"method", r.Method,
		"tenant_id", store.TenantIDFromContext(ctx),
		"user_id", store.UserIDFromContext(ctx),
	)
	locale := store.LocaleFromContext(ctx)
	writeJSON(w, http.StatusForbidden, map[string]string{
		"error": i18n.T(locale, i18n.MsgMasterScopeRequired),
	})
	return false
}
