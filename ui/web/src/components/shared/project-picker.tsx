import { useEffect, useMemo, useRef } from "react";
import { useTranslation } from "react-i18next";
import { Combobox, type ComboboxOption } from "@/components/ui/combobox";
import { useProjects } from "@/pages/projects/hooks/use-projects";
import type { Project } from "@/types/project";

interface ProjectPickerProps {
  value: string | null;
  onChange: (projectId: string | null) => void;
  placeholder?: string;
  className?: string;
  /** Include "(none)" option that maps to null. Default false. */
  includeNone?: boolean;
  /** Include "All projects" sentinel option, value `__all__`. Default false. */
  includeAll?: boolean;
  /** Render dropdown into a portal container (useful inside dialogs). */
  portalContainer?: React.RefObject<HTMLElement | null>;
  /** Show only projects with this status. Default "active". */
  status?: "active" | "archived" | "all";
  /** Pre-loaded projects to avoid extra fetch (optional). */
  projects?: Project[];
}

const SENTINEL_NONE = "__none__";
const SENTINEL_ALL = "__all__";

export function ProjectPicker({
  value,
  onChange,
  placeholder,
  className,
  includeNone = false,
  includeAll = false,
  portalContainer,
  status = "active",
  projects: externalProjects,
}: ProjectPickerProps) {
  const { t } = useTranslation("projects");
  const { projects: hookProjects, loading, load } = useProjects();
  const loadedRef = useRef(false);

  useEffect(() => {
    if (externalProjects) return;
    if (loadedRef.current) return;
    loadedRef.current = true;
    load({ status });
  }, [externalProjects, load, status]);

  const projects = externalProjects ?? hookProjects;

  const options: ComboboxOption[] = useMemo(() => {
    const opts: ComboboxOption[] = [];
    if (includeAll) opts.push({ value: SENTINEL_ALL, label: t("picker.all") });
    if (includeNone) opts.push({ value: SENTINEL_NONE, label: t("picker.none") });
    for (const p of projects) {
      const meta = (p.metadata ?? {}) as Record<string, unknown>;
      const display = typeof meta.displayName === "string" && meta.displayName ? meta.displayName : p.slug;
      const label = display === p.slug ? p.slug : `${display} (${p.slug})`;
      opts.push({ value: p.id, label });
    }
    return opts;
  }, [projects, includeAll, includeNone, t]);

  const stringValue = value ?? (includeNone ? SENTINEL_NONE : "");

  const handleChange = (next: string) => {
    if (!next || next === SENTINEL_NONE) {
      onChange(null);
      return;
    }
    if (next === SENTINEL_ALL) {
      onChange(null);
      return;
    }
    onChange(next);
  };

  return (
    <Combobox
      value={stringValue}
      onChange={handleChange}
      options={options}
      placeholder={placeholder ?? (loading ? "..." : t("picker.placeholder"))}
      className={className}
      allowCustom={false}
      portalContainer={portalContainer}
    />
  );
}
