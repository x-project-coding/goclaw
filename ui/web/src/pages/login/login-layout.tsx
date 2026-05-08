import { Moon, Sun } from "lucide-react";
import { useTranslation } from "react-i18next";
import { useUiStore } from "@/stores/use-ui-store";

interface LoginLayoutProps {
  children: React.ReactNode;
  subtitle?: string;
}

export function LoginLayout({ children, subtitle }: LoginLayoutProps) {
  const { t } = useTranslation("topbar");
  const theme = useUiStore((s) => s.theme);
  const setTheme = useUiStore((s) => s.setTheme);
  // Resolve effective dark for the icon. Recompute on every render so the
  // toggle stays in sync with the store (matches the topbar toggle pattern).
  const isDark =
    theme === "dark" ||
    (theme === "system" && typeof window !== "undefined" && window.matchMedia("(prefers-color-scheme: dark)").matches);

  return (
    <div className="relative flex min-h-dvh items-center justify-center overflow-hidden bg-background px-4 safe-top safe-bottom">
      {/* Branded backdrop: subtle radial wash anchored on --primary. The
          gradient is layered with bg-background so dark/light parity falls
          out naturally without hard-coded colors. */}
      <div
        aria-hidden
        className="pointer-events-none absolute inset-0 -z-10 bg-[radial-gradient(ellipse_at_top,_color-mix(in_oklch,var(--primary)_18%,transparent),_transparent_55%)]"
      />
      <div
        aria-hidden
        className="pointer-events-none absolute inset-x-0 bottom-0 -z-10 h-1/2 bg-[radial-gradient(ellipse_at_bottom,_color-mix(in_oklch,var(--primary)_10%,transparent),_transparent_60%)]"
      />

      <button
        type="button"
        onClick={() => setTheme(isDark ? "light" : "dark")}
        className="absolute top-4 right-4 cursor-pointer rounded-md p-2 text-muted-foreground hover:bg-accent hover:text-accent-foreground"
        title={t("toggleTheme")}
        aria-label={t("toggleTheme")}
      >
        {isDark ? <Sun className="h-4 w-4" /> : <Moon className="h-4 w-4" />}
      </button>

      <div className="w-full max-w-sm space-y-6 rounded-xl border border-border/60 bg-card/95 p-6 shadow-lg backdrop-blur-sm sm:p-8">
        <div className="text-center">
          <img src="/goclaw-icon.svg" alt="GoClaw" className="mx-auto mb-3 h-20 w-20" />
          <h1 className="text-3xl font-bold tracking-tight text-primary">GoClaw</h1>
          {subtitle && (
            <p className="mt-2 text-sm text-muted-foreground">{subtitle}</p>
          )}
        </div>
        {children}
      </div>
    </div>
  );
}
