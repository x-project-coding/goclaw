import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { Loader2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { useProviders } from "@/pages/providers/hooks/use-providers";
import { PROVIDER_TYPES } from "@/constants/providers";
import type { ProviderData } from "@/types/provider";

interface StepProviderProps {
  onComplete: (provider: ProviderData) => void;
}

// Simple slugify so the user only types a display name; we derive the unique
// `name` (DB key) from it. Keeps the wizard a 2-input form.
function slugify(input: string): string {
  return input
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "")
    .slice(0, 48);
}

export function StepProvider({ onComplete }: StepProviderProps) {
  const { t } = useTranslation("setup");
  const { createProvider } = useProviders(false); // disabled fetch: wizard cares about creation only
  const [providerType, setProviderType] = useState<string>("anthropic_native");
  const [displayName, setDisplayName] = useState("");
  const [apiKey, setApiKey] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const meta = useMemo(
    () => PROVIDER_TYPES.find((pt) => pt.value === providerType),
    [providerType],
  );
  // CLI / OAuth providers don't take a typed api key — accept empty submit.
  const requiresApiKey = !["claude_cli", "chatgpt_oauth", "acp", "codex_cli"].includes(providerType);

  const canSubmit =
    !submitting &&
    displayName.trim().length > 0 &&
    (!requiresApiKey || apiKey.trim().length > 0);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!canSubmit) return;
    setSubmitting(true);
    setError(null);
    try {
      const created = await createProvider({
        name: slugify(displayName) || `provider-${Date.now()}`,
        display_name: displayName.trim(),
        provider_type: providerType,
        api_base: meta?.apiBase || undefined,
        api_key: requiresApiKey ? apiKey.trim() : undefined,
        enabled: true,
      });
      onComplete(created);
    } catch (err) {
      setError(err instanceof Error ? err.message : t("provider.error.generic"));
      setSubmitting(false);
    }
  };

  return (
    <form
      onSubmit={handleSubmit}
      className="space-y-4 rounded-xl border border-border/60 bg-card/95 p-6 shadow-lg backdrop-blur-sm sm:p-8"
    >
      <div>
        <h2 className="text-lg font-semibold">{t("provider.title")}</h2>
        <p className="mt-1 text-sm text-muted-foreground">{t("provider.subtitle")}</p>
      </div>

      <div className="space-y-2">
        <Label htmlFor="provider-type">{t("provider.typeLabel")}</Label>
        <Select value={providerType} onValueChange={setProviderType}>
          <SelectTrigger id="provider-type">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {PROVIDER_TYPES.map((pt) => (
              <SelectItem key={pt.value} value={pt.value}>
                {pt.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>

      <div className="space-y-2">
        <Label htmlFor="display-name">{t("provider.nameLabel")}</Label>
        <Input
          id="display-name"
          value={displayName}
          onChange={(e) => setDisplayName(e.target.value)}
          placeholder={t("provider.namePlaceholder")}
          autoFocus
          disabled={submitting}
          className="text-base md:text-sm"
        />
      </div>

      {requiresApiKey && (
        <div className="space-y-2">
          <Label htmlFor="api-key">{t("provider.apiKeyLabel")}</Label>
          <Input
            id="api-key"
            type="password"
            value={apiKey}
            onChange={(e) => setApiKey(e.target.value)}
            placeholder={meta?.placeholder || t("provider.apiKeyPlaceholder")}
            disabled={submitting}
            autoComplete="off"
            className="text-base md:text-sm"
          />
        </div>
      )}

      {error && (
        <p className="rounded-md border border-destructive/50 bg-destructive/10 px-3 py-2 text-sm text-destructive">
          {error}
        </p>
      )}

      <div className="flex justify-end pt-2">
        <Button type="submit" disabled={!canSubmit}>
          {submitting && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
          {t("provider.submit")}
        </Button>
      </div>
    </form>
  );
}
