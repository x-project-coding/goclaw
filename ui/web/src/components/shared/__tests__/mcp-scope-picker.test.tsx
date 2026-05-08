/**
 * mcp-scope-picker covers two paths:
 *   - editable mode (create) — radio combobox, selecting team/project clears the unrelated id
 *   - disabled mode (edit) — read-only summary resolving team/project name
 *
 * The interaction surface uses Combobox which renders into a portal. To keep
 * tests stable we exercise the disabled summary path (deterministic DOM) and
 * stub the contained data hooks.
 */
import { describe, it, expect, vi } from "vitest";
import { render, screen } from "@testing-library/react";

vi.mock("react-i18next", () => ({
  useTranslation: () => ({
    t: (k: string, opts?: Record<string, unknown>) => {
      if (opts && "name" in opts) return `${k}:${opts.name}`;
      return k;
    },
  }),
}));

vi.mock("@/pages/teams/hooks/use-teams", () => ({
  useTeams: () => ({
    teams: [{ id: "team-1", name: "Alpha", lead_agent_id: "x", status: "active", created_by: "u" }],
    load: vi.fn(),
  }),
}));

vi.mock("@/pages/projects/hooks/use-projects", () => ({
  useProjects: () => ({
    projects: [{ id: "proj-1", slug: "ops", metadata: { displayName: "Ops" }, status: "active" }],
    load: vi.fn(),
  }),
}));

// ProjectPicker pulls in the Combobox + react-query plumbing transitively;
// stub it to a marker so we can assert it would render in editable+project mode.
vi.mock("@/components/shared/project-picker", () => ({
  ProjectPicker: ({ value }: { value: string | null }) => (
    <div data-testid="project-picker-stub">{value ?? "(none)"}</div>
  ),
}));

import { McpScopePicker } from "../mcp-scope-picker";

describe("McpScopePicker — disabled (edit) mode", () => {
  it("renders a read-only summary for team scope using the resolved team name", () => {
    render(
      <McpScopePicker
        scope="team"
        teamId="team-1"
        projectId={null}
        onChange={() => {}}
        disabled
      />,
    );
    const ro = screen.getByTestId("mcp-scope-readonly");
    expect(ro.textContent).toContain("Alpha");
  });

  it("renders a read-only summary for project scope using project displayName", () => {
    render(
      <McpScopePicker
        scope="project"
        teamId={null}
        projectId="proj-1"
        onChange={() => {}}
        disabled
      />,
    );
    const ro = screen.getByTestId("mcp-scope-readonly");
    expect(ro.textContent).toContain("Ops");
  });

  it("shows the editLockHint instead of an editable hint", () => {
    render(
      <McpScopePicker
        scope="global"
        teamId={null}
        projectId={null}
        onChange={() => {}}
        disabled
      />,
    );
    expect(screen.getByText("scope.editLockHint")).toBeTruthy();
  });
});

describe("McpScopePicker — editable mode wiring", () => {
  it("renders ProjectPicker stub when scope=project", () => {
    render(
      <McpScopePicker
        scope="project"
        teamId={null}
        projectId="proj-1"
        onChange={() => {}}
      />,
    );
    expect(screen.getByTestId("project-picker-stub")).toBeTruthy();
  });

  it("does not render ProjectPicker for scope=global", () => {
    render(
      <McpScopePicker
        scope="global"
        teamId={null}
        projectId={null}
        onChange={() => {}}
      />,
    );
    expect(screen.queryByTestId("project-picker-stub")).toBeNull();
  });
});
