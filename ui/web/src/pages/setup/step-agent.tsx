import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Loader2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { useAgents } from "@/pages/agents/hooks/use-agents";
import type { AgentData } from "@/types/agent";
import type { ProviderData } from "@/types/provider";

interface StepAgentProps {
  provider: ProviderData;
  modelId: string;
  onBack: () => void;
  onComplete: (agent: AgentData) => void;
}

function slugify(input: string): string {
  return input
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "")
    .slice(0, 48);
}

export function StepAgent({ provider, modelId, onBack, onComplete }: StepAgentProps) {
  const { t } = useTranslation("setup");
  const { createAgent } = useAgents();
  const [displayName, setDisplayName] = useState("");
  const [description, setDescription] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const canSubmit = !submitting && displayName.trim().length > 0;

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!canSubmit) return;
    setSubmitting(true);
    setError(null);
    try {
      const created = await createAgent({
        agent_key: slugify(displayName) || `agent-${Date.now()}`,
        display_name: displayName.trim(),
        provider: provider.name,
        model: modelId,
        agent_description: description.trim() || null,
      });
      onComplete(created);
    } catch (err) {
      setError(err instanceof Error ? err.message : t("agent.error.generic"));
      setSubmitting(false);
    }
  };

  return (
    <form
      onSubmit={handleSubmit}
      className="space-y-4 rounded-xl border border-border/60 bg-card/95 p-6 shadow-lg backdrop-blur-sm sm:p-8"
    >
      <div>
        <h2 className="text-lg font-semibold">{t("agent.title")}</h2>
        <p className="mt-1 text-sm text-muted-foreground">{t("agent.subtitle")}</p>
      </div>

      <div className="space-y-2">
        <Label htmlFor="agent-name">{t("agent.nameLabel")}</Label>
        <Input
          id="agent-name"
          value={displayName}
          onChange={(e) => setDisplayName(e.target.value)}
          placeholder={t("agent.namePlaceholder")}
          autoFocus
          disabled={submitting}
          className="text-base md:text-sm"
        />
      </div>

      <div className="space-y-2">
        <Label htmlFor="agent-desc">{t("agent.descLabel")}</Label>
        <textarea
          id="agent-desc"
          value={description}
          onChange={(e) => setDescription(e.target.value)}
          placeholder={t("agent.descPlaceholder")}
          rows={3}
          disabled={submitting}
          className="flex w-full rounded-md border border-input bg-background px-3 py-2 text-base shadow-sm placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring disabled:cursor-not-allowed disabled:opacity-50 md:text-sm"
        />
      </div>

      <div className="rounded-md bg-muted/50 px-3 py-2 text-xs text-muted-foreground">
        {t("agent.usingProvider", {
          provider: provider.display_name || provider.name,
          model: modelId,
        })}
      </div>

      {error && (
        <p className="rounded-md border border-destructive/50 bg-destructive/10 px-3 py-2 text-sm text-destructive">
          {error}
        </p>
      )}

      <div className="flex justify-between pt-2">
        <Button type="button" variant="outline" onClick={onBack} disabled={submitting}>
          {t("nav.back")}
        </Button>
        <Button type="submit" disabled={!canSubmit}>
          {submitting && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
          {t("agent.submit")}
        </Button>
      </div>
    </form>
  );
}
