import { create } from "zustand";
import { persist } from "zustand/middleware";

type UserRole = "owner" | "admin" | "operator" | "viewer" | "";
export type Edition = "standard" | "lite";

interface AuthState {
  // v4 password auth: token = JWT access token. refreshToken kept separate.
  // Existing 60+ callers read `token` directly — keep field name stable to avoid wide refactor.
  token: string;
  refreshToken: string;
  userId: string;
  senderID: string; // browser pairing: persistent device identity
  connected: boolean;
  role: UserRole; // server-assigned role from connect response
  serverInfo: { name?: string; version?: string } | null;
  edition: Edition; // server edition — UI feature gating

  setCredentials: (token: string, userId: string) => void;
  setTokens: (accessToken: string, refreshToken: string, userId: string) => void;
  setPairing: (senderID: string, userId: string) => void;
  setConnected: (connected: boolean, serverInfo?: { name?: string; version?: string }) => void;
  setRole: (role: UserRole) => void;
  setEdition: (edition: Edition) => void;
  logout: () => void;
}

export const useAuthStore = create<AuthState>()(
  persist(
    (set) => ({
      token: "",
      refreshToken: "",
      userId: "",
      senderID: "",
      connected: false,
      role: "" as UserRole,
      serverInfo: null,
      edition: "standard" as Edition,

      setCredentials: (token, userId) => {
        set({ token, userId });
      },

      setTokens: (accessToken, refreshToken, userId) => {
        set({ token: accessToken, refreshToken, userId });
      },

      setPairing: (senderID, userId) => {
        set({ senderID, userId });
      },

      setConnected: (connected, serverInfo) => {
        set({ connected, serverInfo: serverInfo ?? null });
      },

      setRole: (role) => {
        set({ role });
      },

      setEdition: (edition) => {
        set({ edition });
      },

      logout: () => {
        set({
          token: "", refreshToken: "", userId: "", senderID: "", connected: false, role: "", serverInfo: null,
          edition: "standard",
        });
      },
    }),
    {
      name: "goclaw:auth",
      version: 1,
      // v0 → v1: drop multi-tenant residue (tenantId, tenantSlug, tenants[]).
      // v3 stored these alongside credentials; v4 has no tenant scope.
      migrate: (persisted, oldVersion) => {
        if (!persisted || typeof persisted !== "object") return persisted;
        if (oldVersion < 1) {
          const s = persisted as Record<string, unknown>;
          delete s.tenantId;
          delete s.tenantSlug;
          delete s.tenants;
          delete s.activeTenantId;
        }
        return persisted;
      },
      partialize: (state) => ({
        token: state.token,
        refreshToken: state.refreshToken,
        userId: state.userId,
        senderID: state.senderID,
      }),
    }
  )
);
