import "@testing-library/jest-dom/vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { Component, ComponentLink, ProjectsOverview, RepositoryModel } from "@/api";
import { LinksTab } from "@/components/projects/repository/LinksTab";
import { I18nProvider } from "@/hooks/useI18n";

if (!Element.prototype.scrollIntoView) {
  Element.prototype.scrollIntoView = () => {};
}

const { updateLink, getProjectsOverview } = vi.hoisted(() => ({
  updateLink: vi.fn(),
  getProjectsOverview: vi.fn(),
}));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return {
    ...actual,
    api: {
      ...actual.api,
      updateLink,
      getProjectsOverview,
    },
  };
});

const componentA: Component = {
  id: "comp-api",
  repository_id: "repo-1",
  path: "services/api",
  name: { detected: "api" },
  role: { detected: "backend" },
  stack: { detected: {} },
  commands: [],
  docs: {},
  gates: {},
  status: "active",
  manually_added: false,
  needs_review: false,
  created_at: "2024-01-01T00:00:00Z",
  updated_at: "2024-01-01T00:00:00Z",
};

const suggestedLink: ComponentLink = {
  id: "link-1",
  repository_id: "repo-1",
  from_component_id: "comp-api",
  protocol: "http",
  confidence: "medium",
  hint: "billing-svc",
  status: "suggested",
  source: "scan",
  auto_confirmed: false,
  missing: false,
  created_at: "2024-01-01T00:00:00Z",
  updated_at: "2024-01-01T00:00:00Z",
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
  shape: "single",
  components: [componentA],
  checks: [],
  links: [suggestedLink],
  incoming_links: [],
  resources: [],
  linked_components: [],
  environments: [],
  notes: [],
  review: [],
};

const overview: ProjectsOverview = {
  projects: [],
  unassigned: [
    {
      id: "repo-2",
      name: "billing-svc",
      description: "",
      shape: "single",
      components: [{ id: "comp-billing", path: ".", name: "billing-svc", role: "backend", stack_summary: "", checks: 0, required_checks: 0 }],
      review_count: 0,
      environments: [],
      updated_at: "2024-01-01T00:00:00Z",
    },
  ],
};

function renderTab() {
  return render(
    <I18nProvider>
      <LinksTab model={model} selectedComponentId="comp-api" onSelectComponent={vi.fn()} onReload={vi.fn()} />
    </I18nProvider>,
  );
}

describe("LinksTab", () => {
  beforeEach(() => {
    updateLink.mockReset().mockResolvedValue(suggestedLink);
    getProjectsOverview.mockReset().mockResolvedValue(overview);
  });

  it("dismissing the selected link calls updateLink with status: dismissed", async () => {
    renderTab();
    fireEvent.click(screen.getByText("billing-svc"));

    fireEvent.click(await screen.findByText("Dismiss"));

    await waitFor(() => expect(updateLink).toHaveBeenCalledWith("link-1", { status: "dismissed" }));
  });

  it("retargeting through the component picker sends to_component_id", async () => {
    renderTab();
    fireEvent.click(screen.getByText("billing-svc"));

    fireEvent.click(await screen.findByText("Point to a component…"));
    fireEvent.click(await screen.findByText(/billing-svc\/$/));

    await waitFor(() => expect(updateLink).toHaveBeenCalledWith("link-1", { to_component_id: "comp-billing" }));
  });
});
