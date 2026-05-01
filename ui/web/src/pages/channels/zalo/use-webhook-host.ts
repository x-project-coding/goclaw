import { useEffect, useState } from "react";

const STORAGE_KEY = "goclaw.zalo.webhook_host";

function defaultHost(): string {
  if (typeof window === "undefined") return "";
  return window.location.origin;
}

/**
 * Persist a per-browser override for the gateway host that operators paste
 * into Zalo's dev console. Falls back to window.location.origin when no
 * override is stored. Stored in localStorage so it survives reloads.
 */
export function useWebhookHost(): [string, (next: string) => void] {
  const [host, setHost] = useState<string>(() => {
    if (typeof window === "undefined") return "";
    return window.localStorage.getItem(STORAGE_KEY) ?? defaultHost();
  });

  useEffect(() => {
    if (typeof window === "undefined") return;
    const trimmed = host.trim();
    if (!trimmed || trimmed === defaultHost()) {
      window.localStorage.removeItem(STORAGE_KEY);
      return;
    }
    if (!isValidHttpURL(trimmed)) {
      // Don't persist garbage — onChange fires on every keystroke.
      return;
    }
    window.localStorage.setItem(STORAGE_KEY, trimmed);
  }, [host]);

  return [host, setHost];
}

function isValidHttpURL(value: string): boolean {
  try {
    const u = new URL(value);
    return u.protocol === "http:" || u.protocol === "https:";
  } catch {
    return false;
  }
}
