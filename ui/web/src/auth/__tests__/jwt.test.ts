import { describe, it, expect } from "vitest";
import { decodeJwt, isExpired, expiresInSeconds, isExpiringSoon } from "../jwt";

// Helper: build a JWT with the given payload (no real signature; we don't verify it).
function makeJwt(payload: Record<string, unknown>): string {
  const header = btoa(JSON.stringify({ alg: "HS256", typ: "JWT" })).replace(/=+$/, "");
  const body = btoa(JSON.stringify(payload)).replace(/=+$/, "");
  return `${header}.${body}.signature`;
}

describe("decodeJwt", () => {
  it("decodes valid payload", () => {
    const tok = makeJwt({ sub: "user-1", exp: 1700000000 });
    const out = decodeJwt(tok);
    expect(out).toEqual({ sub: "user-1", exp: 1700000000 });
  });

  it("returns null for empty / malformed token", () => {
    expect(decodeJwt("")).toBeNull();
    expect(decodeJwt("not-a-jwt")).toBeNull();
    expect(decodeJwt("a.b")).toBeNull();
    expect(decodeJwt("a.!!!.c")).toBeNull();
  });

  it("handles URL-safe base64 (- and _)", () => {
    const payload = { sub: "ok" };
    const json = JSON.stringify(payload);
    const std = btoa(json).replace(/=+$/, "");
    const urlSafe = std.replace(/\+/g, "-").replace(/\//g, "_");
    const tok = `header.${urlSafe}.sig`;
    expect(decodeJwt(tok)).toEqual(payload);
  });
});

describe("isExpired", () => {
  it("true for missing/malformed/no-exp", () => {
    expect(isExpired("")).toBe(true);
    expect(isExpired("garbage")).toBe(true);
    expect(isExpired(makeJwt({ sub: "x" }))).toBe(true);
  });

  it("true for past exp", () => {
    const past = Math.floor(Date.now() / 1000) - 100;
    expect(isExpired(makeJwt({ exp: past }))).toBe(true);
  });

  it("false for future exp", () => {
    const future = Math.floor(Date.now() / 1000) + 3600;
    expect(isExpired(makeJwt({ exp: future }))).toBe(false);
  });

  it("respects skew seconds", () => {
    // exp is 30s in the future; with 60s skew it counts as expired.
    const soon = Math.floor(Date.now() / 1000) + 30;
    expect(isExpired(makeJwt({ exp: soon }), 0)).toBe(false);
    expect(isExpired(makeJwt({ exp: soon }), 60)).toBe(true);
  });
});

describe("expiresInSeconds", () => {
  it("returns positive for future exp", () => {
    const t = Math.floor(Date.now() / 1000) + 3600;
    expect(expiresInSeconds(makeJwt({ exp: t }))).toBeGreaterThan(3500);
  });

  it("returns 0 when no exp claim", () => {
    expect(expiresInSeconds(makeJwt({ sub: "x" }))).toBe(0);
  });

  it("returns negative for expired", () => {
    const t = Math.floor(Date.now() / 1000) - 100;
    expect(expiresInSeconds(makeJwt({ exp: t }))).toBeLessThan(0);
  });
});

describe("isExpiringSoon", () => {
  it("true within default 60s window", () => {
    const t = Math.floor(Date.now() / 1000) + 30;
    expect(isExpiringSoon(makeJwt({ exp: t }))).toBe(true);
  });

  it("false when far in future", () => {
    const t = Math.floor(Date.now() / 1000) + 3600;
    expect(isExpiringSoon(makeJwt({ exp: t }))).toBe(false);
  });
});
