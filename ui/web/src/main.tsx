import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import "./i18n";
import App from "./App";
import "./index.css";

// Apply dark/light class BEFORE React mounts so the auth pages don't flash
// with the wrong theme on first paint. ThemeProvider re-applies on every
// state change after this; both paths read the same Zustand persist key.
(() => {
  try {
    const raw = localStorage.getItem("goclaw:ui");
    const stored = raw ? (JSON.parse(raw)?.state?.theme as string | undefined) : undefined;
    const resolved = stored ?? "system";
    const isDark =
      resolved === "dark" ||
      (resolved === "system" && window.matchMedia("(prefers-color-scheme: dark)").matches);
    document.documentElement.classList.add(isDark ? "dark" : "light");
  } catch {
    // localStorage unavailable (private mode, blocked) — fall back to system pref.
    if (window.matchMedia("(prefers-color-scheme: dark)").matches) {
      document.documentElement.classList.add("dark");
    } else {
      document.documentElement.classList.add("light");
    }
  }
})();

const LOADER_MIN_MS = 800;
const loaderStart = performance.now();

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <App />
  </StrictMode>,
);

const ric = window.requestIdleCallback ?? ((cb: () => void) => setTimeout(cb, 1));
ric(() => {
  const elapsed = performance.now() - loaderStart;
  const delay = Math.max(0, LOADER_MIN_MS - elapsed);
  setTimeout(() => {
    const loader = document.getElementById("app-loader");
    if (loader) {
      loader.classList.add("fade-out");
      setTimeout(() => loader.remove(), 300);
    }
  }, delay);
});
