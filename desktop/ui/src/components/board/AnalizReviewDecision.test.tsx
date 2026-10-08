import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it, vi, beforeEach } from "vitest";
import type { BoardTask, TaskDocument } from "@/api";
import { AnalizReviewDecision } from "@/components/board/AnalizReviewDecision";
import { HumanUatDecision } from "@/components/board/HumanUatDecision";
import { I18nProvider } from "@/hooks/useI18n";

const { updateRepositoryTask, createTaskComment, listTaskAnnotations } = vi.hoisted(() => ({
  updateRepositoryTask: vi.fn(),
  createTaskComment: vi.fn(),
  listTaskAnnotations: vi.fn(),
}));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return {
    ...actual,
    api: { ...actual.api, updateRepositoryTask, createTaskComment, listTaskAnnotations },
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

const htmlDoc: TaskDocument = {
  id: "doc-1",
  task_id: "task-1",
  title: "analiz: 2026-09-20 checkout",
  content: "<h1>Checkout</h1>",
  format: "html",
  position: 0,
  created_by_type: "agent",
  created_by_id: "agent-1",
  created_at: "2026-09-20T00:00:00Z",
  updated_at: "2026-09-20T00:00:00Z",
};

function Providers({ children }: { children: ReactNode }) {
  return (
    <MemoryRouter>
      <I18nProvider>{children}</I18nProvider>
    </MemoryRouter>
  );
}

function renderWithI18n(task: BoardTask, onUpdated: () => void, documents?: TaskDocument[]) {
  return render(
    <Providers>
      <AnalizReviewDecision task={task} repositoryId="repo-1" onUpdated={onUpdated} documents={documents} />
    </Providers>,
  );
}

describe("AnalizReviewDecision", () => {
  beforeEach(() => {
    updateRepositoryTask.mockReset();
    createTaskComment.mockReset();
    listTaskAnnotations.mockReset().mockResolvedValue({ annotations: [] });
  });

  it("renders nothing when the task is not in analiz_review", () => {
    renderWithI18n(makeTask({ column: "todo" }), vi.fn());
    expect(screen.queryByText("Approve")).not.toBeInTheDocument();
    expect(screen.queryByText("Decline")).not.toBeInTheDocument();
  });

  it("shows the approve/decline control for an analiz_review task, mutually exclusive with HumanUatDecision", () => {
    const task = makeTask();
    render(
      <Providers>
        <AnalizReviewDecision task={task} repositoryId="repo-1" onUpdated={vi.fn()} />
        <HumanUatDecision task={task} repositoryId="repo-1" onUpdated={vi.fn()} />
      </Providers>,
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

  it("offers no review link while the task has no documents", () => {
    renderWithI18n(makeTask(), vi.fn());
    expect(screen.queryByRole("link", { name: /Review analysis/ })).not.toBeInTheDocument();
    expect(listTaskAnnotations).not.toHaveBeenCalled();
  });

  it("links to the analysis review page once the task has a document", async () => {
    renderWithI18n(makeTask(), vi.fn(), [htmlDoc]);

    const link = await screen.findByRole("link", { name: "Review analysis" });
    expect(link).toHaveAttribute("href", "/repositories/repo-1/tasks/task-1/analysis");
    expect(listTaskAnnotations).toHaveBeenCalledWith("repo-1", "task-1");
  });

  it("counts the open comments on the review link", async () => {
    listTaskAnnotations.mockResolvedValue({
      annotations: [
        { id: "a1", status: "open" },
        { id: "a2", status: "open" },
        { id: "a3", status: "resolved" },
      ],
    });
    renderWithI18n(makeTask(), vi.fn(), [htmlDoc]);

    expect(await screen.findByRole("link", { name: "Review analysis (2 open comments)" })).toBeInTheDocument();
  });

  it("speaks of a design for a design task and says approving it approves the design system", async () => {
    listTaskAnnotations.mockResolvedValue({ annotations: [{ id: "a1", status: "open" }] });
    renderWithI18n(makeTask({ task_type: "design", key: "D-3" }), vi.fn(), [htmlDoc]);

    expect(await screen.findByRole("link", { name: "Review design (1 open comments)" })).toHaveAttribute(
      "href",
      "/repositories/repo-1/tasks/task-1/analysis",
    );
    expect(
      screen.getByText("Approving this design task approves the design system versions it proposes."),
    ).toBeInTheDocument();
  });
});
