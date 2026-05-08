import { describe, it, expect, vi, beforeEach } from "vitest";
import { renderHook, act, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { createElement } from "react";

const mocks = vi.hoisted(() => ({
  callMock: vi.fn(),
  successMock: vi.fn(),
  errorMock: vi.fn(),
}));

vi.mock("@/hooks/use-ws", () => ({
  useWs: () => ({ call: mocks.callMock }),
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

import { useSetDefaultProject } from "../hooks/use-set-default-project";

const wrapper = ({ children }: { children: ReactNode }) => {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return createElement(QueryClientProvider, { client: qc }, children);
};

describe("useSetDefaultProject", () => {
  beforeEach(() => {
    callMock.mockReset();
    successMock.mockReset();
    errorMock.mockReset();
  });

  it("calls channels.contacts.set_default_project with camelCase params", async () => {
    callMock.mockResolvedValueOnce({ ok: true, channelContactId: "c1", projectId: "p1" });
    const { result } = renderHook(() => useSetDefaultProject(), { wrapper });
    await act(async () => {
      await result.current.setDefaultProject("c1", "p1");
    });
    expect(callMock).toHaveBeenCalledWith("channels.contacts.set_default_project", {
      channelContactId: "c1",
      projectId: "p1",
    });
    await waitFor(() => expect(successMock).toHaveBeenCalled());
  });

  it("clears binding by passing projectId=null", async () => {
    callMock.mockResolvedValueOnce({ ok: true, channelContactId: "c1", projectId: "" });
    const { result } = renderHook(() => useSetDefaultProject(), { wrapper });
    await act(async () => {
      await result.current.setDefaultProject("c1", null);
    });
    expect(callMock).toHaveBeenCalledWith("channels.contacts.set_default_project", {
      channelContactId: "c1",
      projectId: null,
    });
  });

  it("surfaces a toast on error", async () => {
    callMock.mockRejectedValueOnce(new Error("forbidden"));
    const { result } = renderHook(() => useSetDefaultProject(), { wrapper });
    await act(async () => {
      await expect(result.current.setDefaultProject("c1", "p1")).rejects.toThrow("forbidden");
    });
    await waitFor(() => expect(errorMock).toHaveBeenCalled());
  });
});
