import { Navigate, useLocation } from "react-router";
import { useAuthStore } from "@/stores/use-auth-store";
import { ROUTES } from "@/lib/constants";
import { useFirstRunGate } from "@/pages/setup/hooks/use-first-run-gate";

// Routes that the first-run gate must NOT redirect away from. /setup itself
// is the destination; /profile + /logout let the user escape if the wizard
// somehow blocks them.
const SETUP_BYPASS_PATHS = new Set<string>([
  ROUTES.SETUP,
  ROUTES.PROFILE,
]);

export function RequireAuth({ children }: { children: React.ReactNode }) {
  const token = useAuthStore((s) => s.token);
  const userId = useAuthStore((s) => s.userId);
  const senderID = useAuthStore((s) => s.senderID);
  const location = useLocation();

  // Authenticated check first — gate hook depends on http auth working.
  const authenticated = (token || senderID) && userId;
  const { needsSetup } = useFirstRunGate(Boolean(authenticated));

  if (!authenticated) {
    return <Navigate to={ROUTES.LOGIN} state={{ from: location }} replace />;
  }

  if (needsSetup && !SETUP_BYPASS_PATHS.has(location.pathname)) {
    return <Navigate to={ROUTES.SETUP} replace />;
  }

  return <>{children}</>;
}
