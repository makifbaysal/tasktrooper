import "@testing-library/jest-dom/vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { DesignSystemVersion, InitiativeProject, ProjectScan, RepositoryDesignSystemView, RepositoryModel } from "@/api";
import { I18nProvider } from "@/hooks/useI18n";
import { RepositoryPage } from "@/pages/RepositoryPage";

if (!Element.prototype.scrollIntoView) {
  Element.prototype.scrollIntoView = () => {};
}

const {
  getRepositoryModel,
  getInitiativeProject,
  getLatestRepositoryScan,
  getRepositoryDesignSystem,
  setRepositoryDesignBaseProject,
} = vi.hoisted(() => ({
  getRepositoryModel: vi.fn(),
  getInitiativeProject: vi.fn(),
  getLatestRepositoryScan: vi.fn(),
  getRepositoryDesignSystem: vi.fn(),
  setRepositoryDesignBaseProject: vi.fn(),
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
      getRepositoryDesignSystem,
      setRepositoryDesignBaseProject,
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
      needs_review: false,
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
      needs_review: false,
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

function designVersion(over: Partial<DesignSystemVersion>): DesignSystemVersion {
  return {
    id: "ds-1",
    scope: "project",
    version: 1,
    status: "approved",
    design_md: "",
    tokens: {},
    inventory_md: "",
    rationale: "",
    created_by: "designer",
    created_at: "2024-01-01T00:00:00Z",
    updated_at: "2024-01-01T00:00:00Z",
    ...over,
  };
}

const layer = designVersion({
  id: "layer-2",
  scope: "repository",
  repository_id: "repo-1",
  version: 2,
  rationale: "The admin app is denser.",
  tokens: { space: { $type: "dimension", row: { $value: "6px" } } },
  source_task_id: "task-5",
  source_task_key: "D-5",
  source_task_repository_id: "repo-9",
});

const ambiguousDesign: RepositoryDesignSystemView = {
  repository_id: "repo-1",
  repository_name: "acme-platform",
  repository_kind: "monorepo",
  effective: { repository_id: "repo-1", layer, tokens: layer.tokens, ambiguous: true },
  project_choices: [
    { project: { id: "proj-1", name: "Acme Shop" }, base_version: 3 },
    { project: { id: "proj-2", name: "Back Office" }, base_version: 1 },
  ],
  pending_layers: [],
  layer_versions: [layer],
  overrides: [],
  lint: [],
};

const chosenDesign: RepositoryDesignSystemView = {
  ...ambiguousDesign,
  base_project_id: "proj-1",
  effective: {
    repository_id: "repo-1",
    project: { id: "proj-1", name: "Acme Shop" },
    base: designVersion({ id: "base-3", project_id: "proj-1", version: 3 }),
    layer,
    tokens: layer.tokens,
  },
  overrides: ["space.row"],
};

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
    getRepositoryDesignSystem.mockReset().mockResolvedValue(ambiguousDesign);
    setRepositoryDesignBaseProject.mockReset().mockResolvedValue(chosenDesign);
  });

  it("renders every tab and shows the tab selected by ?tab=", async () => {
    renderPage("components");

    // The Components tab defaults to the first component and shows its detail.
    await waitFor(() => expect(screen.getByText("Identity")).toBeInTheDocument());

    for (const label of ["Overview", "Components", "Checks", "Links", "Deploy", "Settings"]) {
      expect(screen.getByRole("tab", { name: new RegExp(label) })).toBeInTheDocument();
    }
  });

  it("falls back to the overview tab for a stale ?tab=knowledge URL", async () => {
    renderPage("knowledge");

    await waitFor(() => expect(screen.getByRole("tab", { name: /^Overview/ })).toBeInTheDocument());
    expect(screen.getByRole("tab", { name: /^Overview/ })).toHaveAttribute("aria-selected", "true");
  });

  it("switches tabs on click, updating which panel is shown", async () => {
    renderPage("components");
    await waitFor(() => expect(screen.getByText("Identity")).toBeInTheDocument());

    fireEvent.click(screen.getByRole("tab", { name: /^Links/ }));

    await waitFor(() => expect(screen.getByText("Outgoing · 0")).toBeInTheDocument());
    expect(screen.queryByText("Identity")).not.toBeInTheDocument();
  });

  it("asks for a base project while it is ambiguous and saves the choice", async () => {
    renderPage("design");

    expect(await screen.findByText("Choose a base project")).toBeInTheDocument();
    expect(getRepositoryDesignSystem).toHaveBeenCalledWith("repo-1");
    expect(screen.getByText("No project base. This repository's layer is its whole design system.")).toBeInTheDocument();
    expect(screen.getByText("The admin app is denser.")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("combobox", { name: "Base project" }));
    fireEvent.click(await screen.findByRole("option", { name: "Acme Shop · base v3" }));

    await waitFor(() => expect(setRepositoryDesignBaseProject).toHaveBeenCalledWith("repo-1", "proj-1"));
    expect(await screen.findByText("Builds on Acme Shop · base v3")).toBeInTheDocument();
    expect(screen.queryByText("Choose a base project")).not.toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Open the project's design system" })).toHaveAttribute(
      "href",
      "/projects/proj-1?tab=design",
    );
    expect(screen.getByText("Overrides (1)")).toBeInTheDocument();
  });

  it("links a version's source task straight to its review page", async () => {
    renderPage("design");

    const links = await screen.findAllByRole("link", { name: "D-5" });
    expect(links.length).toBeGreaterThan(0);
    for (const link of links) expect(link).toHaveAttribute("href", "/repositories/repo-9/tasks/task-5/analysis");
  });
});
