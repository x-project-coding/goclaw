package http

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// chainHandler builds a tiny next handler that records whether it ran. The
// CSRF middleware must invoke it on accept and skip it on reject — that
// distinction is what we are testing.
func chainHandler() (http.Handler, *bool) {
	called := false
	h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	})
	return h, &called
}

// TestCSRF_SafeMethodsBypass keeps GET/HEAD/OPTIONS exempt — they must not
// require the custom header even when it is missing.
func TestCSRF_SafeMethodsBypass(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodOptions} {
		next, called := chainHandler()
		mw := CSRFRequireXRequestedWith(next)

		req := httptest.NewRequest(method, "/v1/agents", nil)
		w := httptest.NewRecorder()
		mw.ServeHTTP(w, req)

		if !*called {
			t.Errorf("%s: expected next handler to run, but it was blocked", method)
		}
	}
}

// TestCSRF_MutatingWithoutHeaderRejected covers the core defense: a mutating
// request that lacks X-Requested-With must be rejected with 403 before the
// downstream handler runs.
func TestCSRF_MutatingWithoutHeaderRejected(t *testing.T) {
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		next, called := chainHandler()
		mw := CSRFRequireXRequestedWith(next)

		req := httptest.NewRequest(method, "/v1/agents", nil)
		w := httptest.NewRecorder()
		mw.ServeHTTP(w, req)

		if *called {
			t.Errorf("%s: next handler ran without the header — expected reject", method)
		}
		if w.Code != http.StatusForbidden {
			t.Errorf("%s: status = %d, want 403", method, w.Code)
		}
	}
}

// TestCSRF_MutatingWithHeaderPasses confirms the FE happy path — the header
// presence (any value) lets the request through.
func TestCSRF_MutatingWithHeaderPasses(t *testing.T) {
	next, called := chainHandler()
	mw := CSRFRequireXRequestedWith(next)

	req := httptest.NewRequest(http.MethodPost, "/v1/agents", nil)
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	w := httptest.NewRecorder()
	mw.ServeHTTP(w, req)

	if !*called {
		t.Errorf("expected next handler to run when header is set; status=%d", w.Code)
	}
}

// TestCSRF_BootstrapAndOAuthBypass: the install flow can't set the header
// (no FE bundle yet) and OAuth callbacks come from a foreign IdP redirect
// that also can't set custom headers — both must remain reachable.
func TestCSRF_BootstrapAndOAuthBypass(t *testing.T) {
	bypassPaths := []string{
		"/v1/bootstrap/init",
		"/v1/bootstrap/status",
		"/v1/auth/oauth/google/callback",
	}
	for _, path := range bypassPaths {
		next, called := chainHandler()
		mw := CSRFRequireXRequestedWith(next)

		req := httptest.NewRequest(http.MethodPost, path, nil)
		w := httptest.NewRecorder()
		mw.ServeHTTP(w, req)

		if !*called {
			t.Errorf("%s: expected whitelist bypass; status=%d", path, w.Code)
		}
	}
}
