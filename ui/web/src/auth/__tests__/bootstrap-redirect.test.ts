import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { ApiError } from "@/api/errors";
import { isBootstrapRequired, handleBootstrapRedirect } from "../bootstrap-redirect";

describe("isBootstrapRequired", () => {
  it("true when ApiError.code = bootstrap_required", () => {
    expect(isBootstrapRequired(new ApiError("bootstrap_required", "init me"))).toBe(true);
    expect(isBootstrapRequired(new ApiError("BOOTSTRAP_REQUIRED", "init me"))).toBe(true);
  });

  it("true when flat backend body landed in message field", () => {
    // Backend returns { error: 'bootstrap_required', message: '...' }; http-client maps
    // the string into ApiError.message and leaves code = HTTP_ERROR.
    expect(isBootstrapRequired(new ApiError("HTTP_ERROR", "bootstrap_required"))).toBe(true);
  });

  it("false for unrelated errors", () => {
    expect(isBootstrapRequired(new ApiError("UNAUTHORIZED", "no token"))).toBe(false);
    expect(isBootstrapRequired(new Error("network"))).toBe(false);
    expect(isBootstrapRequired(null)).toBe(false);
    expect(isBootstrapRequired(undefined)).toBe(false);
  });
});

describe("handleBootstrapRedirect", () => {
  let originalLocation: Location;
  let replaceSpy: ReturnType<typeof vi.fn>;

  beforeEach(() => {
    originalLocation = window.location;
    replaceSpy = vi.fn();
    Object.defineProperty(window, "location", {
      configurable: true,
      value: { pathname: "/chat", replace: replaceSpy },
    });
  });

  afterEach(() => {
    Object.defineProperty(window, "location", { configurable: true, value: originalLocation });
  });

  it("redirects to /bootstrap on bootstrap_required error", () => {
    const handled = handleBootstrapRedirect(new ApiError("bootstrap_required", "init me"));
    expect(handled).toBe(true);
    expect(replaceSpy).toHaveBeenCalledWith("/bootstrap");
  });

  it("does not redirect for unrelated errors", () => {
    const handled = handleBootstrapRedirect(new ApiError("UNAUTHORIZED", "no token"));
    expect(handled).toBe(false);
    expect(replaceSpy).not.toHaveBeenCalled();
  });

  it("does not redirect when already on /bootstrap", () => {
    Object.defineProperty(window, "location", {
      configurable: true,
      value: { pathname: "/bootstrap", replace: replaceSpy },
    });
    const handled = handleBootstrapRedirect(new ApiError("bootstrap_required", "init me"));
    expect(handled).toBe(false);
    expect(replaceSpy).not.toHaveBeenCalled();
  });
});
