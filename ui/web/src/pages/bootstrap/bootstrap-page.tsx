import { useEffect, useState } from "react";
import { Navigate, useNavigate } from "react-router";
import { useTranslation } from "react-i18next";
import { LoginLayout } from "@/pages/login/login-layout";
import { useAuthStore } from "@/stores/use-auth-store";
import { ROUTES } from "@/lib/constants";
import { BootstrapForm } from "./bootstrap-form";

interface StatusResponse {
  bootstrapped: boolean;
}

export function BootstrapPage() {
  const { t } = useTranslation("auth");
  const navigate = useNavigate();
  const setTokens = useAuthStore((s) => s.setTokens);
  const [status, setStatus] = useState<"loading" | "needs-bootstrap" | "done">("loading");

  useEffect(() => {
    let cancelled = false;
    fetch("/v1/bootstrap/status")
      .then((res) => (res.ok ? (res.json() as Promise<StatusResponse>) : Promise.reject()))
      .then((body) => {
        if (cancelled) return;
        setStatus(body.bootstrapped ? "done" : "needs-bootstrap");
      })
      .catch(() => {
        // If status endpoint is unreachable, default to showing the form so the user
        // can still attempt bootstrap; backend will reject if state disagrees.
        if (!cancelled) setStatus("needs-bootstrap");
      });
    return () => {
      cancelled = true;
    };
  }, []);

  if (status === "done") {
    return <Navigate to={ROUTES.LOGIN} replace />;
  }

  return (
    <LoginLayout subtitle={t("bootstrap.subtitle")}>
      <h2 className="text-center text-lg font-semibold">{t("bootstrap.title")}</h2>
      {status === "loading" ? (
        <p className="text-center text-sm text-muted-foreground">{t("bootstrap.submitting")}</p>
      ) : (
        <BootstrapForm
          onSuccess={(accessToken, refreshToken, userId) => {
            setTokens(accessToken, refreshToken, userId);
            navigate(ROUTES.OVERVIEW, { replace: true });
          }}
        />
      )}
    </LoginLayout>
  );
}
