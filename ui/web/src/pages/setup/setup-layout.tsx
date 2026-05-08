import { Moon, Sun } from "lucide-react";
import { useTranslation } from "react-i18next";
import { useUiStore } from "@/stores/use-ui-store";

interface SetupLayoutProps {
  children: React.ReactNode;
}

// Branded shell matching LoginLayout (radial backdrop on --primary), but
// wider so the wizard cards have room. Used only for first-run setup —
// the rest of the app uses AppLayout with sidebar+topbar.
export function SetupLayout({ children }: SetupLayoutProps) {
  const { t } = useTranslation("setup");
  const { t: tTopbar } = useTranslation("topbar");
  const theme = useUiStore((s) => s.theme);
  const setTheme = useUiStore((s) => s.setTheme);
  const isDark =
    theme === "dark" ||
    (theme === "system" && typeof window !== "undefined" && window.matchMedia("(prefers-color-scheme: dark)").matches);

  return (
    <div className="relative flex min-h-dvh items-center justify-center overflow-hidden bg-background px-4 py-8 safe-top safe-bottom">
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
        title={tTopbar("toggleTheme")}
        aria-label={tTopbar("toggleTheme")}
      >
        {isDark ? <Sun className="h-4 w-4" /> : <Moon className="h-4 w-4" />}
      </button>

      <div className="w-full max-w-2xl space-y-6">
        <div className="text-center">
          <img src="/goclaw-icon.svg" alt="GoClaw" className="mx-auto mb-3 h-16 w-16" />
          <h1 className="text-3xl font-bold tracking-tight text-primary">{t("layout.title")}</h1>
          <p className="mt-2 text-sm text-muted-foreground">{t("layout.subtitle")}</p>
        </div>
        {children}
      </div>
    </div>
  );
}
