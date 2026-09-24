import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";
import type { ProjectMap } from "@/api";
import { ProjectArchitectureMap } from "@/components/projects/map/ProjectArchitectureMap";
import { I18nProvider } from "@/hooks/useI18n";

const { updateLink } = vi.hoisted(() => ({ updateLink: vi.fn() }));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return { ...actual, api: { ...actual.api, updateLink } };
});

const map: ProjectMap = {
  project: { id: "proj-1", name: "Acme Shop" },
  nodes: [
    {
      id: "c:comp-api",
      kind: "component",
      tier: "service",
      label: "api",
      role: "backend",
      component_id: "comp-api",
      repository_id: "repo-1",
      repository_name: "acme-platform",
      path: "services/api",
      foreign: false,
      stack_summary: "Go 1.23",
      provider: "gcp",
      health: "healthy",
      error_count_24h: 0,
    },
    {
      id: "r:res-db",
      kind: "resource",
      tier: "data",
      label: "acme-prod",
      resource_kind: "database",
      vendor: "Cloud SQL",
      resource_id: "res-db",
      foreign: false,
    },
  ],
  edges: [
    {
      id: "e:link-1",
      link_id: "link-1",
      from: "c:comp-api",
      to: "r:res-db",
      protocol: "sql",
      status: "suggested",
      source: "scan",
      cross_project: false,
    },
  ],
};

function renderMap(onChanged = vi.fn()) {
  render(
    <I18nProvider>
      <MemoryRouter>
        <ProjectArchitectureMap map={map} currentProjectId="proj-1" crossProjects={[]} onChanged={onChanged} />
      </MemoryRouter>
    </I18nProvider>,
  );
  return { onChanged };
}

describe("ProjectArchitectureMap", () => {
  it("renders a node per component and resource in the fixture", () => {
    renderMap();
    expect(screen.getByText("api")).toBeInTheDocument();
    expect(screen.getByText("acme-prod")).toBeInTheDocument();
  });

  it("shows an empty state when the project has no components", () => {
    render(
      <I18nProvider>
        <MemoryRouter>
          <ProjectArchitectureMap
            map={{ project: { id: "proj-1", name: "Acme Shop" }, nodes: [], edges: [] }}
            currentProjectId="proj-1"
            crossProjects={[]}
            onChanged={vi.fn()}
          />
        </MemoryRouter>
      </I18nProvider>,
    );
    expect(screen.getByText("Nothing to map yet")).toBeInTheDocument();
  });

  it("selecting a node opens the side panel with its repository detail", () => {
    renderMap();
    fireEvent.click(screen.getByText("api"));
    const panel = within(screen.getByTestId("map-side-panel"));
    expect(panel.getByText("Repository")).toBeInTheDocument();
    expect(panel.getByText("acme-platform/services/api")).toBeInTheDocument();
  });

  it("confirming a suggested edge from the side panel calls api.updateLink", async () => {
    updateLink.mockReset().mockResolvedValue({});
    const { onChanged } = renderMap();

    fireEvent.click(screen.getByText("api"));
    // The outgoing list row for the suggested edge to acme-prod.
    fireEvent.click(within(screen.getByTestId("map-side-panel")).getByText("→ acme-prod"));

    fireEvent.click(within(screen.getByTestId("map-side-panel")).getByRole("button", { name: "Link it" }));

    await waitFor(() => expect(updateLink).toHaveBeenCalledWith("link-1", { status: "confirmed" }));
    await waitFor(() => expect(onChanged).toHaveBeenCalled());
  });

  it("dismissing a suggested edge from the side panel calls api.updateLink with dismissed", async () => {
    updateLink.mockReset().mockResolvedValue({});
    renderMap();

    fireEvent.click(screen.getByText("api"));
    fireEvent.click(within(screen.getByTestId("map-side-panel")).getByText("→ acme-prod"));
    fireEvent.click(within(screen.getByTestId("map-side-panel")).getByRole("button", { name: "Ignore" }));

    await waitFor(() => expect(updateLink).toHaveBeenCalledWith("link-1", { status: "dismissed" }));
  });
});
