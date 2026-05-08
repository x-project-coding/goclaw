// Refresh interceptor — handles 401 by rotating the refresh token.
// Single-flight: parallel 401s share one refresh call to avoid token thrash.
//
// Backend contract (Phase 06):
//   POST /v1/auth/refresh  body { "refresh_token": "<token>" }
//     200 → { access_token, refresh_token } (rotated; old refresh is now revoked)
//     401 → refresh expired/revoked → caller MUST log out

export interface RefreshTokensResult {
  accessToken: string;
  refreshToken: string;
}

export interface RefreshDeps {
  /** Read current refresh token from store. */
  getRefreshToken: () => string;
  /** Persist rotated tokens. */
  setTokens: (accessToken: string, refreshToken: string) => void;
  /** Called when refresh fails (e.g., expired/revoked) — caller logs out + redirects. */
  onRefreshFailed: () => void;
  /** Base URL for API calls. */
  baseUrl?: string;
  /** Override fetch (for tests). */
  fetchImpl?: typeof fetch;
}

interface RefreshResponse {
  access_token?: string;
  refresh_token?: string;
}

export class RefreshInterceptor {
  private inflight: Promise<RefreshTokensResult> | null = null;

  constructor(private deps: RefreshDeps) {}

  /**
   * Refresh tokens using single-flight: concurrent calls share the same Promise.
   * Returns rotated tokens or throws on failure (caller should log out).
   */
  async refresh(): Promise<RefreshTokensResult> {
    if (this.inflight) return this.inflight;

    this.inflight = this.doRefresh().finally(() => {
      this.inflight = null;
    });
    return this.inflight;
  }

  /** Reset in-flight state — for tests + after logout. */
  reset(): void {
    this.inflight = null;
  }

  private async doRefresh(): Promise<RefreshTokensResult> {
    const token = this.deps.getRefreshToken();
    if (!token) {
      this.deps.onRefreshFailed();
      throw new Error("no_refresh_token");
    }

    const fetchFn = this.deps.fetchImpl ?? fetch;
    const url = new URL("/v1/auth/refresh", this.deps.baseUrl || window.location.origin).toString();

    let res: Response;
    try {
      res = await fetchFn(url, {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          // Required by the BE CSRF middleware on every mutation. Keep in
          // sync with auth-context.tsx::postJSON + api/http-client.ts.
          "X-Requested-With": "XMLHttpRequest",
        },
        body: JSON.stringify({ refresh_token: token }),
      });
    } catch {
      // Network failure — surface but DO NOT log user out (transient).
      throw new Error("refresh_network_error");
    }

    if (!res.ok) {
      // 401/403 → refresh expired/revoked. Force logout.
      this.deps.onRefreshFailed();
      throw new Error(`refresh_failed_${res.status}`);
    }

    const data = (await res.json().catch(() => ({}))) as RefreshResponse;
    if (!data.access_token || !data.refresh_token) {
      this.deps.onRefreshFailed();
      throw new Error("refresh_invalid_response");
    }

    this.deps.setTokens(data.access_token, data.refresh_token);
    return { accessToken: data.access_token, refreshToken: data.refresh_token };
  }
}
