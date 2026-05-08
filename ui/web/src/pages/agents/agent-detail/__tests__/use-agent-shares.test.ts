import { describe, it, expect, vi, beforeEach } from "vitest";
import { renderHook, act, waitFor } from "@testing-library/react";

const mocks = vi.hoisted(() => ({
  callMock: vi.fn(),
  successMock: vi.fn(),
  errorMock: vi.fn(),
}));

vi.mock("@/hooks/use-ws", () => ({
  useWs: () => ({ call: mocks.callMock }),
}));

vi.mock("@/stores/use-auth-store", () => ({
  useAuthStore: <T,>(selector: (s: { connected: boolean }) => T) => selector({ connected: true }),
}));

vi.mock("@/stores/use-toast-store", () => ({
  toast: { success: mocks.successMock, error: mocks.errorMock },
}));

vi.mock("@/lib/error-utils", () => ({
  userFriendlyError: (e: unknown) => String(e),
}));

vi.mock("i18next", () => ({
  default: { t: (k: string) => k },
}));

const { callMock, successMock, errorMock } = mocks;

import { useAgentShares } from "../use-agent-shares";

const AGENT_ID = "00000000-0000-0000-0000-000000000aaa";
const sampleShare = {
  id: "share-1",
  agent_id: AGENT_ID,
  shared_with_user_id: "user-1",
  shared_with_team_id: null,
  role: "viewer" as const,
  created_by: "owner",
  created_at: "2026-05-08T00:00:00Z",
  updated_at: "2026-05-08T00:00:00Z",
};

describe("useAgentShares", () => {
  beforeEach(() => {
    callMock.mockReset();
    successMock.mockReset();
    errorMock.mockReset();
  });

  it("loads shares on first render via agents.shares.list", async () => {
    callMock.mockResolvedValueOnce({ shares: [sampleShare] });
    const { result } = renderHook(() => useAgentShares(AGENT_ID));
    await waitFor(() => expect(result.current.shares).toEqual([sampleShare]));
    expect(callMock).toHaveBeenCalledWith("agents.shares.list", { agentId: AGENT_ID });
  });

  it("addShare sends camelCase params with empty string for unused target", async () => {
    callMock.mockResolvedValueOnce({ shares: [] }); // initial load
    callMock.mockResolvedValueOnce({ ok: true }); // create
    callMock.mockResolvedValueOnce({ shares: [sampleShare] }); // refresh

    const { result } = renderHook(() => useAgentShares(AGENT_ID));
    await waitFor(() => expect(callMock).toHaveBeenCalledTimes(1));

    await act(async () => {
      const ok = await result.current.addShare({ userId: "user-1", role: "viewer" });
      expect(ok).toBe(true);
    });

    const createCall = callMock.mock.calls.find(([m]) => m === "agents.shares.create");
    expect(createCall?.[1]).toEqual({
      agentId: AGENT_ID,
      sharedWithUserId: "user-1",
      sharedWithTeamId: "",
      role: "viewer",
    });
    expect(successMock).toHaveBeenCalled();
  });

  it("removeShare routes by team when teamId provided", async () => {
    callMock.mockResolvedValueOnce({ shares: [] });
    callMock.mockResolvedValueOnce({ ok: true });
    callMock.mockResolvedValueOnce({ shares: [] });

    const { result } = renderHook(() => useAgentShares(AGENT_ID));
    await waitFor(() => expect(callMock).toHaveBeenCalledTimes(1));

    await act(async () => {
      await result.current.removeShare({ teamId: "team-1" });
    });

    const deleteCall = callMock.mock.calls.find(([m]) => m === "agents.shares.delete");
    expect(deleteCall?.[1]).toEqual({
      agentId: AGENT_ID,
      sharedWithUserId: "",
      sharedWithTeamId: "team-1",
    });
  });

  it("surfaces an error toast when addShare fails", async () => {
    callMock.mockResolvedValueOnce({ shares: [] });
    callMock.mockRejectedValueOnce(new Error("forbidden"));

    const { result } = renderHook(() => useAgentShares(AGENT_ID));
    await waitFor(() => expect(callMock).toHaveBeenCalledTimes(1));

    await act(async () => {
      const ok = await result.current.addShare({ userId: "user-1", role: "viewer" });
      expect(ok).toBe(false);
    });
    expect(errorMock).toHaveBeenCalled();
  });
});
