import { useState } from "react";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { useTranslation } from "react-i18next";
import { AlertCircle, CheckCircle2 } from "lucide-react";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  profileUpdateSchema,
  passwordChangeSchema,
  type ProfileUpdateData,
  type PasswordChangeData,
} from "@/schemas/auth.schema";

interface ProfileFormProps {
  initialDisplayName: string;
  accessToken: string;
  onUpdated: () => Promise<void>;
  onPasswordChanged: () => Promise<void>;
}

interface ApiErrorBody {
  error?: string;
  message?: string;
}

export function ProfileForm({
  initialDisplayName,
  accessToken,
  onUpdated,
  onPasswordChanged,
}: ProfileFormProps) {
  const { t } = useTranslation("auth");

  // --- Display name form ---
  const [nameSaving, setNameSaving] = useState(false);
  const [nameSaved, setNameSaved] = useState(false);
  const [nameError, setNameError] = useState<string | null>(null);
  const nameForm = useForm<ProfileUpdateData>({
    resolver: zodResolver(profileUpdateSchema),
    defaultValues: { displayName: initialDisplayName },
  });

  const submitName = async (data: ProfileUpdateData) => {
    setNameSaving(true);
    setNameSaved(false);
    setNameError(null);
    try {
      const res = await fetch("/v1/users/me", {
        method: "PATCH",
        headers: {
          "Content-Type": "application/json",
          Authorization: `Bearer ${accessToken}`,
        },
        body: JSON.stringify({ display_name: data.displayName.trim() }),
      });
      if (!res.ok) {
        const body = (await res.json().catch(() => ({}))) as ApiErrorBody;
        setNameError(body.message ?? t("bootstrap.error.generic"));
        return;
      }
      setNameSaved(true);
      await onUpdated();
    } catch {
      setNameError(t("bootstrap.error.generic"));
    } finally {
      setNameSaving(false);
    }
  };

  // --- Password change form ---
  const [pwSaving, setPwSaving] = useState(false);
  const [pwError, setPwError] = useState<string | null>(null);
  const pwForm = useForm<PasswordChangeData>({
    resolver: zodResolver(passwordChangeSchema),
    defaultValues: { currentPassword: "", newPassword: "" },
  });

  const submitPassword = async (data: PasswordChangeData) => {
    setPwSaving(true);
    setPwError(null);
    try {
      const res = await fetch("/v1/auth/change-password", {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          Authorization: `Bearer ${accessToken}`,
        },
        body: JSON.stringify({
          current_password: data.currentPassword,
          new_password: data.newPassword,
        }),
      });
      if (res.status === 401) {
        const body = (await res.json().catch(() => ({}))) as ApiErrorBody;
        setPwError(body.message ?? t("login.error.invalidCredentials"));
        return;
      }
      if (!res.ok) {
        const body = (await res.json().catch(() => ({}))) as ApiErrorBody;
        if (body.error === "weak_password") {
          setPwError(t("bootstrap.error.weakPassword"));
        } else {
          setPwError(body.message ?? t("bootstrap.error.generic"));
        }
        return;
      }
      // On success, backend revoked all sessions — caller must redirect to /login.
      pwForm.reset();
      await onPasswordChanged();
    } catch {
      setPwError(t("bootstrap.error.generic"));
    } finally {
      setPwSaving(false);
    }
  };

  return (
    <div className="space-y-6">
      {/* Display name */}
      <form
        onSubmit={nameForm.handleSubmit(submitName)}
        className="space-y-4 rounded-lg border bg-card p-6 shadow-sm"
      >
        <div className="space-y-2">
          <Label htmlFor="displayName">{t("profile.displayName")}</Label>
          <Input
            id="displayName"
            type="text"
            autoComplete="name"
            {...nameForm.register("displayName")}
            className="text-base md:text-sm"
            disabled={nameSaving}
          />
          {nameForm.formState.errors.displayName && (
            <p className="text-xs text-destructive">
              {t("bootstrap.error.displayNameTooShort")}
            </p>
          )}
        </div>

        {nameError && (
          <ErrorBox>{nameError}</ErrorBox>
        )}
        {nameSaved && !nameError && (
          <SuccessBox>{t("profile.saved")}</SuccessBox>
        )}

        <button
          type="submit"
          disabled={nameSaving || !nameForm.formState.isDirty}
          className="inline-flex h-9 items-center justify-center rounded-md bg-primary px-4 py-2 text-sm font-medium text-primary-foreground shadow transition-colors hover:bg-primary/90 disabled:pointer-events-none disabled:opacity-50"
        >
          {nameSaving ? t("profile.saving") : t("profile.save")}
        </button>
      </form>

      {/* Change password */}
      <form
        onSubmit={pwForm.handleSubmit(submitPassword)}
        className="space-y-4 rounded-lg border bg-card p-6 shadow-sm"
        autoComplete="off"
      >
        <h2 className="text-base font-semibold">{t("profile.changePassword")}</h2>

        <div className="space-y-2">
          <Label htmlFor="currentPassword">{t("profile.currentPassword")}</Label>
          <Input
            id="currentPassword"
            type="password"
            autoComplete="current-password"
            {...pwForm.register("currentPassword")}
            className="text-base md:text-sm"
            disabled={pwSaving}
          />
        </div>

        <div className="space-y-2">
          <Label htmlFor="newPassword">{t("profile.newPassword")}</Label>
          <Input
            id="newPassword"
            type="password"
            autoComplete="new-password"
            {...pwForm.register("newPassword")}
            className="text-base md:text-sm"
            disabled={pwSaving}
          />
          {pwForm.formState.errors.newPassword ? (
            <p className="text-xs text-destructive">
              {t("bootstrap.error.weakPassword")}
            </p>
          ) : (
            <p className="text-xs text-muted-foreground">{t("bootstrap.passwordHint")}</p>
          )}
        </div>

        {pwError && <ErrorBox>{pwError}</ErrorBox>}

        <button
          type="submit"
          disabled={pwSaving}
          className="inline-flex h-9 items-center justify-center rounded-md bg-primary px-4 py-2 text-sm font-medium text-primary-foreground shadow transition-colors hover:bg-primary/90 disabled:pointer-events-none disabled:opacity-50"
        >
          {pwSaving ? t("profile.saving") : t("profile.changePassword")}
        </button>
      </form>
    </div>
  );
}

function ErrorBox({ children }: { children: React.ReactNode }) {
  return (
    <div className="flex items-start gap-2 rounded-md border border-destructive/50 bg-destructive/10 px-3 py-2 text-sm text-destructive">
      <AlertCircle className="mt-0.5 h-4 w-4 shrink-0" />
      <span>{children}</span>
    </div>
  );
}

function SuccessBox({ children }: { children: React.ReactNode }) {
  return (
    <div className="flex items-start gap-2 rounded-md border border-emerald-500/50 bg-emerald-500/10 px-3 py-2 text-sm text-emerald-700 dark:text-emerald-400">
      <CheckCircle2 className="mt-0.5 h-4 w-4 shrink-0" />
      <span>{children}</span>
    </div>
  );
}
