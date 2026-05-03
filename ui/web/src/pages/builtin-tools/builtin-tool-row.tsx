import { useTranslation } from "react-i18next";
import { Settings, Info } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Switch } from "@/components/ui/switch";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import type { BuiltinToolData } from "./hooks/use-builtin-tools";

// Tools with dedicated settings forms always show the Settings button,
// even when settings is empty (the form lets the user create settings).
const TOOLS_WITH_DEDICATED_FORM = new Set([
  "web_search", "web_fetch", "tts", "knowledge_graph_search",
  "create_image", "create_audio", "create_video",
]);

export function hasEditableSettings(tool: BuiltinToolData): boolean {
  if (TOOLS_WITH_DEDICATED_FORM.has(tool.name)) return true;
  return tool.settings != null && Object.keys(tool.settings).length > 0;
}

export function getConfigHint(tool: BuiltinToolData): string | undefined {
  return (tool.metadata as any)?.config_hint as string | undefined;
}

export function isDeprecated(tool: BuiltinToolData): boolean {
  return (tool.metadata as any)?.deprecated === true;
}

interface ToolRowProps {
  tool: BuiltinToolData;
  onToggle: (tool: BuiltinToolData) => void;
  onSettings: (tool: BuiltinToolData) => void;
}

export function ToolRow({ tool, onToggle, onSettings }: ToolRowProps) {
  const { t } = useTranslation("tools");
  const configHint = getConfigHint(tool);
  const editable = hasEditableSettings(tool);
  const deprecated = isDeprecated(tool);

  return (
    <div className={`flex items-center gap-4 px-4 py-2 hover:bg-muted/30 transition-colors${deprecated ? " opacity-60" : ""}`}>
      <div className="min-w-0 flex-1">
        <div className="flex items-baseline gap-1.5">
          <span className="text-sm font-medium leading-tight">{tool.display_name}</span>
          <code className="text-xs-plus text-muted-foreground">{tool.name}</code>
          {deprecated && (
            <TooltipProvider delayDuration={200}>
              <Tooltip>
                <TooltipTrigger asChild>
                  <Badge variant="destructive" className="ml-1 h-4 px-1 text-2xs leading-none cursor-default">
                    {t("builtin.deprecated")}
                  </Badge>
                </TooltipTrigger>
                <TooltipContent side="top">
                  <p className="text-xs">{t("builtin.deprecatedTooltip")}</p>
                </TooltipContent>
              </Tooltip>
            </TooltipProvider>
          )}
          {!deprecated && tool.requires && tool.requires.length > 0 && (
            <TooltipProvider delayDuration={200}>
              <Tooltip>
                <TooltipTrigger asChild>
                  <Badge variant="outline" className="ml-1 h-4 px-1 text-2xs leading-none cursor-default">
                    {t("builtin.requires")}
                  </Badge>
                </TooltipTrigger>
                <TooltipContent side="top">
                  <p className="text-xs">{t("builtin.requiresTooltip", { list: tool.requires.join(", ") })}</p>
                </TooltipContent>
              </Tooltip>
            </TooltipProvider>
          )}
        </div>
        {tool.description && (
          <p className="text-xs text-muted-foreground leading-snug truncate mt-0.5">
            {t(`builtin.descriptions.${tool.name}`, tool.description)}
          </p>
        )}
      </div>

      <div className="flex items-center gap-1.5 shrink-0">
        {editable && !deprecated && (
          <Button
            variant="ghost"
            size="sm"
            onClick={() => onSettings(tool)}
            className="h-7 gap-1 px-2 text-xs"
          >
            <Settings className="h-3 w-3" />
            {t("builtin.settings")}
          </Button>
        )}
        {!editable && !deprecated && configHint && (
          <TooltipProvider delayDuration={200}>
            <Tooltip>
              <TooltipTrigger asChild>
                <span className="flex items-center gap-1 text-xs-plus text-muted-foreground cursor-default">
                  <Info className="h-3 w-3" />
                  {configHint}
                </span>
              </TooltipTrigger>
              <TooltipContent side="top">
                <p className="text-xs">{t("builtin.configuredVia")}</p>
              </TooltipContent>
            </Tooltip>
          </TooltipProvider>
        )}
        <Switch
          checked={tool.enabled}
          onCheckedChange={() => onToggle(tool)}
          disabled={deprecated}
        />
      </div>
    </div>
  );
}
