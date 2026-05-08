import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { Loader2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { useHttp } from "@/hooks/use-ws";
import type { ModelInfo, ProviderData, ProviderModelsResponse } from "@/types/provider";

interface StepModelProps {
  provider: ProviderData;
  onBack: () => void;
  onComplete: (modelId: string) => void;
}

export function StepModel({ provider, onBack, onComplete }: StepModelProps) {
  const { t } = useTranslation("setup");
  const http = useHttp();
  const [models, setModels] = useState<ModelInfo[]>([]);
  const [selected, setSelected] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setError(null);
    http
      .get<ProviderModelsResponse>(`/v1/providers/${provider.id}/models`)
      .then((res) => {
        if (cancelled) return;
        const list = res.models ?? [];
        setModels(list);
        // Auto-select first model so the user can just click Next.
        const first = list[0];
        if (first) setSelected(first.id);
      })
      .catch((err) => {
        if (cancelled) return;
        setError(err instanceof Error ? err.message : t("model.error.fetch"));
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [http, provider.id, t]);

  const canSubmit = !!selected && !loading;

  return (
    <div className="space-y-4 rounded-xl border border-border/60 bg-card/95 p-6 shadow-lg backdrop-blur-sm sm:p-8">
      <div>
        <h2 className="text-lg font-semibold">{t("model.title")}</h2>
        <p className="mt-1 text-sm text-muted-foreground">
          {t("model.subtitle", { provider: provider.display_name || provider.name })}
        </p>
      </div>

      {loading && (
        <div className="flex items-center gap-2 text-sm text-muted-foreground">
          <Loader2 className="h-4 w-4 animate-spin" />
          {t("model.loading")}
        </div>
      )}

      {error && !loading && (
        <p className="rounded-md border border-destructive/50 bg-destructive/10 px-3 py-2 text-sm text-destructive">
          {error}
        </p>
      )}

      {!loading && !error && models.length === 0 && (
        <p className="rounded-md border border-amber-500/50 bg-amber-500/10 px-3 py-2 text-sm">
          {t("model.empty")}
        </p>
      )}

      {!loading && models.length > 0 && (
        <div className="max-h-72 space-y-1.5 overflow-y-auto rounded-md border border-border/60 p-2">
          {models.map((m) => (
            <label
              key={m.id}
              className={
                "flex cursor-pointer items-center gap-2 rounded-md px-3 py-2 text-sm hover:bg-accent/50 " +
                (selected === m.id ? "bg-accent" : "")
              }
            >
              <input
                type="radio"
                name="model"
                value={m.id}
                checked={selected === m.id}
                onChange={() => setSelected(m.id)}
                className="h-4 w-4 accent-primary"
              />
              <span className="truncate">{m.name || m.id}</span>
            </label>
          ))}
        </div>
      )}

      <div className="flex justify-between pt-2">
        <Button type="button" variant="outline" onClick={onBack}>
          {t("nav.back")}
        </Button>
        <Button
          type="button"
          disabled={!canSubmit}
          onClick={() => selected && onComplete(selected)}
        >
          {t("nav.next")}
        </Button>
      </div>
    </div>
  );
}
