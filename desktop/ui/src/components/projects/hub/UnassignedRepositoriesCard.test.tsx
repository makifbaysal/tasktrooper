import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { RepositorySummary } from "@/api";
import { UnassignedRepositoriesCard } from "@/components/projects/hub/UnassignedRepositoriesCard";
import { I18nProvider } from "@/hooks/useI18n";

const { deleteRepository, setRepositoryProjects } = vi.hoisted(() => ({
  deleteRepository: vi.fn(),
  setRepositoryProjects: vi.fn(),
}));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return { ...actual, api: { ...actual.api, deleteRepository, setRepositoryProjects } };
});

const stale: RepositorySummary = {
  id: "repo-stale",
  name: "old-repo",
  description: "",
  project_ids: [],
  shape: "single",
  components: [],
  review_count: 0,
  environments: [],
  updated_at: "2026-01-01T00:00:00Z",
};

function renderCard(onChanged = vi.fn()) {
  render(
    <I18nProvider>
      <UnassignedRepositoriesCard repositories={[stale]} projects={[{ id: "proj-1", name: "Acme" }]} onChanged={onChanged} />
    </I18nProvider>,
  );
  return onChanged;
}

describe("UnassignedRepositoriesCard", () => {
  beforeEach(() => {
    deleteRepository.mockReset().mockResolvedValue(undefined);
    setRepositoryProjects.mockReset();
  });

  it("removes a repository only after the confirmation", async () => {
    const onChanged = renderCard();

    fireEvent.click(screen.getByRole("button", { name: "Remove repository" }));
    expect(await screen.findByText("Remove old-repo?")).toBeInTheDocument();
    expect(deleteRepository).not.toHaveBeenCalled();

    const confirm = screen.getAllByRole("button", { name: "Remove repository" }).at(-1)!;
    fireEvent.click(confirm);

    await waitFor(() => expect(deleteRepository).toHaveBeenCalledWith("repo-stale"));
    await waitFor(() => expect(onChanged).toHaveBeenCalledTimes(1));
  });

  it("leaves the repository alone when the confirmation is cancelled", async () => {
    const onChanged = renderCard();

    fireEvent.click(screen.getByRole("button", { name: "Remove repository" }));
    fireEvent.click(await screen.findByRole("button", { name: "Cancel" }));

    await waitFor(() => expect(screen.queryByText("Remove old-repo?")).not.toBeInTheDocument());
    expect(deleteRepository).not.toHaveBeenCalled();
    expect(onChanged).not.toHaveBeenCalled();
  });

  it("keeps the list as it is when the removal fails", async () => {
    deleteRepository.mockRejectedValue(new Error("boom"));
    const onChanged = renderCard();

    fireEvent.click(screen.getByRole("button", { name: "Remove repository" }));
    await screen.findByText("Remove old-repo?");
    fireEvent.click(screen.getAllByRole("button", { name: "Remove repository" }).at(-1)!);

    await waitFor(() => expect(deleteRepository).toHaveBeenCalledWith("repo-stale"));
    expect(onChanged).not.toHaveBeenCalled();
  });
});
