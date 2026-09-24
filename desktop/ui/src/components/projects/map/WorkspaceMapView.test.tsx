import "@testing-library/jest-dom/vitest";
import { render, screen, within } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it } from "vitest";
import type { WorkspaceMap } from "@/api";
import { WorkspaceMapView } from "@/components/projects/map/WorkspaceMapView";
import { I18nProvider } from "@/hooks/useI18n";

const map: WorkspaceMap = {
  projects: [
    {
      id: "proj-shop",
      name: "Acme Shop",
      type: "single_repo",
      repositories: [
        {
          id: "repo-platform",
          name: "acme-platform",
          shape: "single",
          components: [{ id: "c-api", path: ".", name: "api", role: "backend", stack_summary: "Go 1.23", checks: 1, required_checks: 1 }],
        },
      ],
    },
    {
      id: "proj-billing",
      name: "Acme Billing",
      type: "single_repo",
      repositories: [
        {
          id: "repo-billing",
          name: "billing-svc",
          shape: "single",
          components: [{ id: "c-billing", path: ".", name: "billing-svc", role: "backend", stack_summary: "NestJS 10", checks: 1, required_checks: 1 }],
        },
      ],
    },
    {
      id: "proj-tasktrooper",
      name: "TaskTrooper",
      type: "monorepo",
      repositories: [
        {
          id: "repo-tt",
          name: "tasktrooper-oss",
          shape: "monorepo",
          components: [{ id: "c-server", path: "server", name: "server", role: "backend", stack_summary: "Go", checks: 1, required_checks: 1 }],
        },
      ],
    },
  ],
  unassigned: [],
  edges: [
    { from_project_id: "proj-shop", to_project_id: "proj-billing", links: 1, suggested: 0, protocols: ["http"], examples: [] },
  ],
  shared_resources: [],
};

function renderView() {
  render(
    <I18nProvider>
      <MemoryRouter>
        <WorkspaceMapView map={map} />
      </MemoryRouter>
    </I18nProvider>,
  );
}

describe("WorkspaceMapView", () => {
  it("renders every project's box with its repository", () => {
    renderView();
    expect(screen.getByText("Acme Shop")).toBeInTheDocument();
    expect(screen.getByText("acme-platform")).toBeInTheDocument();
    expect(screen.getByText("Acme Billing")).toBeInTheDocument();
    expect(screen.getByText("billing-svc")).toBeInTheDocument();
    expect(screen.getByText("TaskTrooper")).toBeInTheDocument();
  });

  it("captions only the project with no edge and no shared resource as Independent", () => {
    renderView();
    // Only TaskTrooper has no edges and no shared resources in the fixture.
    expect(screen.getAllByText("Independent")).toHaveLength(1);

    const taskTrooperNode = screen.getByText("TaskTrooper").closest(".react-flow__node") as HTMLElement;
    expect(within(taskTrooperNode).getByText("Independent")).toBeInTheDocument();
  });

  it("shows no Independent caption on projects that are linked", () => {
    renderView();
    const shopNode = screen.getByText("Acme Shop").closest(".react-flow__node") as HTMLElement;
    expect(within(shopNode).queryByText("Independent")).not.toBeInTheDocument();

    const billingNode = screen.getByText("Acme Billing").closest(".react-flow__node") as HTMLElement;
    expect(within(billingNode).queryByText("Independent")).not.toBeInTheDocument();
  });
});
