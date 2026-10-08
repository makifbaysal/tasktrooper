import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes, useLocation } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { BoardTask } from "@/api";
import { DesignTaskLink } from "@/components/projects/designsystem/DesignTaskLink";

const { lookupTask } = vi.hoisted(() => ({ lookupTask: vi.fn() }));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return { ...actual, api: { ...actual.api, lookupTask } };
});

function Where() {
  const location = useLocation();
  return <p>at {location.pathname + location.search}</p>;
}

function renderLink(repositoryId?: string) {
  return render(
    <MemoryRouter initialEntries={["/projects/proj-1"]}>
      <Routes>
        <Route path="/projects/:projectId" element={<DesignTaskLink taskId="task-5" taskKey="D-5" repositoryId={repositoryId} />} />
        <Route path="*" element={<Where />} />
      </Routes>
    </MemoryRouter>,
  );
}

describe("DesignTaskLink", () => {
  beforeEach(() => {
    lookupTask.mockReset();
  });

  it("links straight to the review page when the repository is known", () => {
    renderLink("repo-9");

    const link = screen.getByRole("link", { name: "D-5" });
    expect(link).toHaveAttribute("href", "/repositories/repo-9/tasks/task-5/analysis");
    fireEvent.click(link);
    expect(screen.getByText("at /repositories/repo-9/tasks/task-5/analysis")).toBeInTheDocument();
    expect(lookupTask).not.toHaveBeenCalled();
  });

  it("without a repository, points at the board and resolves the review page on click", async () => {
    lookupTask.mockResolvedValue({ id: "task-5", repository_id: "repo-3" } as BoardTask);
    renderLink();

    const link = screen.getByRole("link", { name: "D-5" });
    expect(link).toHaveAttribute("href", "/board?task=task-5");
    fireEvent.click(link);

    expect(await screen.findByText("at /repositories/repo-3/tasks/task-5/analysis")).toBeInTheDocument();
    expect(lookupTask).toHaveBeenCalledWith("D-5");
  });

  it("falls back to the board when the lookup fails", async () => {
    lookupTask.mockRejectedValue(new Error("not found"));
    renderLink();

    fireEvent.click(screen.getByRole("link", { name: "D-5" }));

    expect(await screen.findByText("at /board?task=task-5")).toBeInTheDocument();
  });
});
