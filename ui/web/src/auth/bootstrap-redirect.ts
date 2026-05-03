// Bootstrap redirect — when backend returns 503 with bootstrap_required,
// the gateway has no admin user yet. Redirect the user to /bootstrap.
//
// Backend contract (Phase 06): any non-bootstrap endpoint returns
//   503 { error: { code: "bootstrap_required", ... } } until POST /v1/bootstrap/init succeeds.

import { ApiError } from "@/api/errors";

const BOOTSTRAP_PATH = "/bootstrap";

/**
 * Inspect an error and redirect to /bootstrap if it indicates the gateway is uninitialized.
 *
 * Uses `location.replace` (not `history.push`) intentionally: a hard reload at /bootstrap
 * resets all in-memory React state and remounts AuthProvider, so any stale tokens / user
 * objects from a prior session can't leak into the bootstrap form.
 */
export function handleBootstrapRedirect(err: unknown): boolean {
  if (!isBootstrapRequired(err)) return false;
  if (typeof window === "undefined") return false;
  if (window.location.pathname === BOOTSTRAP_PATH) return false; // already there
  window.location.replace(BOOTSTRAP_PATH);
  return true;
}

/** True when the response signals the gateway needs initial bootstrap. */
export function isBootstrapRequired(err: unknown): boolean {
  if (!(err instanceof ApiError)) return false;
  // Backend (auth_middleware.go) returns flat 503 body { error: "bootstrap_required", message: ... }.
  // The http-client unwraps the flat string into ApiError.message; the nested form would land in code.
  if (err.code === "bootstrap_required" || err.code === "BOOTSTRAP_REQUIRED") return true;
  if (err.message === "bootstrap_required") return true;
  return false;
}
