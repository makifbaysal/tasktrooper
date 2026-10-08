import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter, Route, Routes, useLocation } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { BoardTask, TaskAnnotation, TaskDocument, TaskQuestion } from "@/api";
import { I18nProvider } from "@/hooks/useI18n";
import { ThemeProvider } from "@/hooks/useTheme";
import { AnalysisReviewPage } from "@/pages/AnalysisReviewPage";

const {
  listRepositoryTasks,
  listTaskDocuments,
  listTaskAnnotations,
  submitTaskAnnotations,
  updateRepositoryTask,
  listTaskQuestions,
  answerTaskQuestion,
  submitTaskQuestions,
  listTaskComments,
  createTaskComment,
} = vi.hoisted(() => ({
  listRepositoryTasks: vi.fn(),
  listTaskDocuments: vi.fn(),
  listTaskAnnotations: vi.fn(),
  submitTaskAnnotations: vi.fn(),
  updateRepositoryTask: vi.fn(),
  listTaskQuestions: vi.fn(),
  answerTaskQuestion: vi.fn(),
  submitTaskQuestions: vi.fn(),
  listTaskComments: vi.fn(),
  createTaskComment: vi.fn(),
}));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return {
    ...actual,
    api: {
      ...actual.api,
      listRepositoryTasks,
      listTaskDocuments,
      listTaskAnnotations,
      submitTaskAnnotations,
      updateRepositoryTask,
      listTaskQuestions,
      answerTaskQuestion,
      submitTaskQuestions,
      listTaskComments,
      createTaskComment,
    },
  };
});

function makeTask(overrides: Partial<BoardTask> = {}): BoardTask {
  return {
    id: "task-1",
    repository_id: "repo-1",
    key: "A-30",
    task_number: 30,
    title: "Checkout caching",
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

function makeDoc(overrides: Partial<TaskDocument> = {}): TaskDocument {
  return {
    id: "doc-html",
    task_id: "task-1",
    title: "analiz: 2026-09-20 checkout caching",
    content: "<html><body><h1>Checkout caching</h1><p>Use the cache.</p></body></html>",
    format: "html",
    position: 1,
    created_by_type: "agent",
    created_by_id: "agent-1",
    created_at: "2026-09-20T00:00:00Z",
    updated_at: "2026-09-20T00:00:00Z",
    ...overrides,
  };
}

function makeAnnotation(overrides: Partial<TaskAnnotation> = {}): TaskAnnotation {
  return {
    id: "a1",
    task_id: "task-1",
    document_id: "doc-html",
    quote: "the cache",
    prefix: "Use ",
    suffix: ".",
    body: "Which cache?",
    status: "open",
    reply: "",
    created_by_type: "user",
    created_at: "2026-09-20T10:00:00Z",
    updated_at: "2026-09-20T10:00:00Z",
    ...overrides,
  };
}

function makeQuestion(overrides: Partial<TaskQuestion> = {}): TaskQuestion {
  return {
    id: "q1",
    task_id: "task-1",
    key: "Q1",
    prompt: "Which cache backend?",
    kind: "technical",
    blocking: true,
    recommended_answer: "",
    status: "open",
    answer: "",
    created_at: "2026-09-20T00:00:00Z",
    updated_at: "2026-09-20T00:00:00Z",
    ...overrides,
  };
}

function BoardProbe() {
  const location = useLocation();
  return <p>board at {location.pathname + location.search}</p>;
}

function renderPage(path = "/repositories/repo-1/tasks/task-1/analysis") {
  return render(
    <ThemeProvider>
      <I18nProvider>
        <MemoryRouter initialEntries={[path]}>
          <Routes>
            <Route path="/repositories/:repositoryId/tasks/:taskId/analysis" element={<AnalysisReviewPage />} />
            <Route path="/board" element={<BoardProbe />} />
          </Routes>
        </MemoryRouter>
      </I18nProvider>
    </ThemeProvider>,
  );
}

async function srcdoc(): Promise<string> {
  return waitFor(() => {
    const value = document.querySelector("iframe")?.getAttribute("srcdoc");
    if (!value) throw new Error("no frame yet");
    return value;
  });
}

describe("AnalysisReviewPage", () => {
  beforeEach(() => {
    listRepositoryTasks.mockReset().mockResolvedValue({ tasks: [makeTask()] });
    listTaskDocuments.mockReset().mockResolvedValue({
      documents: [
        makeDoc({ id: "doc-spec", title: "spec: old notes", content: "# Old spec", format: undefined, position: 0 }),
        makeDoc(),
      ],
    });
    listTaskAnnotations.mockReset().mockResolvedValue({
      annotations: [
        makeAnnotation(),
        makeAnnotation({ id: "a2", quote: "Checkout caching", body: "Scope?" }),
        makeAnnotation({ id: "a3", quote: "Use", status: "resolved", reply: "Clarified." }),
      ],
    });
    submitTaskAnnotations.mockReset();
    updateRepositoryTask.mockReset();
    listTaskQuestions.mockReset().mockResolvedValue({ questions: [] });
    answerTaskQuestion.mockReset();
    submitTaskQuestions.mockReset();
    listTaskComments.mockReset().mockResolvedValue({ comments: [] });
    createTaskComment.mockReset();
  });

  it("opens the analiz HTML document in the sandboxed frame with its comments", async () => {
    renderPage();

    expect(await screen.findByRole("heading", { name: "Checkout caching" })).toBeInTheDocument();
    expect(screen.getByText("A-30")).toBeInTheDocument();
    expect(await srcdoc()).toContain("<p>Use the cache.</p>");
    expect(document.querySelector("iframe")?.getAttribute("sandbox")).toBe("allow-scripts");
    expect(screen.getByText("2 open · 0 sent · 1 resolved")).toBeInTheDocument();
    expect(screen.getByText("Clarified.")).toBeInTheDocument();
    expect(listTaskAnnotations).toHaveBeenCalledWith("repo-1", "task-1");
  });

  it("opens the document the URL names", async () => {
    renderPage("/repositories/repo-1/tasks/task-1/analysis?doc=doc-spec");
    expect(await srcdoc()).toContain("<h1>Old spec</h1>");
  });

  it("sends every open comment with the note and goes back to the task on the board", async () => {
    submitTaskAnnotations.mockResolvedValue({ submitted: 2, task: makeTask({ column: "need_revision" }) });
    renderPage();

    fireEvent.click(await screen.findByRole("button", { name: "Send comments (2)" }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.change(within(dialog).getByLabelText("Note (optional)"), { target: { value: " Keep it short " } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Send comments" }));

    await waitFor(() => expect(submitTaskAnnotations).toHaveBeenCalledWith("repo-1", "task-1", "Keep it short"));
    expect(await screen.findByText("board at /board?task=task-1")).toBeInTheDocument();
  });

  it("keeps the dialog open when the server refuses the submit", async () => {
    submitTaskAnnotations.mockRejectedValue(new Error("task is not in analiz_review"));
    renderPage();

    fireEvent.click(await screen.findByRole("button", { name: "Send comments (2)" }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.click(within(dialog).getByRole("button", { name: "Send comments" }));

    await waitFor(() => expect(submitTaskAnnotations).toHaveBeenCalledWith("repo-1", "task-1", undefined));
    expect(screen.getByRole("dialog")).toBeInTheDocument();
    expect(screen.queryByText(/board at/)).not.toBeInTheDocument();
  });

  it("asks before approving over unsent comments, then moves the task to done", async () => {
    updateRepositoryTask.mockResolvedValue(makeTask({ column: "done" }));
    renderPage();

    fireEvent.click(await screen.findByRole("button", { name: "Approve" }));
    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByText("Approve with unsent comments?")).toBeInTheDocument();
    fireEvent.click(within(dialog).getByRole("button", { name: "Approve" }));

    await waitFor(() => expect(updateRepositoryTask).toHaveBeenCalledWith("repo-1", "task-1", { column: "done" }));
    expect(await screen.findByText("board at /board?task=task-1")).toBeInTheDocument();
  });

  it("shows the revision banner and holds the submit while the agent revises", async () => {
    listRepositoryTasks.mockResolvedValue({ tasks: [makeTask({ column: "need_revision" })] });
    listTaskAnnotations.mockResolvedValue({ annotations: [makeAnnotation({ status: "submitted" })] });
    renderPage();

    expect(await screen.findByText("The agent is revising the analysis…")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Send comments (0)" })).toBeDisabled();
    expect(screen.queryByRole("button", { name: "Approve" })).not.toBeInTheDocument();
    expect(screen.getByText(/Commenting is paused/)).toBeInTheDocument();
  });

  it("says so when the task no longer exists", async () => {
    listRepositoryTasks.mockResolvedValue({ tasks: [] });
    renderPage();
    expect(await screen.findByText("Task not found")).toBeInTheDocument();
  });

  it("disables Send answers while a blocking question is unanswered, naming it in the tooltip", async () => {
    listRepositoryTasks.mockResolvedValue({
      tasks: [makeTask({ column: "blocked", blocked_resource: "analysis_questions" })],
    });
    listTaskAnnotations.mockResolvedValue({ annotations: [] });
    listTaskQuestions.mockResolvedValue({ questions: [makeQuestion({ blocking: true, status: "open" })] });
    renderPage();

    const sendAnswers = await screen.findByRole("button", { name: "Send answers" });
    expect(sendAnswers).toBeDisabled();
    expect(sendAnswers.getAttribute("title")).toContain("Q1");
  });

  it("sends the answers once every blocking question has one, and returns to the board", async () => {
    listRepositoryTasks.mockResolvedValue({
      tasks: [makeTask({ column: "blocked", blocked_resource: "analysis_questions" })],
    });
    listTaskAnnotations.mockResolvedValue({ annotations: [] });
    listTaskQuestions.mockResolvedValue({
      questions: [makeQuestion({ blocking: true, status: "answered", answer: "Redis" })],
    });
    submitTaskQuestions.mockResolvedValue({ submitted: 1, task: makeTask({ column: "in_progress" }) });
    renderPage();

    const sendAnswers = await screen.findByRole("button", { name: "Send answers" });
    expect(sendAnswers).not.toBeDisabled();
    fireEvent.click(sendAnswers);

    await waitFor(() => expect(submitTaskQuestions).toHaveBeenCalledWith("repo-1", "task-1"));
    expect(await screen.findByText("board at /board?task=task-1")).toBeInTheDocument();
  });

  it("counts an answered-unsubmitted question into Send comments, allowed with zero open annotations", async () => {
    listTaskAnnotations.mockResolvedValue({ annotations: [] });
    listTaskQuestions.mockResolvedValue({
      questions: [makeQuestion({ blocking: false, status: "answered", answer: "Redis", submitted_at: null })],
    });
    renderPage();

    const sendComments = await screen.findByRole("button", { name: "Send comments (1)" });
    expect(sendComments).not.toBeDisabled();
  });

  it("sends answers alone through the submit dialog when there is no comment", async () => {
    listTaskAnnotations.mockResolvedValue({ annotations: [] });
    listTaskQuestions.mockResolvedValue({
      questions: [makeQuestion({ blocking: false, status: "answered", answer: "Redis", submitted_at: null })],
    });
    submitTaskAnnotations.mockResolvedValue({ submitted: 0, task: makeTask({ column: "need_revision" }) });
    renderPage();

    fireEvent.click(await screen.findByRole("button", { name: "Send comments (1)" }));
    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByText("Send your answers to the agent? (1)")).toBeInTheDocument();
    const confirm = within(dialog).getByRole("button", { name: "Send answers" });
    expect(confirm).not.toBeDisabled();
    fireEvent.click(confirm);

    await waitFor(() => expect(submitTaskAnnotations).toHaveBeenCalledWith("repo-1", "task-1", undefined));
    expect(await screen.findByText("board at /board?task=task-1")).toBeInTheDocument();
  });

  it("notes unsent answers when approving, alongside unsent comments", async () => {
    listTaskQuestions.mockResolvedValue({
      questions: [makeQuestion({ blocking: false, status: "answered", answer: "Redis", submitted_at: null })],
    });
    renderPage();

    fireEvent.click(await screen.findByRole("button", { name: "Approve" }));
    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByText(/will be applied when this task is split/)).toBeInTheDocument();
  });

  it("uses design wording for a design task in review", async () => {
    listRepositoryTasks.mockResolvedValue({ tasks: [makeTask({ task_type: "design", key: "D-3" })] });
    renderPage();

    expect(
      await screen.findByText("Approving this design task approves the design system versions it proposes."),
    ).toBeInTheDocument();
    await srcdoc();
    expect(document.querySelector("iframe")?.getAttribute("title")).toBe("Design document");
  });

  it("compares two design variants side by side and records the chosen one as a comment", async () => {
    listRepositoryTasks.mockResolvedValue({ tasks: [makeTask({ task_type: "design", key: "D-12" })] });
    listTaskDocuments.mockResolvedValue({
      documents: [
        makeDoc({ id: "doc-b", title: "design: export dialog · B", content: "<p>Variant B</p>" }),
        makeDoc({ id: "doc-handoff", title: "handoff: export dialog", content: "# Hand-off", format: "markdown" }),
        makeDoc({ id: "doc-a", title: "design: export dialog · A", content: "<p>Variant A</p>" }),
      ],
    });
    listTaskAnnotations.mockResolvedValue({ annotations: [] });
    listTaskComments.mockResolvedValue({
      comments: [
        {
          id: "c1",
          task_id: "task-1",
          author_type: "user",
          author_id: "me",
          content: "Chosen variant: design: export dialog · A",
          created_at: "2026-09-21T00:00:00Z",
        },
      ],
    });
    createTaskComment.mockImplementation(async (_repo: string, _task: string, content: string) => ({
      id: "c2",
      task_id: "task-1",
      author_type: "user",
      author_id: "me",
      content,
      created_at: "2026-09-22T00:00:00Z",
    }));
    renderPage();

    fireEvent.click(await screen.findByRole("button", { name: "Compare variants" }));
    const left = screen.getByRole("region", { name: "Left variant" });
    const right = screen.getByRole("region", { name: "Right variant" });
    await waitFor(() => expect(left.querySelector("iframe")?.getAttribute("srcdoc")).toContain("<p>Variant A</p>"));
    expect(right.querySelector("iframe")?.getAttribute("srcdoc")).toContain("<p>Variant B</p>");
    expect(left.querySelector("iframe")?.getAttribute("sandbox")).toBe("allow-scripts");
    expect(listTaskComments).toHaveBeenCalledWith("repo-1", "task-1");

    await waitFor(() => expect(within(left).getByText("Chosen")).toBeInTheDocument());
    expect(within(right).queryByText("Chosen")).not.toBeInTheDocument();
    expect(within(left).getByRole("button", { name: "Choose this variant" })).toBeDisabled();

    fireEvent.click(within(right).getByRole("button", { name: "Choose this variant" }));
    await waitFor(() =>
      expect(createTaskComment).toHaveBeenCalledWith("repo-1", "task-1", "Chosen variant: design: export dialog · B"),
    );
    expect(await within(right).findByText("Chosen")).toBeInTheDocument();
    expect(within(left).queryByText("Chosen")).not.toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Back to comments" }));
    expect(screen.queryByRole("region", { name: "Left variant" })).not.toBeInTheDocument();
    expect(screen.getByText("No comments yet")).toBeInTheDocument();
  });

  it("offers no comparison for a design task with a single HTML document", async () => {
    listRepositoryTasks.mockResolvedValue({ tasks: [makeTask({ task_type: "design" })] });
    renderPage();

    await srcdoc();
    expect(screen.queryByRole("button", { name: "Compare variants" })).not.toBeInTheDocument();
    expect(listTaskComments).not.toHaveBeenCalled();
  });

  it("uses design wording while the agent revises a design", async () => {
    listRepositoryTasks.mockResolvedValue({ tasks: [makeTask({ task_type: "design", column: "need_revision" })] });
    renderPage();

    expect(await screen.findByText("The agent is revising the design…")).toBeInTheDocument();
    expect(screen.getByText("Commenting is paused while the agent revises the design.")).toBeInTheDocument();
  });
});
