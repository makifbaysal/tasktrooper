import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { InitiativeProject, ProjectsOverview } from "@/api";
import { I18nProvider } from "@/hooks/useI18n";
import { ProjectsPage } from "@/pages/ProjectsPage";

const { getProjectsOverview, createInitiativeProject } = vi.hoisted(() => ({
  getProjectsOverview: vi.fn(),
  createInitiativeProject: vi.fn(),
}));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return {
    ...actual,
    api: {
      ...actual.api,
      getProjectsOverview,
      createInitiativeProject,
    },
  };
});

const overview: ProjectsOverview = {
  projects: [
    {
      id: "proj-shop",
      name: "Acme Shop",
      description: "Orders and checkout",
      type: "multi_repo",
      review_count: 0,
      cross_projects: [{ id: "proj-billing", name: "Acme Billing" }],
      cross_links: 1,
      shared_resources: [],
      repositories: [
        {
          id: "repo-platform",
          name: "acme-platform",
          description: "",
          project_ids: ["proj-shop"],
          shape: "monorepo",
          components: [
            { id: "c-web", path: "apps/web", name: "web", role: "frontend", stack_summary: "Next.js 15", checks: 2, required_checks: 2 },
            { id: "c-api", path: "services/api", name: "api", role: "backend", stack_summary: "Go 1.23", checks: 3, required_checks: 3 },
          ],
          review_count: 0,
          environments: [],
          updated_at: "2026-01-01T00:00:00Z",
        },
        {
          id: "repo-mobile",
          name: "acme-mobile",
          description: "",
          project_ids: ["proj-shop"],
          shape: "single",
          components: [
            { id: "c-mobile", path: ".", name: "mobile", role: "mobile", stack_summary: "Expo 52", checks: 1, required_checks: 1 },
          ],
          review_count: 0,
          environments: [],
          updated_at: "2026-01-01T00:00:00Z",
        },
      ],
    },
    {
      id: "proj-billing",
      name: "Acme Billing",
      description: "Billing service",
      type: "single_repo",
      review_count: 0,
      cross_projects: [],
      cross_links: 0,
      shared_resources: [],
      repositories: [
        {
          id: "repo-billing",
          name: "billing-svc",
          description: "",
          project_ids: ["proj-billing"],
          shape: "single",
          components: [
            { id: "c-billing", path: ".", name: "billing-svc", role: "backend", stack_summary: "NestJS 10", checks: 2, required_checks: 2 },
          ],
          review_count: 0,
          environments: [],
          updated_at: "2026-01-01T00:00:00Z",
        },
      ],
    },
  ],
  unassigned: [
    {
      id: "repo-scratch",
      name: "scratch-repo",
      description: "",
      project_ids: [],
      shape: "single",
      components: [],
      review_count: 0,
      environments: [],
      updated_at: "2026-01-01T00:00:00Z",
    },
  ],
};

function renderPage(initialEntries: string[] = ["/projects"]) {
  return render(
    <I18nProvider>
      <MemoryRouter initialEntries={initialEntries}>
        <Routes>
          <Route path="/projects" element={<ProjectsPage />} />
          <Route path="/projects/new" element={<div>Add repository flow</div>} />
        </Routes>
      </MemoryRouter>
    </I18nProvider>,
  );
}

describe("ProjectsPage", () => {
  beforeEach(() => {
    getProjectsOverview.mockReset().mockResolvedValue(overview);
    createInitiativeProject.mockReset();
  });

  it("renders a card per project and the unassigned repositories from the fixture", async () => {
    renderPage();

    expect(await screen.findByText("Acme Shop")).toBeInTheDocument();
    // "Acme Billing" appears twice: its own card title, and the cross-project
    // link inside Acme Shop's footer (see the dedicated footer test below).
    expect(screen.getAllByText("Acme Billing")).toHaveLength(2);
    expect(screen.getByText("acme-platform")).toBeInTheDocument();
    expect(screen.getByText("acme-mobile")).toBeInTheDocument();
    expect(screen.getByText("billing-svc")).toBeInTheDocument();

    expect(screen.getByText("Repositories without a project")).toBeInTheDocument();
    expect(screen.getByText("scratch-repo")).toBeInTheDocument();
  });

  it("hides non-matching repos (and empty-after-filter projects) when a role chip is selected", async () => {
    renderPage();
    await screen.findByText("Acme Shop");

    fireEvent.click(screen.getByRole("button", { name: "Mobile" }));

    await waitFor(() => expect(screen.queryByText("acme-platform")).not.toBeInTheDocument());
    expect(screen.getByText("acme-mobile")).toBeInTheDocument();
    // Acme Billing has only a backend component, so its card (and its one
    // repository) drops out entirely — only the cross-project mention of it
    // inside Acme Shop's footer remains.
    expect(screen.queryByText("billing-svc")).not.toBeInTheDocument();
    expect(screen.getAllByText("Acme Billing")).toHaveLength(1);
  });

  it("shows the cross-project footer line only for the project that has cross_projects", async () => {
    renderPage();
    await screen.findByText("Acme Shop");

    expect(screen.getAllByText("Links to")).toHaveLength(1);
  });

  it("creates a project through the New project dialog and hands off to add a repository", async () => {
    const created: InitiativeProject = {
      id: "proj-new",
      name: "New One",
      description: "",
      created_at: "2026-01-01T00:00:00Z",
      updated_at: "2026-01-01T00:00:00Z",
    };
    createInitiativeProject.mockResolvedValue(created);

    renderPage();
    await screen.findByText("Acme Shop");

    fireEvent.click(screen.getByRole("button", { name: "New project" }));
    fireEvent.change(await screen.findByLabelText("Name"), { target: { value: "New One" } });
    fireEvent.click(screen.getByRole("button", { name: "Create" }));

    await waitFor(() =>
      expect(createInitiativeProject).toHaveBeenCalledWith({ name: "New One", description: "" }),
    );
    expect(await screen.findByText("Add repository flow")).toBeInTheDocument();
  });
});
