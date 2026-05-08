import { useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { Trash2, UserPlus } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Combobox, type ComboboxOption } from "@/components/ui/combobox";
import { UserPickerCombobox } from "@/components/shared/user-picker-combobox";
import { ProjectRoleChip } from "@/pages/projects/components/project-role-chip";
import { useTeams } from "@/pages/teams/hooks/use-teams";
import { useAgentShares, type ShareRole } from "./use-agent-shares";

interface AgentSharesTabProps {
  agentId: string;
}

type TargetKind = "user" | "team";

/**
 * Lists explicit grants for a single agent and lets owners attach/revoke
 * (user|team) → role rows. Mirrors the BE invariant — exactly one of
 * user/team is set per row — by toggling a single target kind in the
 * "Add" form rather than exposing both inputs at once.
 */
export function AgentSharesTab({ agentId }: AgentSharesTabProps) {
  const { t } = useTranslation("agents");
  const { shares, loading, addShare, removeShare } = useAgentShares(agentId);
  const { teams, load: loadTeams } = useTeams();

  const [kind, setKind] = useState<TargetKind>("user");
  const [userValue, setUserValue] = useState("");
  const [teamValue, setTeamValue] = useState("");
  const [role, setRole] = useState<ShareRole>("viewer");
  const [submitting, setSubmitting] = useState(false);

  useEffect(() => {
    loadTeams();
  }, [loadTeams]);

  const teamOptions: ComboboxOption[] = useMemo(
    () => teams.map((tm) => ({ value: tm.id, label: tm.name })),
    [teams],
  );
  const teamLabel = (id: string) => teams.find((tm) => tm.id === id)?.name ?? id;

  const handleAdd = async () => {
    const target = kind === "user" ? { userId: userValue.trim() } : { teamId: teamValue.trim() };
    if ((kind === "user" && !target.userId) || (kind === "team" && !target.teamId)) return;
    setSubmitting(true);
    try {
      const ok = await addShare({ ...target, role });
      if (ok) {
        setUserValue("");
        setTeamValue("");
        setRole("viewer");
      }
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div className="space-y-6">
      <section className="rounded-md border p-4">
        <h3 className="text-sm font-medium">{t("shares.addTitle")}</h3>
        <p className="text-xs text-muted-foreground mt-1">{t("shares.addHint")}</p>

        <div className="mt-3 flex flex-wrap items-end gap-2">
          <div className="inline-flex items-center rounded-md border p-0.5 text-xs">
            {(["user", "team"] as TargetKind[]).map((k) => (
              <button
                key={k}
                type="button"
                role="radio"
                aria-checked={kind === k}
                onClick={() => setKind(k)}
                className={
                  "rounded px-2.5 py-1 font-medium capitalize " +
                  (kind === k ? "bg-primary text-primary-foreground" : "text-muted-foreground hover:text-foreground")
                }
              >
                {t(`shares.target.${k}`)}
              </button>
            ))}
          </div>

          {kind === "user" ? (
            <UserPickerCombobox
              value={userValue}
              onChange={setUserValue}
              valueMode="uuid"
              allowCustom={false}
              placeholder={t("shares.userPlaceholder")}
              className="min-w-[260px]"
            />
          ) : (
            <Combobox
              value={teamValue}
              onChange={setTeamValue}
              options={teamOptions}
              allowCustom={false}
              placeholder={t("shares.teamPlaceholder")}
              className="min-w-[260px]"
            />
          )}

          <ProjectRoleChip value={role} onChange={(r) => setRole(r as ShareRole)} />

          <Button onClick={handleAdd} disabled={submitting} size="sm" className="gap-1">
            <UserPlus className="h-3.5 w-3.5" />
            {submitting ? t("shares.adding") : t("shares.add")}
          </Button>
        </div>
      </section>

      <section>
        <h3 className="text-sm font-medium mb-2">
          {t("shares.listTitle")}
          <span className="ml-2 text-xs text-muted-foreground">({shares.length})</span>
        </h3>

        {loading && shares.length === 0 ? (
          <p className="text-sm text-muted-foreground">{t("shares.loading")}</p>
        ) : shares.length === 0 ? (
          <p className="text-sm text-muted-foreground">{t("shares.empty")}</p>
        ) : (
          <ul className="rounded-md border divide-y">
            {shares.map((s) => {
              const isUser = !!s.shared_with_user_id;
              const targetLabel = isUser
                ? s.shared_with_user_id ?? "—"
                : teamLabel(s.shared_with_team_id ?? "");
              return (
                <li key={s.id} className="flex items-center gap-3 px-3 py-2">
                  <Badge variant="outline" className="text-xs-plus capitalize">
                    {t(`shares.target.${isUser ? "user" : "team"}`)}
                  </Badge>
                  <span className="text-sm font-mono text-muted-foreground truncate">{targetLabel}</span>
                  <ProjectRoleChip value={s.role} size="sm" />
                  <Button
                    variant="ghost"
                    size="sm"
                    className="ml-auto text-destructive hover:text-destructive"
                    onClick={() =>
                      removeShare(
                        isUser
                          ? { userId: s.shared_with_user_id ?? "" }
                          : { teamId: s.shared_with_team_id ?? "" },
                      )
                    }
                    title={t("shares.remove")}
                  >
                    <Trash2 className="h-3.5 w-3.5" />
                  </Button>
                </li>
              );
            })}
          </ul>
        )}
      </section>
    </div>
  );
}
