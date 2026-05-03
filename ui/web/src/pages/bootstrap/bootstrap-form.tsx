import { useState } from "react";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { useTranslation } from "react-i18next";
import { AlertCircle } from "lucide-react";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { bootstrapSchema, type BootstrapData } from "@/schemas/auth.schema";

interface BootstrapFormProps {
  onSuccess: (accessToken: string, refreshToken: string, userId: string) => void;
}

interface BootstrapResponse {
  access_token: string;
  refresh_token: string;
  user_id: string;
  role: string;
}

export function BootstrapForm({ onSuccess }: BootstrapFormProps) {
  const { t } = useTranslation("auth");
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const {
    register,
    handleSubmit,
    formState: { errors },
  } = useForm<BootstrapData>({
    resolver: zodResolver(bootstrapSchema),
    defaultValues: { email: "", password: "", displayName: "", bootstrapToken: "" },
  });

  // Map zod schema codes / backend error codes to translated messages.
  function mapZodError(code: string | undefined): string {
    if (!code) return "";
    switch (code) {
      case "min12":
      case "needsLetter":
      case "needsDigit":
      case "needsSymbol":
        return t("bootstrap.error.weakPassword");
      case "invalidEmail":
        return t("bootstrap.error.invalidEmail");
      case "displayNameTooShort":
        return t("bootstrap.error.displayNameTooShort");
      case "displayNameTooLong":
        return t("bootstrap.error.displayNameTooLong");
      case "required":
        return t("bootstrap.error.generic");
      default:
        return code;
    }
  }

  const onValid = async (data: BootstrapData) => {
    setSubmitting(true);
    setError(null);
    try {
      const res = await fetch("/v1/bootstrap/init", {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          "X-Bootstrap-Token": data.bootstrapToken.trim(),
        },
        body: JSON.stringify({
          email: data.email.trim(),
          password: data.password,
          display_name: data.displayName.trim(),
        }),
      });
      if (res.status === 403) {
        setError(t("bootstrap.error.invalidToken"));
        return;
      }
      if (res.status === 409) {
        setError(t("bootstrap.error.alreadyBootstrapped"));
        return;
      }
      if (!res.ok) {
        const body = await res.json().catch(() => ({}));
        const code = (body as { error?: string }).error;
        if (code === "weak_password") setError(t("bootstrap.error.weakPassword"));
        else if (code === "invalid_email") setError(t("bootstrap.error.invalidEmail"));
        else setError(t("bootstrap.error.generic"));
        return;
      }
      const body = (await res.json()) as BootstrapResponse;
      onSuccess(body.access_token, body.refresh_token, body.user_id);
    } catch {
      setError(t("bootstrap.error.generic"));
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <form onSubmit={handleSubmit(onValid)} className="space-y-4" autoComplete="off">
      <div className="space-y-2">
        <Label htmlFor="email">{t("bootstrap.email")}</Label>
        <Input
          id="email"
          type="email"
          autoComplete="email"
          {...register("email")}
          placeholder={t("bootstrap.emailPlaceholder")}
          className="text-base md:text-sm"
          autoFocus
          disabled={submitting}
        />
        {errors.email && (
          <p className="text-xs text-destructive">{mapZodError(errors.email.message)}</p>
        )}
      </div>

      <div className="space-y-2">
        <Label htmlFor="displayName">{t("bootstrap.displayName")}</Label>
        <Input
          id="displayName"
          type="text"
          autoComplete="name"
          {...register("displayName")}
          className="text-base md:text-sm"
          disabled={submitting}
        />
        {errors.displayName && (
          <p className="text-xs text-destructive">{mapZodError(errors.displayName.message)}</p>
        )}
      </div>

      <div className="space-y-2">
        <Label htmlFor="password">{t("bootstrap.password")}</Label>
        <Input
          id="password"
          type="password"
          autoComplete="new-password"
          {...register("password")}
          className="text-base md:text-sm"
          disabled={submitting}
        />
        {errors.password ? (
          <p className="text-xs text-destructive">{mapZodError(errors.password.message)}</p>
        ) : (
          <p className="text-xs text-muted-foreground">{t("bootstrap.passwordHint")}</p>
        )}
      </div>

      <div className="space-y-2">
        <Label htmlFor="bootstrapToken">{t("bootstrap.bootstrapToken")}</Label>
        <Input
          id="bootstrapToken"
          type="text"
          autoComplete="off"
          {...register("bootstrapToken")}
          className="text-base md:text-sm font-mono"
          disabled={submitting}
        />
        {errors.bootstrapToken ? (
          <p className="text-xs text-destructive">{mapZodError(errors.bootstrapToken.message)}</p>
        ) : (
          <p className="text-xs text-muted-foreground">{t("bootstrap.bootstrapTokenHint")}</p>
        )}
      </div>

      {error && (
        <div className="flex items-start gap-2 rounded-md border border-destructive/50 bg-destructive/10 px-3 py-2 text-sm text-destructive">
          <AlertCircle className="mt-0.5 h-4 w-4 shrink-0" />
          <span>{error}</span>
        </div>
      )}

      <button
        type="submit"
        disabled={submitting}
        className="inline-flex h-9 w-full items-center justify-center rounded-md bg-primary px-4 py-2 text-sm font-medium text-primary-foreground shadow transition-colors hover:bg-primary/90 disabled:pointer-events-none disabled:opacity-50"
      >
        {submitting ? t("bootstrap.submitting") : t("bootstrap.submit")}
      </button>
    </form>
  );
}
