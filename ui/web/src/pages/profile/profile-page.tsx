import { useState } from "react";
import { useNavigate } from "react-router";
import { useTranslation } from "react-i18next";
import { LogOut } from "lucide-react";
import { useAuth } from "@/auth/auth-context";
import { ROUTES } from "@/lib/constants";
import { ProfileForm } from "./profile-form";

export function ProfilePage() {
  const { t } = useTranslation("auth");
  const { user, accessToken, logout, loading, reloadUser } = useAuth();
  const navigate = useNavigate();
  const [signingOut, setSigningOut] = useState(false);

  if (loading) {
    return (
      <div className="flex h-full items-center justify-center">
        <p className="text-sm text-muted-foreground">{t("login.submitting")}</p>
      </div>
    );
  }

  if (!user) {
    return (
      <div className="flex h-full items-center justify-center">
        <p className="text-sm text-muted-foreground">{t("session.expired")}</p>
      </div>
    );
  }

  return (
    <div className="mx-auto max-w-2xl space-y-6 p-6">
      <header>
        <h1 className="text-2xl font-bold tracking-tight">{t("profile.title")}</h1>
      </header>

      <div className="space-y-4 rounded-lg border bg-card p-6 shadow-sm">
        <Field label={t("profile.email")} value={user.email} />
        <Field label="Role" value={user.role} mono />
        <Field label="Status" value={user.status} mono />
      </div>

      <ProfileForm
        initialDisplayName={user.displayName ?? ""}
        accessToken={accessToken}
        onUpdated={reloadUser}
        onPasswordChanged={async () => {
          // Backend revoked all sessions on password change — force re-login.
          await logout();
          navigate(ROUTES.LOGIN, { replace: true });
        }}
      />

      <button
        type="button"
        disabled={signingOut}
        onClick={async () => {
          setSigningOut(true);
          await logout();
          navigate(ROUTES.LOGIN, { replace: true });
        }}
        className="inline-flex h-9 items-center justify-center gap-2 rounded-md border border-destructive/50 bg-destructive/10 px-4 py-2 text-sm font-medium text-destructive transition-colors hover:bg-destructive/20 disabled:pointer-events-none disabled:opacity-50"
      >
        <LogOut className="h-4 w-4" />
        {t("profile.logout")}
      </button>
    </div>
  );
}

function Field({ label, value, mono }: { label: string; value: string; mono?: boolean }) {
  return (
    <div className="grid grid-cols-3 items-center gap-4">
      <span className="text-sm font-medium text-muted-foreground">{label}</span>
      <span className={`col-span-2 text-sm ${mono ? "font-mono" : ""}`}>{value}</span>
    </div>
  );
}
