import { useCallback, useEffect, useRef, useState } from "react";
import i18next from "i18next";
import { useWs } from "@/hooks/use-ws";
import { useAuthStore } from "@/stores/use-auth-store";
import { Methods } from "@/api/protocol";
import { toast } from "@/stores/use-toast-store";
import { userFriendlyError } from "@/lib/error-utils";

export type ShareRole = "viewer" | "member" | "editor";

export interface AgentShare {
  id: string;
  agent_id: string;
  shared_with_user_id?: string | null;
  shared_with_team_id?: string | null;
  role: ShareRole;
  created_by: string;
  created_at: string;
  updated_at: string;
}

interface CreateShareInput {
  /** Provide exactly one of userId / teamId — the BE rejects both/neither. */
  userId?: string;
  teamId?: string;
  role: ShareRole;
}

/**
 * useAgentShares wraps the agents.shares.* WS RPC family for the agent-detail
 * Shares tab. Mirrors the BE invariant — exactly one of (userId|teamId) at a
 * time — and surfaces toast feedback on success/failure so the tab stays a
 * thin renderer.
 */
export function useAgentShares(agentId: string) {
  const ws = useWs();
  const connected = useAuthStore((s) => s.connected);
  const [shares, setShares] = useState<AgentShare[]>([]);
  const [loading, setLoading] = useState(false);
  const lastAgentRef = useRef<string | null>(null);

  const load = useCallback(async () => {
    if (!agentId || !connected) return;
    setLoading(true);
    try {
      const res = await ws.call<{ shares: AgentShare[] }>(Methods.AGENTS_SHARES_LIST, { agentId });
      setShares(res.shares ?? []);
    } catch (err) {
      toast.error(i18next.t("agents:shares.errors.loadFailed"), userFriendlyError(err));
    } finally {
      setLoading(false);
    }
  }, [ws, connected, agentId]);

  useEffect(() => {
    if (lastAgentRef.current === agentId) return;
    lastAgentRef.current = agentId;
    void load();
  }, [agentId, load]);

  const addShare = useCallback(
    async (input: CreateShareInput): Promise<boolean> => {
      try {
        await ws.call(Methods.AGENTS_SHARES_CREATE, {
          agentId,
          sharedWithUserId: input.userId ?? "",
          sharedWithTeamId: input.teamId ?? "",
          role: input.role,
        });
        toast.success(i18next.t("agents:shares.toast.added"));
        await load();
        return true;
      } catch (err) {
        toast.error(i18next.t("agents:shares.errors.addFailed"), userFriendlyError(err));
        return false;
      }
    },
    [ws, agentId, load],
  );

  const removeShare = useCallback(
    async (target: { userId?: string; teamId?: string }): Promise<boolean> => {
      try {
        await ws.call(Methods.AGENTS_SHARES_DELETE, {
          agentId,
          sharedWithUserId: target.userId ?? "",
          sharedWithTeamId: target.teamId ?? "",
        });
        toast.success(i18next.t("agents:shares.toast.removed"));
        await load();
        return true;
      } catch (err) {
        toast.error(i18next.t("agents:shares.errors.removeFailed"), userFriendlyError(err));
        return false;
      }
    },
    [ws, agentId, load],
  );

  return { shares, loading, refresh: load, addShare, removeShare };
}
