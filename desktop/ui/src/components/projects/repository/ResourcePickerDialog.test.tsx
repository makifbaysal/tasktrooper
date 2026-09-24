import "@testing-library/jest-dom/vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { WorkspaceResource } from "@/api";
import { ResourcePickerDialog } from "@/components/projects/repository/ResourcePickerDialog";
import { I18nProvider } from "@/hooks/useI18n";

if (!Element.prototype.scrollIntoView) {
  Element.prototype.scrollIntoView = () => {};
}

const { listWorkspaceResources } = vi.hoisted(() => ({
  listWorkspaceResources: vi.fn(),
}));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return {
    ...actual,
    api: {
      ...actual.api,
      listWorkspaceResources,
    },
  };
});

const inRepo: WorkspaceResource = {
  resource: {
    id: "res-repo",
    kind: "database",
    vendor: "Neon",
    name: "Database",
    identity_key: "k1",
    created_at: "2024-01-01T00:00:00Z",
    updated_at: "2024-01-01T00:00:00Z",
  },
  users: [{ repository_id: "repo-1", repository_name: "acme-platform", project_ids: [], component_id: "comp-a", component_path: "." }],
  projects: [],
  link_count: 1,
};

const inProject: WorkspaceResource = {
  resource: {
    id: "res-project",
    kind: "cache",
    vendor: "Upstash",
    name: "Redis cache",
    identity_key: "k2",
    created_at: "2024-01-01T00:00:00Z",
    updated_at: "2024-01-01T00:00:00Z",
  },
  users: [{ repository_id: "repo-2", repository_name: "billing-svc", project_ids: ["proj-1"], component_id: "comp-b", component_path: "." }],
  projects: [{ id: "proj-1", name: "Platform" }],
  link_count: 1,
};

const elsewhere: WorkspaceResource = {
  resource: {
    id: "res-other",
    kind: "database",
    vendor: "",
    name: "Analytics DB",
    identity_key: "k3",
    created_at: "2024-01-01T00:00:00Z",
    updated_at: "2024-01-01T00:00:00Z",
  },
  users: [{ repository_id: "repo-3", repository_name: "analytics", project_ids: ["proj-9"], component_id: "comp-c", component_path: "worker" }],
  projects: [{ id: "proj-9", name: "Data" }],
  link_count: 2,
};

function renderDialog(onPick = vi.fn()) {
  return render(
    <I18nProvider>
      <ResourcePickerDialog
        open
        onOpenChange={vi.fn()}
        title="Merge with an existing resource"
        submitLabel="Merge"
        repositoryId="repo-1"
        projectIds={["proj-1"]}
        onPick={onPick}
      />
    </I18nProvider>,
  );
}

describe("ResourcePickerDialog", () => {
  beforeEach(() => {
    listWorkspaceResources.mockReset().mockResolvedValue({ resources: [inRepo, inProject, elsewhere] });
  });

  it("sections resources by repository, project, and elsewhere", async () => {
    renderDialog();

    expect(await screen.findByText("Database")).toBeInTheDocument();
    expect(screen.getByText("In this repository")).toBeInTheDocument();
    expect(screen.getByText("Redis cache")).toBeInTheDocument();
    expect(screen.getByText("In this project")).toBeInTheDocument();
    expect(screen.getByText("Analytics DB")).toBeInTheDocument();
    expect(screen.getByText("In other projects")).toBeInTheDocument();
  });

  it("pre-filters by defaultKind", async () => {
    render(
      <I18nProvider>
        <ResourcePickerDialog
          open
          onOpenChange={vi.fn()}
          title="Merge with an existing resource"
          submitLabel="Merge"
          defaultKind="cache"
          repositoryId="repo-1"
          projectIds={["proj-1"]}
          onPick={vi.fn()}
        />
      </I18nProvider>,
    );

    await screen.findByText("Redis cache");
    expect(screen.queryByText("Database")).not.toBeInTheDocument();
    expect(screen.queryByText("Analytics DB")).not.toBeInTheDocument();
  });

  it("filters by search text across name, vendor, and repository", async () => {
    renderDialog();
    await screen.findByText("Database");

    fireEvent.change(screen.getByPlaceholderText("Search name, vendor, or repository…"), { target: { value: "neon" } });

    expect(screen.getByText("Database")).toBeInTheDocument();
    expect(screen.queryByText("Redis cache")).not.toBeInTheDocument();
    expect(screen.queryByText("Analytics DB")).not.toBeInTheDocument();
  });

  it("shows an empty state when nothing matches", async () => {
    renderDialog();
    await screen.findByText("Database");

    fireEvent.change(screen.getByPlaceholderText("Search name, vendor, or repository…"), { target: { value: "nothing-matches-this" } });

    expect(await screen.findByText("No matching resources")).toBeInTheDocument();
  });

  it("submits the picked resource", async () => {
    const onPick = vi.fn().mockResolvedValue(undefined);
    renderDialog(onPick);
    fireEvent.click(await screen.findByText("Database"));

    fireEvent.click(screen.getByText("Merge"));

    await waitFor(() => expect(onPick).toHaveBeenCalledWith(inRepo));
  });

  it("excludes the given resource id", async () => {
    render(
      <I18nProvider>
        <ResourcePickerDialog
          open
          onOpenChange={vi.fn()}
          title="Merge with an existing resource"
          submitLabel="Merge"
          excludeResourceId="res-repo"
          repositoryId="repo-1"
          projectIds={["proj-1"]}
          onPick={vi.fn()}
        />
      </I18nProvider>,
    );

    await screen.findByText("Redis cache");
    expect(screen.queryByText("Database")).not.toBeInTheDocument();
  });
});
