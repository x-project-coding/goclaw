import { useEffect, useMemo, useRef } from "react";
import { useTranslation } from "react-i18next";
import { Label } from "@/components/ui/label";
import { Combobox, type ComboboxOption } from "@/components/ui/combobox";
import { ProjectPicker } from "@/components/shared/project-picker";
import { useTeams } from "@/pages/teams/hooks/use-teams";
import { useProjects } from "@/pages/projects/hooks/use-projects";

/**
 * MCP server visibility scope. Mirrors the BE invariant:
 *   - global   → both teamId and projectId NULL
 *   - team     → teamId set, projectId NULL
 *   - project  → projectId set, teamId NULL
 *
 * The two IDs are mutually exclusive — switching scope clears the unrelated id
 * so the form state never violates the BE CHECK constraint.
 */
export type McpScope = "global" | "team" | "project";

interface McpScopePickerProps {
  scope: McpScope;
  teamId: string | null;
  projectId: string | null;
  onChange: (next: { scope: McpScope; teamId: string | null; projectId: string | null }) => void;
  /** When true, all controls render as disabled — used when editing (BE allow-list excludes scope on update). */
  disabled?: boolean;
  /** Optional id stub to keep label/control association unique if multiple instances render on one page. */
  idPrefix?: string;
}

const SCOPES: McpScope[] = ["global", "team", "project"];

export function McpScopePicker({
  scope,
  teamId,
  projectId,
  onChange,
  disabled = false,
  idPrefix = "mcp-scope",
}: McpScopePickerProps) {
  const { t } = useTranslation("mcp");
  const { teams, load: loadTeams } = useTeams();
  const { projects, load: loadProjects } = useProjects();
  const loadedRef = useRef(false);

  useEffect(() => {
    if (loadedRef.current) return;
    loadedRef.current = true;
    loadTeams();
    // Pre-load projects so disabled mode can resolve a project name without
    // mounting ProjectPicker (which loads on its own).
    loadProjects({ status: "active" });
  }, [loadTeams, loadProjects]);

  const scopeOptions: ComboboxOption[] = useMemo(
    () => SCOPES.map((s) => ({ value: s, label: t(`scope.values.${s}`) })),
    [t],
  );

  const teamOptions: ComboboxOption[] = useMemo(
    () => teams.map((tm) => ({ value: tm.id, label: tm.name })),
    [teams],
  );

  const handleScopeChange = (next: string) => {
    const ns = next as McpScope;
    if (ns === scope) return;
    onChange({
      scope: ns,
      teamId: ns === "team" ? teamId : null,
      projectId: ns === "project" ? projectId : null,
    });
  };

  if (disabled) {
    const teamName = teams.find((tm) => tm.id === teamId)?.name;
    const project = projects.find((p) => p.id === projectId);
    const projectLabel = project
      ? (() => {
          const meta = (project.metadata ?? {}) as Record<string, unknown>;
          const display = typeof meta.displayName === "string" && meta.displayName ? meta.displayName : project.slug;
          return display === project.slug ? project.slug : `${display} (${project.slug})`;
        })()
      : projectId;
    let summary = t("scope.values.global");
    if (scope === "team") summary = t("scope.summaries.team", { name: teamName ?? teamId ?? "—" });
    else if (scope === "project") summary = t("scope.summaries.project", { name: projectLabel ?? "—" });

    return (
      <div className="grid gap-1.5">
        <Label>{t("scope.label")}</Label>
        <div
          className="rounded-md border border-input bg-muted/40 px-3 py-2 text-sm"
          aria-readonly="true"
          data-testid={`${idPrefix}-readonly`}
        >
          {summary}
        </div>
        <p className="text-xs text-muted-foreground">{t("scope.editLockHint")}</p>
      </div>
    );
  }

  return (
    <div className="grid gap-1.5">
      <Label htmlFor={`${idPrefix}-scope`}>{t("scope.label")}</Label>
      <Combobox
        value={scope}
        onChange={handleScopeChange}
        options={scopeOptions}
        placeholder={t("scope.label")}
        allowCustom={false}
      />
      <p className="text-xs text-muted-foreground">{t(`scope.hints.${scope}`)}</p>

      {scope === "team" && (
        <div className="grid gap-1.5 mt-2">
          <Label htmlFor={`${idPrefix}-team`}>{t("scope.teamLabel")}</Label>
          <Combobox
            value={teamId ?? ""}
            onChange={(v) => onChange({ scope: "team", teamId: v || null, projectId: null })}
            options={teamOptions}
            placeholder={t("scope.teamPlaceholder")}
            allowCustom={false}
          />
        </div>
      )}

      {scope === "project" && (
        <div className="grid gap-1.5 mt-2">
          <Label htmlFor={`${idPrefix}-project`}>{t("scope.projectLabel")}</Label>
          <ProjectPicker
            value={projectId}
            onChange={(v) =>
              onChange({
                scope: "project",
                teamId: null,
                projectId: typeof v === "string" ? v : null,
              })
            }
            placeholder={t("scope.projectPlaceholder")}
          />
        </div>
      )}
    </div>
  );
}
