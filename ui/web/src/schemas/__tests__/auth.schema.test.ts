import { describe, it, expect } from "vitest";
import {
  passwordLoginSchema,
  bootstrapSchema,
  profileUpdateSchema,
  passwordChangeSchema,
} from "../auth.schema";

describe("passwordLoginSchema", () => {
  it("accepts valid email + non-empty password", () => {
    expect(passwordLoginSchema.safeParse({ email: "a@b.co", password: "x" }).success).toBe(true);
  });

  it("rejects bad email", () => {
    const r = passwordLoginSchema.safeParse({ email: "nope", password: "x" });
    expect(r.success).toBe(false);
  });

  it("rejects empty password", () => {
    const r = passwordLoginSchema.safeParse({ email: "a@b.co", password: "" });
    expect(r.success).toBe(false);
  });
});

describe("bootstrapSchema", () => {
  const valid = {
    email: "root@example.com",
    password: "Hunter12345!",
    displayName: "Root",
    bootstrapToken: "abc123",
  };

  it("accepts a fully-valid payload", () => {
    expect(bootstrapSchema.safeParse(valid).success).toBe(true);
  });

  it("rejects password under 12 chars", () => {
    const r = bootstrapSchema.safeParse({ ...valid, password: "Short1!" });
    expect(r.success).toBe(false);
  });

  it("rejects password without a digit", () => {
    const r = bootstrapSchema.safeParse({ ...valid, password: "NoDigitsHere!@" });
    expect(r.success).toBe(false);
  });

  it("rejects password without a symbol", () => {
    const r = bootstrapSchema.safeParse({ ...valid, password: "NoSymbol12345" });
    expect(r.success).toBe(false);
  });

  it("rejects password without a letter", () => {
    const r = bootstrapSchema.safeParse({ ...valid, password: "1234567890!@" });
    expect(r.success).toBe(false);
  });

  it("rejects displayName under 2 chars", () => {
    const r = bootstrapSchema.safeParse({ ...valid, displayName: "x" });
    expect(r.success).toBe(false);
  });

  it("rejects displayName over 64 chars", () => {
    const r = bootstrapSchema.safeParse({ ...valid, displayName: "x".repeat(65) });
    expect(r.success).toBe(false);
  });

  it("rejects empty bootstrap token", () => {
    const r = bootstrapSchema.safeParse({ ...valid, bootstrapToken: "" });
    expect(r.success).toBe(false);
  });

  it("rejects invalid email", () => {
    const r = bootstrapSchema.safeParse({ ...valid, email: "not-an-email" });
    expect(r.success).toBe(false);
  });
});

describe("profileUpdateSchema", () => {
  it("accepts valid display name", () => {
    expect(profileUpdateSchema.safeParse({ displayName: "Alice" }).success).toBe(true);
  });

  it("rejects single char name", () => {
    expect(profileUpdateSchema.safeParse({ displayName: "A" }).success).toBe(false);
  });
});

describe("passwordChangeSchema", () => {
  it("accepts current + valid new password", () => {
    const r = passwordChangeSchema.safeParse({
      currentPassword: "old",
      newPassword: "Hunter12345!",
    });
    expect(r.success).toBe(true);
  });

  it("rejects when new == current", () => {
    const r = passwordChangeSchema.safeParse({
      currentPassword: "Hunter12345!",
      newPassword: "Hunter12345!",
    });
    expect(r.success).toBe(false);
  });

  it("rejects weak new password", () => {
    const r = passwordChangeSchema.safeParse({
      currentPassword: "old",
      newPassword: "weak",
    });
    expect(r.success).toBe(false);
  });
});
