import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";

const { navigate, listInitiativeProjects, listRepositories, githubStatus, createNewRepository, getLatestRepositoryScan } =
  vi.hoisted(() => ({
    navigate: vi.fn(),
    listInitiativeProjects: vi.fn(),
    listRepositories: vi.fn(),
    githubStatus: vi.fn(),
    createNewRepository: vi.fn(),
    getLatestRepositoryScan: vi.fn(),
  }));

vi.mock("react-router-dom", async () => {
  const actual = await vi.importActual<typeof import("react-router-dom")>("react-router-dom");
  return { ...actual, useNavigate: () => navigate };
});

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return {
    ...actual,
    api: {
      ...actual.api,
      listInitiativeProjects,
      listRepositories,
      githubStatus,
      createNewRepository,
      getLatestRepositoryScan,
    },
  };
});

import { I18nProvider } from "@/hooks/useI18n";
import { AddRepositoryPage } from "@/pages/AddRepositoryPage";

function renderPage() {
  render(
    <I18nProvider>
      <MemoryRouter initialEntries={["/projects/new?project=p1"]}>
        <AddRepositoryPage />
      </MemoryRouter>
    </I18nProvider>,
  );
}

describe("AddRepositoryPage — new repository", () => {
  beforeEach(() => {
    navigate.mockReset();
    listInitiativeProjects.mockReset().mockResolvedValue({ projects: [{ id: "p1", name: "Acme Shop" }] });
    listRepositories.mockReset().mockResolvedValue({ repositories: [] });
    githubStatus.mockReset().mockResolvedValue({ connected: false });
    getLatestRepositoryScan.mockReset();
    createNewRepository.mockReset().mockResolvedValue({
      repository: { id: "repo-new", name: "acme-web", description: "", root_path: "/code/acme-web", created_at: "", updated_at: "" },
      component_id: "comp-1",
      task: { id: "task-1", key: "TT-12", title: "Set up acme-web" },
    });
  });

  it("skips Scan and Review and lands on Done with the setup task", async () => {
    renderPage();
    await waitFor(() => expect(listInitiativeProjects).toHaveBeenCalled());

    fireEvent.click(screen.getByRole("radio", { name: /Create a new repository/ }));
    const stepper = screen.getByRole("navigation", { name: "Add repository steps" });
    expect(within(stepper).queryByText("Scan")).not.toBeInTheDocument();
    expect(within(stepper).queryByText("Review")).not.toBeInTheDocument();
    expect(within(stepper).getByText("Done")).toBeInTheDocument();

    fireEvent.change(screen.getByLabelText("Repository name"), { target: { value: "acme-web" } });
    fireEvent.click(screen.getByRole("button", { name: "Create" }));

    expect(await screen.findByText("acme-web created in Acme Shop")).toBeInTheDocument();
    expect(createNewRepository).toHaveBeenCalledWith({
      name: "acme-web",
      role: "frontend",
      scaffold: true,
      docs: ["coding_standards", "test_standards", "architecture", "local_run"],
      project_ids: ["p1"],
    });
    expect(screen.getByText("Setup task added to the board: TT-12 — Set up acme-web")).toBeInTheDocument();
    expect(screen.queryByText(/^Scanning/)).not.toBeInTheDocument();
    expect(screen.queryByText("A few things I wasn't sure about")).not.toBeInTheDocument();
    expect(getLatestRepositoryScan).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole("button", { name: "Open task" }));
    expect(navigate).toHaveBeenCalledWith("/board?task=task-1");
    fireEvent.click(screen.getByRole("button", { name: "Open repository" }));
    expect(navigate).toHaveBeenCalledWith("/repositories/repo-new?project=p1&setup=1");
  });

  it("says so when the server opened no setup task", async () => {
    createNewRepository.mockResolvedValueOnce({
      repository: { id: "repo-new", name: "acme-web", description: "", root_path: "/code/acme-web", created_at: "", updated_at: "" },
      component_id: "comp-1",
      task: null,
    });
    renderPage();
    await waitFor(() => expect(listInitiativeProjects).toHaveBeenCalled());

    fireEvent.click(screen.getByRole("radio", { name: /Create a new repository/ }));
    fireEvent.change(screen.getByLabelText("Repository name"), { target: { value: "acme-web" } });
    fireEvent.click(screen.getByRole("button", { name: "Create" }));

    expect(await screen.findByText("No setup task was opened")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Open task" })).not.toBeInTheDocument();
  });
});
