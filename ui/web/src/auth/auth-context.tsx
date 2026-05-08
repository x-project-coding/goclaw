// Auth context — wraps use-auth-store with React-friendly login/logout/refresh actions.
// Tokens live in the Zustand store (persisted to localStorage); user profile is fetched lazily.

import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from "react";
import { useAuthStore } from "@/stores/use-auth-store";
import { RefreshInterceptor, type RefreshTokensResult } from "./refresh-interceptor";
import { isExpired, isExpiringSoon } from "./jwt";

export interface AuthUser {
  userId: string;
  email: string;
  role: string;
  status: string;
  displayName?: string;
}

export interface AuthContextValue {
  user: AuthUser | null;
  accessToken: string;
  isAuthenticated: boolean;
  loading: boolean;
  login: (email: string, password: string) => Promise<void>;
  logout: () => Promise<void>;
  refresh: () => Promise<RefreshTokensResult>;
  reloadUser: () => Promise<void>;
}

const AuthContext = createContext<AuthContextValue | null>(null);

interface LoginResponse {
  access_token: string;
  refresh_token: string;
  user_id: string;
  role: string;
}

interface MeResponse {
  user_id: string;
  email: string;
  role: string;
  status: string;
  display_name?: string;
}

// X-Requested-With trips a CORS preflight on cross-origin form submits, so
// the BE CSRF middleware (internal/http/csrf_middleware.go) requires it on
// every mutating call. The shared HttpClient sets it automatically; these
// pre-auth helpers run before HttpClient is wired so the header is set
// inline here. Keep this in sync with api/http-client.ts:headers().
async function postJSON<T>(path: string, body: unknown): Promise<T> {
  const res = await fetch(path, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      "X-Requested-With": "XMLHttpRequest",
    },
    body: JSON.stringify(body),
  });
  if (!res.ok) {
    const data = await res.json().catch(() => ({}));
    const code = (data as { error?: string }).error ?? `http_${res.status}`;
    throw new Error(code);
  }
  return res.json() as Promise<T>;
}

async function getJSON<T>(path: string, token: string): Promise<T> {
  const res = await fetch(path, {
    headers: token ? { Authorization: `Bearer ${token}` } : {},
  });
  if (!res.ok) {
    const data = await res.json().catch(() => ({}));
    const code = (data as { error?: string }).error ?? `http_${res.status}`;
    const err = new Error(code) as Error & { status?: number };
    err.status = res.status;
    throw err;
  }
  return res.json() as Promise<T>;
}

export function AuthProvider({ children }: { children: ReactNode }) {
  const accessToken = useAuthStore((s) => s.token);
  const setTokens = useAuthStore((s) => s.setTokens);
  const storeLogout = useAuthStore((s) => s.logout);

  const [user, setUser] = useState<AuthUser | null>(null);
  const [loading, setLoading] = useState(true);

  const interceptor = useMemo(
    () =>
      new RefreshInterceptor({
        getRefreshToken: () => useAuthStore.getState().refreshToken,
        setTokens: (a, r) => {
          const userId = useAuthStore.getState().userId;
          setTokens(a, r, userId);
        },
        onRefreshFailed: () => {
          storeLogout();
          setUser(null);
        },
      }),
    [setTokens, storeLogout],
  );

  const reloadUser = useCallback(async () => {
    const tok = useAuthStore.getState().token;
    if (!tok) {
      setUser(null);
      return;
    }
    try {
      const me = await getJSON<MeResponse>("/v1/auth/me", tok);
      setUser({
        userId: me.user_id,
        email: me.email,
        role: me.role,
        status: me.status,
        displayName: me.display_name,
      });
    } catch (err) {
      const status = (err as { status?: number }).status;
      if (status === 401 && useAuthStore.getState().refreshToken) {
        try {
          const tokens = await interceptor.refresh();
          const me = await getJSON<MeResponse>("/v1/auth/me", tokens.accessToken);
          setUser({
            userId: me.user_id,
            email: me.email,
            role: me.role,
            status: me.status,
            displayName: me.display_name,
          });
          return;
        } catch {
          // Refresh succeeded but /me still failed, OR refresh itself failed.
          // Either way, the session is unrecoverable — clear tokens so we don't
          // sit with phantom credentials in localStorage.
          storeLogout();
          setUser(null);
          return;
        }
      }
      // Non-401 (e.g., 500, network) — clear user but DO NOT clear tokens
      // (transient failure; let the next request retry).
      setUser(null);
    }
  }, [interceptor, storeLogout]);

  // Initial load: if token exists, hydrate user. If access token expired, try refresh first.
  useEffect(() => {
    let cancelled = false;
    (async () => {
      const tok = useAuthStore.getState().token;
      const ref = useAuthStore.getState().refreshToken;
      if (!tok && !ref) {
        if (!cancelled) setLoading(false);
        return;
      }
      if ((!tok || isExpiringSoon(tok)) && ref) {
        try {
          await interceptor.refresh();
        } catch {
          if (!cancelled) {
            setUser(null);
            setLoading(false);
          }
          return;
        }
      }
      await reloadUser();
      if (!cancelled) setLoading(false);
    })();
    return () => {
      cancelled = true;
    };
  }, [interceptor, reloadUser]);

  const login = useCallback(
    async (email: string, password: string) => {
      const data = await postJSON<LoginResponse>("/v1/auth/login", { email, password });
      setTokens(data.access_token, data.refresh_token, data.user_id);
      await reloadUser();
    },
    [setTokens, reloadUser],
  );

  const logout = useCallback(async () => {
    const tok = useAuthStore.getState().token;
    if (tok) {
      // Best-effort revoke; ignore errors.
      await fetch("/v1/auth/logout", {
        method: "POST",
        headers: {
          Authorization: `Bearer ${tok}`,
          "X-Requested-With": "XMLHttpRequest",
        },
      }).catch(() => undefined);
    }
    interceptor.reset();
    storeLogout();
    setUser(null);
  }, [interceptor, storeLogout]);

  const refresh = useCallback(() => interceptor.refresh(), [interceptor]);

  const value = useMemo<AuthContextValue>(
    () => ({
      user,
      accessToken,
      isAuthenticated: !!user && !!accessToken && !isExpired(accessToken),
      loading,
      login,
      logout,
      refresh,
      reloadUser,
    }),
    [user, accessToken, loading, login, logout, refresh, reloadUser],
  );

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

export function useAuth(): AuthContextValue {
  const ctx = useContext(AuthContext);
  if (!ctx) throw new Error("useAuth must be used within AuthProvider");
  return ctx;
}
