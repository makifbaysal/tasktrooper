import "@testing-library/jest-dom/vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { Component, ComponentCheck, RepositoryModel } from "@/api";
import { ComponentsTab } from "@/components/projects/repository/ComponentsTab";
import { I18nProvider } from "@/hooks/useI18n";

// jsdom has no layout, so Radix Select's scroll-into-view-on-open crashes
// without this — every test file that opens a Select needs its own polyfill
// since vitest.setup.ts (shared, out of this feature's scope) doesn't have one.
if (!Element.prototype.scrollIntoView) {
  Element.prototype.scrollIntoView = () => {};
}

const { updateComponent, updateRepository, createWorkflowSetupTask } = vi.hoisted(() => ({
  updateComponent: vi.fn(),
  updateRepository: vi.fn(),
  createWorkflowSetupTask: vi.fn(),
}));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return {
    ...actual,
    api: {
      ...actual.api,
      updateComponent,
      updateRepository,
      createWorkflowSetupTask,
    },
  };
});

const component: Component = {
  id: "comp-api",
  repository_id: "repo-1",
  path: "services/api",
  name: { detected: "api" },
  role: { detected: "backend", confidence: "exact" },
  stack: { detected: { languages: [{ name: "Go", version: "1.23" }] } },
  commands: [],
  docs: {},
  gates: {},
  status: "active",
  manually_added: false,
  needs_review: false,
  created_at: "2024-01-01T00:00:00Z",
  updated_at: "2024-01-01T00:00:00Z",
};

const overriddenComponent: Component = {
  ...component,
  id: "comp-web",
  path: "apps/web",
  role: { detected: "frontend", override: "backend", confidence: "exact" },
};

const ciCheck: ComponentCheck = {
  id: "check-1",
  repository_id: "repo-1",
  component_id: "comp-api",
  source: "ci",
  workflow: "ci.yml",
  job_key: "test",
  purpose: { detected: "test" },
  local_commands: { detected: [] },
  gate: { detected: "required" },
  dispatchable: true,
  status: "active",
  missing: false,
  needs_review: false,
  created_at: "2024-01-01T00:00:00Z",
  updated_at: "2024-01-01T00:00:00Z",
};

function buildModel(overrides: Partial<RepositoryModel> = {}): RepositoryModel {
  return {
    repository: {
      id: "repo-1",
      name: "acme-platform",
      description: "",
      root_path: "/repo",
      created_at: "2024-01-01T00:00:00Z",
      updated_at: "2024-01-01T00:00:00Z",
    },
    shape: "monorepo",
    components: [component, overriddenComponent],
    checks: [],
    links: [],
    incoming_links: [],
    resources: [],
    linked_components: [],
    environments: [],
    notes: [],
    review: [],
    ...overrides,
  };
}

const model = buildModel();

function renderTab(selectedComponentId: string | null, modelOverride: RepositoryModel = model) {
  return render(
    <I18nProvider>
      <ComponentsTab
        model={modelOverride}
        repositoryId="repo-1"
        selectedComponentId={selectedComponentId}
        onSelectComponent={vi.fn()}
        onReload={vi.fn()}
      />
    </I18nProvider>,
  );
}

describe("ComponentsTab", () => {
  beforeEach(() => {
    updateComponent.mockReset().mockResolvedValue(component);
    updateRepository.mockReset().mockResolvedValue(model.repository);
    createWorkflowSetupTask.mockReset().mockResolvedValue({});
  });

  it("changing the role select calls updateComponent with the new role", async () => {
    renderTab("comp-api");
    await waitFor(() => expect(screen.getByText("Identity")).toBeInTheDocument());

    // The Identity card's role Select is the first combobox on the tab
    // (Commands' "add command" purpose picker is another, rendered after it).
    fireEvent.click(screen.getAllByRole("combobox")[0]);
    const option = await screen.findByText("Frontend");
    fireEvent.click(option);

    await waitFor(() => expect(updateComponent).toHaveBeenCalledWith("comp-api", { role: "frontend" }));
  });

  it("reverting an overridden role sends role: null", async () => {
    renderTab("comp-web");
    await waitFor(() => expect(screen.getByText("Identity")).toBeInTheDocument());

    fireEvent.click(screen.getByText("Revert to detected"));

    await waitFor(() => expect(updateComponent).toHaveBeenCalledWith("comp-web", { role: null }));
  });

  it("toggling human review calls updateRepository with require_human_review", async () => {
    renderTab("comp-api");
    await waitFor(() => expect(screen.getByText("Review")).toBeInTheDocument());

    // The review card renders first, above the selected component's own
    // GatesCard switches (coverage/mutation), so it is the first "switch" role.
    fireEvent.click(screen.getAllByRole("switch")[0]);

    await waitFor(() =>
      expect(updateRepository).toHaveBeenCalledWith("repo-1", {
        name: model.repository.name,
        description: model.repository.description,
        require_human_review: true,
      }),
    );
  });

  it("shows the no-CI-workflows notice when no check has source ci", async () => {
    renderTab("comp-api");
    await waitFor(() => expect(screen.getByText("No CI workflows")).toBeInTheDocument());
  });

  it("hides the no-CI-workflows notice once a ci check exists", async () => {
    renderTab("comp-api", buildModel({ checks: [ciCheck] }));
    await waitFor(() => expect(screen.getByText("Identity")).toBeInTheDocument());

    expect(screen.queryByText("No CI workflows")).not.toBeInTheDocument();
  });

  it("renders the review card in the empty-components branch too", async () => {
    renderTab(null, buildModel({ components: [] }));
    await waitFor(() => expect(screen.getByText("Review")).toBeInTheDocument());
    expect(screen.getByText("No components yet")).toBeInTheDocument();
  });
});
