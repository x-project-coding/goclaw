import { Navigate } from "react-router";
import { useAuthStore } from "@/stores/use-auth-store";
import { ROUTES } from "@/lib/constants";

// v4 vocabulary: root > admin > member > viewer. v3 aliases (owner/operator)
// are normalised so persisted state and any lingering v3 issuance still resolve.
const ROLE_LEVELS: Record<string, number> = {
  root: 4,
  owner: 4,
  admin: 3,
  member: 2,
  operator: 2,
  viewer: 1,
};

/** Check if role meets minimum level. */
function hasMinRole(role: string, minRole: string): boolean {
  return (ROLE_LEVELS[role] ?? 0) >= (ROLE_LEVELS[minRole] ?? 0);
}

/** Renders children only if user has admin role or higher. Redirects to overview otherwise. */
export function RequireAdmin({ children }: { children: React.ReactNode }) {
  const role = useAuthStore((s) => s.role);
  if (!hasMinRole(role, "admin")) {
    return <Navigate to={ROUTES.OVERVIEW} replace />;
  }
  return <>{children}</>;
}

/** Renders children only if user has admin or operator role or higher. */
export function RequireOperator({ children }: { children: React.ReactNode }) {
  const role = useAuthStore((s) => s.role);
  if (!hasMinRole(role, "operator")) {
    return <Navigate to={ROUTES.OVERVIEW} replace />;
  }
  return <>{children}</>;
}

/** Renders children only if user has owner role. Single-user system: equivalent to admin. */
export function RequireOwner({ children }: { children: React.ReactNode }) {
  const role = useAuthStore((s) => s.role);
  if (!hasMinRole(role, "owner")) {
    return <Navigate to={ROUTES.OVERVIEW} replace />;
  }
  return <>{children}</>;
}

