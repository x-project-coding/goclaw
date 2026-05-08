import { useQuery } from "@tanstack/react-query";
import { useHttp } from "@/hooks/use-ws";
import { queryKeys } from "@/lib/query-keys";
import type { ProviderData } from "@/types/provider";
import type { AgentData } from "@/types/agent";

interface FirstRunGate {
  needsSetup: boolean;
  loading: boolean;
}

// Decides whether the authenticated user should be force-redirected to
// /setup. Triggers when BOTH conditions hold:
//   - 0 enabled providers
//   - 0 enabled agents
// Once either resource has at least one enabled record, the gate disengages
// permanently for that DB (no schema flag — KISS).
export function useFirstRunGate(enabled = true): FirstRunGate {
  const http = useHttp();

  const providers = useQuery({
    queryKey: queryKeys.providers.all,
    enabled,
    queryFn: async () => {
      const res = await http.get<{ providers: ProviderData[] }>("/v1/providers");
      return res.providers ?? [];
    },
    staleTime: 60_000,
  });

  const agents = useQuery({
    queryKey: queryKeys.agents.all,
    enabled,
    queryFn: async () => {
      const res = await http.get<{ agents: AgentData[] }>("/v1/agents");
      return res.agents ?? [];
    },
    staleTime: 60_000,
  });

  if (!enabled) {
    return { needsSetup: false, loading: false };
  }

  const loading = providers.isLoading || agents.isLoading;
  if (loading) return { needsSetup: false, loading: true };

  const hasProvider = (providers.data ?? []).some((p) => p.enabled);
  const hasAgent = (agents.data ?? []).length > 0;
  return { needsSetup: !hasProvider && !hasAgent, loading: false };
}
