import "@testing-library/jest-dom/vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { Component, RepositoryModel } from "@/api";
import { ComponentsTab } from "@/components/projects/repository/ComponentsTab";
import { I18nProvider } from "@/hooks/useI18n";

// jsdom has no layout, so Radix Select's scroll-into-view-on-open crashes
// without this — every test file that opens a Select needs its own polyfill
// since vitest.setup.ts (shared, out of this feature's scope) doesn't have one.
if (!Element.prototype.scrollIntoView) {
  Element.prototype.scrollIntoView = () => {};
}

const { updateComponent } = vi.hoisted(() => ({ updateComponent: vi.fn() }));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return {
    ...actual,
    api: {
      ...actual.api,
      updateComponent,
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
  created_at: "2024-01-01T00:00:00Z",
  updated_at: "2024-01-01T00:00:00Z",
};

const overriddenComponent: Component = {
  ...component,
  id: "comp-web",
  path: "apps/web",
  role: { detected: "frontend", override: "backend", confidence: "exact" },
};

const model: RepositoryModel = {
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
};

function renderTab(selectedComponentId: string | null) {
  return render(
    <I18nProvider>
      <ComponentsTab
        model={model}
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
});
