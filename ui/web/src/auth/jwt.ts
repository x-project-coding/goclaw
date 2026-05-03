// JWT decode helpers — payload-only, no signature verify (backend is source of truth).
// Used to preempt 401 by checking exp locally before sending requests.

export interface JwtPayload {
  sub?: string;
  exp?: number; // seconds since epoch
  iat?: number;
  [key: string]: unknown;
}

/** Decode JWT payload without signature verification. Returns null if malformed. */
export function decodeJwt(token: string): JwtPayload | null {
  if (!token) return null;
  const parts = token.split(".");
  if (parts.length !== 3) return null;
  const payloadPart = parts[1];
  if (!payloadPart) return null;
  try {
    const base64 = payloadPart.replace(/-/g, "+").replace(/_/g, "/");
    const padded = base64 + "=".repeat((4 - (base64.length % 4)) % 4);
    const json = atob(padded);
    return JSON.parse(json) as JwtPayload;
  } catch {
    return null;
  }
}

/** True if token is missing, malformed, or expired (with optional skew seconds). */
export function isExpired(token: string, skewSeconds = 0): boolean {
  const payload = decodeJwt(token);
  if (!payload || typeof payload.exp !== "number") return true;
  const nowSec = Math.floor(Date.now() / 1000);
  return payload.exp <= nowSec + skewSeconds;
}

/** Seconds remaining until expiry. Negative if expired, 0 if no exp claim. */
export function expiresInSeconds(token: string): number {
  const payload = decodeJwt(token);
  if (!payload || typeof payload.exp !== "number") return 0;
  const nowSec = Math.floor(Date.now() / 1000);
  return payload.exp - nowSec;
}

/** True if token expires within the given threshold (default 60s) — useful for proactive refresh. */
export function isExpiringSoon(token: string, thresholdSeconds = 60): boolean {
  return isExpired(token, thresholdSeconds);
}
