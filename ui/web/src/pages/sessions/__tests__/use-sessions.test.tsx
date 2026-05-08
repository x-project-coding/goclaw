import { describe, it, expect, vi, beforeEach } from "vitest";
import { renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { createElement } from "react";

const mocks = vi.hoisted(() => ({
  callMock: vi.fn(),
  isConnected: true,
}));

vi.mock("@/hooks/use-ws", () => ({
  useWs: () => ({ call: mocks.callMock, isConnected: mocks.isConnected }),
}));

vi.mock("@/stores/use-auth-store", () => ({
  useAuthStore: <T,>(selector: (s: { connected: boolean }) => T) => selector({ connected: true }),
}));

vi.mock("@/stores/use-toast-store", () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}));

vi.mock("@/lib/error-utils", () => ({
  userFriendlyError: (e: unknown) => String(e),
}));

vi.mock("i18next", () => ({
  default: { t: (k: string) => k },
}));

const { callMock } = mocks;

import { useSessions } from "../hooks/use-sessions";

const wrapper = ({ children }: { children: ReactNode }) => {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return createElement(QueryClientProvider, { client: qc }, children);
};

describe("useSessions — projectId filter wiring", () => {
  beforeEach(() => {
    callMock.mockReset();
    callMock.mockResolvedValue({ sessions: [], total: 0 });
  });

  it("forwards projectId to sessions.list when set", async () => {
    renderHook(() => useSessions({ projectId: "proj-1", limit: 20, offset: 0 }), { wrapper });
    await waitFor(() => expect(callMock).toHaveBeenCalled());
    expect(callMock).toHaveBeenCalledWith("sessions.list", {
      agentId: undefined,
      projectId: "proj-1",
      limit: 20,
      offset: 0,
    });
  });

  it("omits projectId when unset (default behaviour)", async () => {
    renderHook(() => useSessions({ limit: 10, offset: 0 }), { wrapper });
    await waitFor(() => expect(callMock).toHaveBeenCalled());
    expect(callMock).toHaveBeenCalledWith("sessions.list", {
      agentId: undefined,
      projectId: undefined,
      limit: 10,
      offset: 0,
    });
  });

  it("treats empty-string projectId as omitted", async () => {
    renderHook(() => useSessions({ projectId: "", limit: 10, offset: 0 }), { wrapper });
    await waitFor(() => expect(callMock).toHaveBeenCalled());
    const args = callMock.mock.calls[0]?.[1] as { projectId: string | undefined };
    expect(args.projectId).toBeUndefined();
  });
});
