import "@testing-library/jest-dom/vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { InitiativeProject, ProjectScan, RepositoryModel } from "@/api";
import { I18nProvider } from "@/hooks/useI18n";
import { RepositoryPage } from "@/pages/RepositoryPage";

const { getRepositoryModel, getInitiativeProject, getLatestRepositoryScan } = vi.hoisted(() => ({
  getRepositoryModel: vi.fn(),
  getInitiativeProject: vi.fn(),
  getLatestRepositoryScan: vi.fn(),
}));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return {
    ...actual,
    api: {
      ...actual.api,
      getRepositoryModel,
      getInitiativeProject,
      getLatestRepositoryScan,
    },
  };
});

const model: RepositoryModel = {
  repository: {
    id: "repo-1",
    name: "acme-platform",
    description: "",
    root_path: "/repo",
    remote_url: "https://github.com/acme/platform",
    project_ids: ["proj-1"],
    created_at: "2024-01-01T00:00:00Z",
    updated_at: "2024-01-01T00:00:00Z",
  },
  shape: "monorepo",
  components: [
    {
      id: "comp-api",
      repository_id: "repo-1",
      path: "services/api",
      name: { detected: "api" },
      role: { detected: "backend", confidence: "exact" },
      stack: { detected: { languages: [{ name: "Go", version: "1.23" }] } },
      commands: [{ purpose: "build", command: { detected: "go build ./..." } }],
      docs: {},
      gates: {},
      status: "active",
      manually_added: false,
      created_at: "2024-01-01T00:00:00Z",
      updated_at: "2024-01-01T00:00:00Z",
    },
    {
      id: "comp-web",
      repository_id: "repo-1",
      path: "apps/web",
      name: { detected: "web" },
      role: { detected: "frontend", confidence: "exact" },
      stack: { detected: {} },
      commands: [],
      docs: {},
      gates: {},
      status: "active",
      manually_added: false,
      created_at: "2024-01-01T00:00:00Z",
      updated_at: "2024-01-01T00:00:00Z",
    },
  ],
  checks: [],
  links: [],
  incoming_links: [],
  resources: [],
  linked_components: [],
  environments: [],
  notes: [],
  review: [],
};

const project: InitiativeProject = {
  id: "proj-1",
  name: "Acme Shop",
  description: "",
  created_at: "2024-01-01T00:00:00Z",
  updated_at: "2024-01-01T00:00:00Z",
};

const noScan: { scan: ProjectScan | null } = { scan: null };

function renderPage(initialTab: string) {
  return render(
    <I18nProvider>
      <MemoryRouter initialEntries={[`/repositories/repo-1?tab=${initialTab}`]}>
        <Routes>
          <Route path="/repositories/:repositoryId" element={<RepositoryPage />} />
        </Routes>
      </MemoryRouter>
    </I18nProvider>,
  );
}

describe("RepositoryPage", () => {
  beforeEach(() => {
    getRepositoryModel.mockReset().mockResolvedValue(model);
    getInitiativeProject.mockReset().mockResolvedValue(project);
    getLatestRepositoryScan.mockReset().mockResolvedValue(noScan);
  });

  it("renders every tab and shows the tab selected by ?tab=", async () => {
    renderPage("components");

    // The Components tab defaults to the first component and shows its detail.
    await waitFor(() => expect(screen.getByText("Identity")).toBeInTheDocument());

    for (const label of ["Overview", "Components", "Checks", "Links", "Deploy", "Knowledge", "Settings"]) {
      expect(screen.getByRole("tab", { name: new RegExp(label) })).toBeInTheDocument();
    }
  });

  it("switches tabs on click, updating which panel is shown", async () => {
    renderPage("components");
    await waitFor(() => expect(screen.getByText("Identity")).toBeInTheDocument());

    fireEvent.click(screen.getByRole("tab", { name: /^Links/ }));

    await waitFor(() => expect(screen.getByText("Outgoing · 0")).toBeInTheDocument());
    expect(screen.queryByText("Identity")).not.toBeInTheDocument();
  });
});
