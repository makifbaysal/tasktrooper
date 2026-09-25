import "@testing-library/jest-dom/vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { Component, ComponentLink, ProjectsOverview, RepositoryModel, SystemResource, WorkspaceResource } from "@/api";
import { LinksTab } from "@/components/projects/repository/LinksTab";
import { I18nProvider } from "@/hooks/useI18n";

if (!Element.prototype.scrollIntoView) {
  Element.prototype.scrollIntoView = () => {};
}

const { updateLink, getProjectsOverview, listWorkspaceResources, mergeResource } = vi.hoisted(() => ({
  updateLink: vi.fn(),
  getProjectsOverview: vi.fn(),
  listWorkspaceResources: vi.fn(),
  mergeResource: vi.fn(),
}));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return {
    ...actual,
    api: {
      ...actual.api,
      updateLink,
      getProjectsOverview,
      listWorkspaceResources,
      mergeResource,
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
    listWorkspaceResources.mockReset().mockResolvedValue({ resources: [] });
    mergeResource.mockReset().mockResolvedValue({});
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

  it("retargeting through the resource picker sends to_resource_id", async () => {
    listWorkspaceResources.mockResolvedValue({ resources: [makeWorkspaceResource(resourcePg)] });
    renderTab();
    fireEvent.click(screen.getByText("billing-svc"));

    fireEvent.click(await screen.findByText("Link to an existing resource…"));
    fireEvent.click(await screen.findByText("PostgreSQL"));
    fireEvent.click(await screen.findByText("Link"));

    await waitFor(() => expect(updateLink).toHaveBeenCalledWith("link-1", { to_resource_id: "res-pg" }));
  });
});

const resourceDb: SystemResource = {
  id: "res-db",
  kind: "database",
  vendor: "",
  name: "Database",
  identity_key: "database:res-db",
  created_at: "2024-01-01T00:00:00Z",
  updated_at: "2024-01-01T00:00:00Z",
};

const resourcePg: SystemResource = {
  id: "res-pg",
  kind: "database",
  vendor: "",
  name: "PostgreSQL",
  identity_key: "database:res-pg",
  created_at: "2024-01-01T00:00:00Z",
  updated_at: "2024-01-01T00:00:00Z",
};

function makeWorkspaceResource(resource: SystemResource, overrides: Partial<WorkspaceResource> = {}): WorkspaceResource {
  return {
    resource,
    users: [{ repository_id: "repo-1", repository_name: "acme-platform", project_ids: [], component_id: "comp-api", component_path: "services/api" }],
    projects: [],
    link_count: 1,
    ...overrides,
  };
}

describe("LinksTab grouping", () => {
  beforeEach(() => {
    updateLink.mockReset().mockResolvedValue(suggestedLink);
    getProjectsOverview.mockReset().mockResolvedValue(overview);
    listWorkspaceResources.mockReset().mockResolvedValue({ resources: [] });
    mergeResource.mockReset().mockResolvedValue({});
  });

  it("renders one row for two links pointing at the same resource", () => {
    const groupedModel: RepositoryModel = {
      ...model,
      links: [
        { ...suggestedLink, id: "link-db1", to_resource_id: "res-db", protocol: "sql", status: "confirmed", hint: undefined },
        { ...suggestedLink, id: "link-db2", to_resource_id: "res-db", protocol: "sql", status: "confirmed", hint: undefined, env_vars: ["DATABASE_URL"] },
      ],
      resources: [resourceDb],
    };
    render(
      <I18nProvider>
        <LinksTab model={groupedModel} selectedComponentId="comp-api" onSelectComponent={vi.fn()} onReload={vi.fn()} />
      </I18nProvider>,
    );

    expect(screen.getAllByText("Database")).toHaveLength(1);
    expect(screen.getByText("2 signals")).toBeInTheDocument();
  });

  it("merging from the resource panel calls mergeResource with the current and picked resource ids", async () => {
    listWorkspaceResources.mockResolvedValue({
      resources: [makeWorkspaceResource(resourceDb, { link_count: 1 }), makeWorkspaceResource(resourcePg, { link_count: 1 })],
    });
    const groupedModel: RepositoryModel = {
      ...model,
      links: [{ ...suggestedLink, id: "link-db1", to_resource_id: "res-db", protocol: "sql", status: "confirmed", hint: undefined }],
      resources: [resourceDb],
    };
    render(
      <I18nProvider>
        <LinksTab model={groupedModel} selectedComponentId="comp-api" onSelectComponent={vi.fn()} onReload={vi.fn()} />
      </I18nProvider>,
    );

    fireEvent.click(screen.getByText("Database"));
    fireEvent.click(await screen.findByText("Merge with an existing resource…"));
    fireEvent.click(await screen.findByText("PostgreSQL"));
    fireEvent.click(await screen.findByText("Merge"));

    await waitFor(() => expect(mergeResource).toHaveBeenCalledWith("res-db", "res-pg"));
  });
});

describe("LinksTab duplicate resource hint", () => {
  beforeEach(() => {
    updateLink.mockReset().mockResolvedValue(suggestedLink);
    getProjectsOverview.mockReset().mockResolvedValue(overview);
    listWorkspaceResources.mockReset().mockResolvedValue({ resources: [] });
    mergeResource.mockReset().mockResolvedValue({});
  });

  it("shows a notice for Database + PostgreSQL and merges into the chosen resource", async () => {
    const duplicateModel: RepositoryModel = {
      ...model,
      links: [
        { ...suggestedLink, id: "link-db", to_resource_id: "res-db", status: "confirmed", hint: undefined },
        { ...suggestedLink, id: "link-pg", to_resource_id: "res-pg", status: "confirmed", hint: undefined },
      ],
      resources: [resourceDb, resourcePg],
    };
    render(
      <I18nProvider>
        <LinksTab model={duplicateModel} selectedComponentId="comp-api" onSelectComponent={vi.fn()} onReload={vi.fn()} />
      </I18nProvider>,
    );

    expect(await screen.findByText(/connects to 2 separate Database resources: Database, PostgreSQL/)).toBeInTheDocument();

    fireEvent.click(screen.getByText("Merge…"));
    fireEvent.click(await screen.findByLabelText("PostgreSQL"));
    fireEvent.click(await screen.findByText("Merge"));

    await waitFor(() => expect(mergeResource).toHaveBeenCalledWith("res-db", "res-pg"));
  });
});
