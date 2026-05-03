import { useEffect, useState } from "react";
import { Navigate, useNavigate, useLocation } from "react-router";
import { useTranslation } from "react-i18next";
import { useAuth } from "@/auth/auth-context";
import { ROUTES } from "@/lib/constants";
import { LoginLayout } from "./login-layout";
import { PasswordForm } from "./password-form";

interface BootstrapStatus {
  bootstrapped: boolean;
}

export function LoginPage() {
  const { t } = useTranslation("auth");
  const { login, isAuthenticated } = useAuth();
  const navigate = useNavigate();
  const location = useLocation();
  const [needsBootstrap, setNeedsBootstrap] = useState(false);

  // Probe bootstrap status — if gateway is uninitialized, redirect to /bootstrap so the
  // operator can create the first root user without manually navigating.
  useEffect(() => {
    let cancelled = false;
    fetch("/v1/bootstrap/status")
      .then((res) => (res.ok ? (res.json() as Promise<BootstrapStatus>) : null))
      .then((body) => {
        if (cancelled || !body) return;
        if (!body.bootstrapped) setNeedsBootstrap(true);
      })
      .catch(() => undefined);
    return () => {
      cancelled = true;
    };
  }, []);

  if (needsBootstrap) {
    return <Navigate to={ROUTES.BOOTSTRAP} replace />;
  }

  const from =
    (location.state as { from?: { pathname: string } })?.from?.pathname ?? ROUTES.OVERVIEW;

  if (isAuthenticated) {
    return <Navigate to={from} replace />;
  }

  return (
    <LoginLayout subtitle={t("login.subtitle")}>
      <h2 className="text-center text-lg font-semibold">{t("login.title")}</h2>
      <PasswordForm
        onSubmit={async (email, password) => {
          await login(email, password);
          navigate(from, { replace: true });
        }}
      />
    </LoginLayout>
  );
}
