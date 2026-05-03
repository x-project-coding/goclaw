import { useState, useEffect, useCallback } from "react";
import { useHttp } from "./use-ws";

interface EmbeddingStatus {
  configured: boolean;
  provider?: string;
  provider_name?: string;
  model?: string;
}

export function useEmbeddingStatus() {
  const http = useHttp();
  const [status, setStatus] = useState<EmbeddingStatus | null>(null);
  const [loading, setLoading] = useState(true);

  const refresh = useCallback(async () => {
    try {
      const res = await http.get<EmbeddingStatus>("/v1/embedding/status");
      setStatus(res);
    } catch {
      setStatus({ configured: false });
    } finally {
      setLoading(false);
    }
  }, [http]);

  useEffect(() => { refresh(); }, [refresh]);

  return { status, loading, refresh };
}
