import "@testing-library/jest-dom/vitest";
import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { ApiError, type RepositoryModel, type WorkspaceIndex } from "@/api";
import { SettingsTab } from "@/components/projects/repository/SettingsTab";
import { I18nProvider } from "@/hooks/useI18n";

const { getRepositoryIndexStatus, listInitiativeProjects } = vi.hoisted(() => ({
  getRepositoryIndexStatus: vi.fn(),
  listInitiativeProjects: vi.fn(),
}));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return {
    ...actual,
    api: { ...actual.api, getRepositoryIndexStatus, listInitiativeProjects },
  };
});

const model = {
  repository: { id: "repo-1", name: "demo", description: "", project_ids: [] },
} as unknown as RepositoryModel;

function renderTab() {
  render(
    <I18nProvider>
      <MemoryRouter>
        <SettingsTab model={model} repositoryId="repo-1" onReload={() => undefined} />
      </MemoryRouter>
    </I18nProvider>,
  );
}

const onComputer = "Built and kept on your computer, with its own embedding model.";

describe("SettingsTab code index", () => {
  beforeEach(() => {
    getRepositoryIndexStatus.mockReset();
    listInitiativeProjects.mockReset();
    listInitiativeProjects.mockResolvedValue({ projects: [] });
  });

  it("says why the index cannot be read instead of 'not indexed yet'", async () => {
    getRepositoryIndexStatus.mockRejectedValue(
      new ApiError("Your computer is not connected. Open the desktop app to see its code index.", 409, "runner_not_attached"),
    );

    renderTab();

    expect(
      await screen.findByText("Your computer is not connected. Open the desktop app to see its code index."),
    ).toBeInTheDocument();
    expect(screen.queryByText("Not indexed yet")).not.toBeInTheDocument();
  });

  it("tells a computer that has not built the index yet where it will live", async () => {
    getRepositoryIndexStatus.mockRejectedValue(
      new ApiError("this repository is not indexed on your computer yet", 404, "index_not_on_computer"),
    );

    renderTab();

    expect(await screen.findByText(onComputer)).toBeInTheDocument();
    expect(screen.getByText("Not indexed yet")).toBeInTheDocument();
  });

  it("shows an index on the viewer's computer with its counts and its failure", async () => {
    getRepositoryIndexStatus.mockResolvedValue({
      id: "",
      root_path: "",
      status: "failed",
      file_count: 40,
      chunk_count: 90,
      symbol_count: 12,
      files_total: 212,
      files_processed: 40,
      error: "indexing stopped after 40/212 files; the next run continues from here",
      location: "computer",
    } satisfies WorkspaceIndex);

    renderTab();

    expect(await screen.findByText(onComputer)).toBeInTheDocument();
    expect(screen.getByText("40")).toBeInTheDocument();
    expect(
      screen.getByText("indexing stopped after 40/212 files; the next run continues from here"),
    ).toBeInTheDocument();
  });

  it("says nothing about a computer for an index on this server", async () => {
    getRepositoryIndexStatus.mockRejectedValue(new ApiError("get index: no rows in result set", 404, "not_found"));

    renderTab();

    expect(await screen.findByText("Not indexed yet")).toBeInTheDocument();
    expect(screen.queryByText(onComputer)).not.toBeInTheDocument();
  });
});
