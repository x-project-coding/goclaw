package http

import (
	"net/http"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// csrfHeaderRequired declares the header the FE always sets on mutating
// requests (`HttpClient.headers()` in ui/web/src/api/http-client.ts only
// emits `X-Requested-With: XMLHttpRequest` for non-GET).
//
// Why this works as a CSRF defense:
//   - A malicious cross-origin form submit cannot set custom headers without
//     a CORS preflight. Browsers transparently sample-pre-flight any request
//     that carries a custom header, and a third-party origin will fail the
//     preflight unless we explicitly allow it (we don't).
//   - GET/HEAD/OPTIONS are intentionally exempt — they are spec-defined
//     "safe" methods and must remain idempotent + side-effect-free.
//   - Bearer-token API clients (CLI, integrations) send the header too;
//     existing clients that do NOT use cookies are still subject to it,
//     which keeps the contract simple.
const csrfHeaderRequired = "X-Requested-With"

// csrfWhitelistedPaths bypass the header check entirely. We exempt the
// bootstrap and OAuth callback flows because they are reached either before
// the FE bundle has loaded or via a 302 redirect from a third party (the
// browser cannot set custom headers in either case).
var csrfWhitelistedPaths = []string{
	"/v1/bootstrap/",
	// OAuth callbacks land here from external IdPs; they must remain
	// reachable without a custom request header. The handler itself is
	// state-bound by the IdP-issued code/state so CSRF risk is contained.
	"/v1/auth/oauth/",
}

// CSRFRequireXRequestedWith blocks mutating HTTP requests that lack the
// configured custom header. The middleware is intentionally simple — there
// is no token store, no per-session secret. The single header check pairs
// with the existing JWT/cookie auth middleware: an attacker page on another
// origin still cannot forge a valid mutation because adding the header
// triggers a CORS preflight that we never approve for foreign origins.
//
// Order: install AFTER BootstrapRequiredMiddleware (so the bootstrap window
// is gated first) and BEFORE per-route auth wrappers (so we reject obvious
// CSRF attempts before we do any auth work).
func CSRFRequireXRequestedWith(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isMutatingMethod(r.Method) {
			next.ServeHTTP(w, r)
			return
		}
		if isCSRFWhitelisted(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		if r.Header.Get(csrfHeaderRequired) == "" {
			locale := store.LocaleFromContext(r.Context())
			writeJSON(w, http.StatusForbidden, map[string]string{
				"error":   "csrf_header_missing",
				"message": i18n.T(locale, i18n.MsgPermissionDenied, csrfHeaderRequired),
			})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func isMutatingMethod(m string) bool {
	switch m {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	}
	return false
}

func isCSRFWhitelisted(path string) bool {
	for _, prefix := range csrfWhitelistedPaths {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}
