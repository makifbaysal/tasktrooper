import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi, beforeEach } from "vitest";
import type { BoardTask } from "@/api";
import { AnalizReviewDecision } from "@/components/board/AnalizReviewDecision";
import { HumanUatDecision } from "@/components/board/HumanUatDecision";
import { I18nProvider } from "@/hooks/useI18n";

const { updateRepositoryTask, createTaskComment } = vi.hoisted(() => ({
  updateRepositoryTask: vi.fn(),
  createTaskComment: vi.fn(),
}));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return {
    ...actual,
    api: { ...actual.api, updateRepositoryTask, createTaskComment },
  };
});

function makeTask(overrides: Partial<BoardTask> = {}): BoardTask {
  return {
    id: "task-1",
    repository_id: "repo-1",
    key: "A-30",
    task_number: 30,
    title: "Some analysis",
    task_type: "analiz",
    description: "",
    technical_description: "",
    column: "analiz_review",
    position: 0,
    priority: "medium",
    created_by: "human",
    created_at: "2026-09-17T00:00:00Z",
    updated_at: "2026-09-17T00:00:00Z",
    ...overrides,
  };
}

function renderWithI18n(task: BoardTask, onUpdated: () => void) {
  return render(
    <I18nProvider>
      <AnalizReviewDecision task={task} repositoryId="repo-1" onUpdated={onUpdated} />
    </I18nProvider>,
  );
}

describe("AnalizReviewDecision", () => {
  beforeEach(() => {
    updateRepositoryTask.mockReset();
    createTaskComment.mockReset();
  });

  it("renders nothing when the task is not in analiz_review", () => {
    renderWithI18n(makeTask({ column: "todo" }), vi.fn());
    expect(screen.queryByText("Approve")).not.toBeInTheDocument();
    expect(screen.queryByText("Decline")).not.toBeInTheDocument();
  });

  it("shows the approve/decline control for an analiz_review task, mutually exclusive with HumanUatDecision", () => {
    const task = makeTask();
    render(
      <I18nProvider>
        <AnalizReviewDecision task={task} repositoryId="repo-1" onUpdated={vi.fn()} />
        <HumanUatDecision task={task} repositoryId="repo-1" onUpdated={vi.fn()} />
      </I18nProvider>,
    );
    expect(screen.getAllByText("Approve")).toHaveLength(1);
    expect(screen.getAllByText("Decline")).toHaveLength(1);
  });

  it("moves the task to done when approve is clicked", async () => {
    updateRepositoryTask.mockResolvedValue(undefined);
    const onUpdated = vi.fn();
    renderWithI18n(makeTask(), onUpdated);

    fireEvent.click(screen.getByText("Approve"));

    await waitFor(() => {
      expect(updateRepositoryTask).toHaveBeenCalledWith("repo-1", "task-1", { column: "done" });
    });
    expect(onUpdated).toHaveBeenCalled();
  });

  it("blocks decline submission until a reason is entered, then posts the reason and moves to need_revision", async () => {
    createTaskComment.mockResolvedValue(undefined);
    updateRepositoryTask.mockResolvedValue(undefined);
    const onUpdated = vi.fn();
    renderWithI18n(makeTask(), onUpdated);

    fireEvent.click(screen.getByText("Decline"));
    const submit = screen.getByRole("button", { name: /send back for revision/i });
    expect(submit).toBeDisabled();

    fireEvent.change(screen.getByLabelText("Reason"), { target: { value: "Missing risk analysis" } });
    expect(submit).toBeEnabled();

    fireEvent.click(submit);

    await waitFor(() => {
      expect(createTaskComment).toHaveBeenCalledWith("repo-1", "task-1", "Missing risk analysis");
      expect(updateRepositoryTask).toHaveBeenCalledWith("repo-1", "task-1", { column: "need_revision" });
    });
    expect(onUpdated).toHaveBeenCalled();
  });
});
