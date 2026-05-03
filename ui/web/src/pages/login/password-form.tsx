import { useState } from "react";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { useTranslation } from "react-i18next";
import { AlertCircle } from "lucide-react";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { passwordLoginSchema, type PasswordLoginData } from "@/schemas/auth.schema";

interface PasswordFormProps {
  onSubmit: (email: string, password: string) => Promise<void>;
}

export function PasswordForm({ onSubmit }: PasswordFormProps) {
  const { t } = useTranslation("auth");
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const {
    register,
    handleSubmit,
    formState: { errors },
  } = useForm<PasswordLoginData>({
    resolver: zodResolver(passwordLoginSchema),
    defaultValues: { email: "", password: "" },
  });

  const onValid = async (data: PasswordLoginData) => {
    setSubmitting(true);
    setError(null);
    try {
      await onSubmit(data.email.trim(), data.password);
    } catch (err) {
      const code = (err as Error)?.message ?? "";
      if (code === "invalid_credentials" || code === "http_401") {
        setError(t("login.error.invalidCredentials"));
      } else if (code === "rate_limit_exceeded" || code === "http_429") {
        setError(t("login.error.rateLimited"));
      } else {
        setError(t("login.error.generic"));
      }
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <form onSubmit={handleSubmit(onValid)} className="space-y-4">
      <div className="space-y-2">
        <Label htmlFor="email">{t("login.email")}</Label>
        <Input
          id="email"
          type="email"
          autoComplete="email"
          {...register("email")}
          placeholder={t("login.emailPlaceholder")}
          className="text-base md:text-sm"
          autoFocus
          disabled={submitting}
        />
        {errors.email && (
          <p className="text-xs text-destructive">{t("bootstrap.error.invalidEmail")}</p>
        )}
      </div>

      <div className="space-y-2">
        <Label htmlFor="password">{t("login.password")}</Label>
        <Input
          id="password"
          type="password"
          autoComplete="current-password"
          {...register("password")}
          className="text-base md:text-sm"
          disabled={submitting}
        />
        {errors.password && (
          <p className="text-xs text-destructive">{t("login.error.generic")}</p>
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
        {submitting ? t("login.submitting") : t("login.submit")}
      </button>
    </form>
  );
}
