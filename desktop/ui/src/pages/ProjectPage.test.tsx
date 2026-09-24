import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { ProjectDetail, ProjectsOverview, RepositoryModel } from "@/api";
import { I18nProvider } from "@/hooks/useI18n";
import { ProjectPage } from "@/pages/ProjectPage";

const { getProjectOverview, getProjectsOverview, getRepositoryModel, setRepositoryProjects } = vi.hoisted(() => ({
  getProjectOverview: vi.fn(),
  getProjectsOverview: vi.fn(),
  getRepositoryModel: vi.fn(),
  setRepositoryProjects: vi.fn(),
}));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return {
    ...actual,
    api: {
      ...actual.api,
      getProjectOverview,
      getProjectsOverview,
      getRepositoryModel,
      setRepositoryProjects,
    },
  };
});

const project: ProjectDetail = {
  id: "proj-1",
  name: "Acme Shop",
  description: "Orders and checkout",
  type: "multi_repo",
  review_count: 1,
  cross_projects: [],
  cross_links: 0,
  shared_resources: [],
  repositories: [
    {
      id: "repo-1",
      name: "acme-platform",
      description: "",
      project_ids: ["proj-1"],
      shape: "monorepo",
      components: [
        { id: "c1", path: "apps/web", name: "web", role: "frontend", stack_summary: "Next.js 15", checks: 2, required_checks: 2 },
      ],
      review_count: 1,
      last_scan: { id: "s1", status: "succeeded", trigger: "import", started_at: "2026-01-01T00:00:00Z", finished_at: "2026-01-01T00:05:00Z" },
      environments: [],
      updated_at: "2026-01-01T00:00:00Z",
    },
    {
      id: "repo-2",
      name: "acme-mobile",
      description: "",
      project_ids: ["proj-1", "proj-2"],
      shape: "single",
      components: [
        { id: "c2", path: ".", name: "mobile", role: "mobile", stack_summary: "Expo 52", checks: 1, required_checks: 1 },
      ],
      review_count: 0,
      environments: [],
      updated_at: "2026-01-01T00:00:00Z",
    },
  ],
  review: [{ kind: "role", entity_id: "c1", repository_id: "repo-1", component_id: "c1", confidence: "medium" }],
};

const repo1Model: RepositoryModel = {
  repository: {
    id: "repo-1",
    name: "acme-platform",
    description: "",
    root_path: "/repos/acme-platform",
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
  },
  shape: "monorepo",
  components: [
    {
      id: "c1",
      repository_id: "repo-1",
      path: "apps/web",
      name: { detected: "" },
      role: { detected: "frontend", confidence: "medium" },
      stack: {},
      commands: [],
      docs: {},
      gates: {},
      status: "active",
      manually_added: false,
      created_at: "2026-01-01T00:00:00Z",
      updated_at: "2026-01-01T00:00:00Z",
    },
  ],
  checks: [],
  links: [],
  incoming_links: [],
  resources: [],
  linked_components: [],
  environments: [],
  notes: [],
  review: [{ kind: "role", entity_id: "c1", repository_id: "repo-1", component_id: "c1", confidence: "medium" }],
};

const emptyOverview: ProjectsOverview = { projects: [], unassigned: [] };

function renderPage() {
  return render(
    <I18nProvider>
      <MemoryRouter initialEntries={["/projects/proj-1"]}>
        <Routes>
          <Route path="/projects/:projectId" element={<ProjectPage />} />
        </Routes>
      </MemoryRouter>
    </I18nProvider>,
  );
}

describe("ProjectPage", () => {
  beforeEach(() => {
    getProjectOverview.mockReset().mockResolvedValue(project);
    getProjectsOverview.mockReset().mockResolvedValue(emptyOverview);
    getRepositoryModel.mockReset().mockResolvedValue(repo1Model);
    setRepositoryProjects.mockReset().mockResolvedValue({});
  });

  it("renders one repositories-tab row per repository", async () => {
    renderPage();

    expect(await screen.findByText("acme-platform")).toBeInTheDocument();
    expect(screen.getByText("acme-mobile")).toBeInTheDocument();
    expect(screen.getByText("Monorepo")).toBeInTheDocument();
    expect(screen.getByText("Single")).toBeInTheDocument();
    expect(screen.getAllByText("Open")).toHaveLength(2);
  });

  it("renders ReviewList for a repository with review_count > 0 on the Review tab", async () => {
    renderPage();
    await screen.findByText("acme-platform");

    fireEvent.click(screen.getByRole("tab", { name: /Review/ }));

    expect(await screen.findByText("Is web a Frontend component?")).toBeInTheDocument();
    expect(getRepositoryModel).toHaveBeenCalledWith("repo-1");
  });

  it("removing a repository from the project sends its remaining project ids", async () => {
    renderPage();
    await screen.findByText("acme-platform");

    fireEvent.click(screen.getByRole("tab", { name: "Settings" }));

    const row = (await screen.findByText("acme-mobile")).closest("div")!;
    fireEvent.click(within(row).getByRole("button", { name: "Remove from project" }));

    await waitFor(() => expect(setRepositoryProjects).toHaveBeenCalledWith("repo-2", ["proj-2"]));
  });
});
