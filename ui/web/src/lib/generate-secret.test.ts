import { describe, expect, it } from "vitest";
import { generateSecret } from "./generate-secret";

describe("generateSecret", () => {
  it("returns a URL-safe base64 string of expected length for 32 bytes", () => {
    const s = generateSecret();
    expect(s.length).toBeGreaterThanOrEqual(40);
    expect(s).toMatch(/^[A-Za-z0-9_-]+$/);
  });

  it("produces distinct values across calls", () => {
    expect(generateSecret()).not.toBe(generateSecret());
  });

  it("respects a custom byte length", () => {
    const s = generateSecret(16);
    expect(s.length).toBeGreaterThanOrEqual(20);
    expect(s).toMatch(/^[A-Za-z0-9_-]+$/);
  });
});
