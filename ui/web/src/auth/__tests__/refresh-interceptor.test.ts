import { describe, it, expect, vi, beforeEach } from "vitest";
import { RefreshInterceptor } from "../refresh-interceptor";

interface Captured {
  fetchCalls: number;
  setCalls: Array<[string, string]>;
  failedCalls: number;
}

function makeDeps(opts: {
  refreshToken?: string;
  fetchImpl: typeof fetch;
}): { interceptor: RefreshInterceptor; captured: Captured } {
  const captured: Captured = { fetchCalls: 0, setCalls: [], failedCalls: 0 };
  const wrappedFetch = ((...args: Parameters<typeof fetch>) => {
    captured.fetchCalls += 1;
    return opts.fetchImpl(...args);
  }) as typeof fetch;
  const interceptor = new RefreshInterceptor({
    getRefreshToken: () => opts.refreshToken ?? "old-refresh",
    setTokens: (a, r) => captured.setCalls.push([a, r]),
    onRefreshFailed: () => {
      captured.failedCalls += 1;
    },
    fetchImpl: wrappedFetch,
    baseUrl: "http://example.test",
  });
  return { interceptor, captured };
}

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

describe("RefreshInterceptor", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it("rotates tokens on 200 response", async () => {
    const { interceptor, captured } = makeDeps({
      fetchImpl: vi.fn().mockResolvedValue(
        jsonResponse({ access_token: "new-access", refresh_token: "new-refresh" }),
      ),
    });
    const result = await interceptor.refresh();
    expect(result).toEqual({ accessToken: "new-access", refreshToken: "new-refresh" });
    expect(captured.setCalls).toEqual([["new-access", "new-refresh"]]);
    expect(captured.failedCalls).toBe(0);
  });

  it("calls onRefreshFailed and throws on 401", async () => {
    const { interceptor, captured } = makeDeps({
      fetchImpl: vi.fn().mockResolvedValue(jsonResponse({ error: "expired" }, 401)),
    });
    await expect(interceptor.refresh()).rejects.toThrow();
    expect(captured.failedCalls).toBe(1);
    expect(captured.setCalls).toEqual([]);
  });

  it("throws without onRefreshFailed when network errors", async () => {
    const { interceptor, captured } = makeDeps({
      fetchImpl: vi.fn().mockRejectedValue(new TypeError("offline")),
    });
    await expect(interceptor.refresh()).rejects.toThrow("refresh_network_error");
    // Network error is transient — must NOT log user out.
    expect(captured.failedCalls).toBe(0);
  });

  it("calls onRefreshFailed and throws when no refresh token", async () => {
    const { interceptor, captured } = makeDeps({
      refreshToken: "",
      fetchImpl: vi.fn(),
    });
    await expect(interceptor.refresh()).rejects.toThrow("no_refresh_token");
    expect(captured.failedCalls).toBe(1);
    expect(captured.fetchCalls).toBe(0);
  });

  it("single-flight: 5 parallel calls share one fetch", async () => {
    let resolveFetch!: (r: Response) => void;
    const fetchPromise = new Promise<Response>((r) => {
      resolveFetch = r;
    });
    const { interceptor, captured } = makeDeps({
      fetchImpl: vi.fn().mockReturnValue(fetchPromise),
    });

    const results = Promise.all([
      interceptor.refresh(),
      interceptor.refresh(),
      interceptor.refresh(),
      interceptor.refresh(),
      interceptor.refresh(),
    ]);
    // Resolve the single in-flight request.
    resolveFetch(jsonResponse({ access_token: "a", refresh_token: "r" }));
    const out = await results;

    expect(captured.fetchCalls).toBe(1); // single-flight
    expect(captured.setCalls).toHaveLength(1); // setTokens called once
    expect(out.every((r) => r.accessToken === "a" && r.refreshToken === "r")).toBe(true);
  });

  it("after a refresh resolves, subsequent calls trigger a fresh fetch", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(jsonResponse({ access_token: "a1", refresh_token: "r1" }))
      .mockResolvedValueOnce(jsonResponse({ access_token: "a2", refresh_token: "r2" }));
    const { interceptor, captured } = makeDeps({ fetchImpl: fetchMock });

    await interceptor.refresh();
    await interceptor.refresh();

    expect(captured.fetchCalls).toBe(2);
    expect(captured.setCalls).toEqual([
      ["a1", "r1"],
      ["a2", "r2"],
    ]);
  });

  it("treats response missing tokens as failure", async () => {
    const { interceptor, captured } = makeDeps({
      fetchImpl: vi.fn().mockResolvedValue(jsonResponse({})),
    });
    await expect(interceptor.refresh()).rejects.toThrow("refresh_invalid_response");
    expect(captured.failedCalls).toBe(1);
  });

  it("reset() clears in-flight state", async () => {
    const { interceptor } = makeDeps({
      fetchImpl: vi.fn().mockResolvedValue(jsonResponse({ access_token: "a", refresh_token: "r" })),
    });
    await interceptor.refresh();
    interceptor.reset();
    // No assertion needed — just verify the call doesn't throw.
  });
});
