import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { AttachmentMeta, BoardTask, TaskComment } from "@/api";
import { TaskDetailDrawer } from "@/components/board/TaskDetailDrawer";
import { I18nProvider } from "@/hooks/useI18n";

if (!Element.prototype.scrollIntoView) {
  Element.prototype.scrollIntoView = () => {};
}

const calls = vi.hoisted(() => ({
  listTaskComments: vi.fn(),
  listTaskAgentRuns: vi.fn(),
  listTaskDocuments: vi.fn(),
  listAcceptanceCriteria: vi.fn(),
  listTaskAttachments: vi.fn(),
  listTestCases: vi.fn(),
  listReleases: vi.fn(),
  listTaskReviews: vi.fn(),
  listAgents: vi.fn(),
  listInitiativeProjects: vi.fn(),
  listRepositories: vi.fn(),
  uploadAttachment: vi.fn(),
  createTaskComment: vi.fn(),
}));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  const pending = new Proxy(actual.api, {
    get: (target, key: string) =>
      key in calls ? calls[key as keyof typeof calls] : typeof (target as Record<string, unknown>)[key] === "function"
        ? () => new Promise(() => {})
        : (target as Record<string, unknown>)[key],
  });
  return { ...actual, api: pending };
});

const stamp = "2026-01-01T00:00:00Z";
const task: BoardTask = {
  id: "t-1",
  key: "SHOP-1",
  title: "Checkout button",
  repository_id: "repo-1",
  task_number: 1,
  task_type: "task",
  description: "",
  technical_description: "",
  column: "todo",
  position: 0,
  priority: "medium",
  created_by: "me",
  created_at: stamp,
  updated_at: stamp,
};

function renderDrawer() {
  return render(
    <I18nProvider>
      <MemoryRouter>
        <TaskDetailDrawer
          open
          onOpenChange={() => {}}
          repositoryId="repo-1"
          task={task}
          columns={[]}
          members={[]}
          agents={[]}
          initiativeProjects={[]}
          repositories={[]}
          onUpdated={() => {}}
        />
      </MemoryRouter>
    </I18nProvider>,
  );
}

function answerDetailPoll(comments: TaskComment[] = []) {
  calls.listTaskComments.mockResolvedValue({ comments });
  calls.listTaskAgentRuns.mockResolvedValue({ runs: [] });
  calls.listTaskDocuments.mockResolvedValue({ documents: [] });
  calls.listAcceptanceCriteria.mockResolvedValue({ items: [] });
  calls.listTaskAttachments.mockResolvedValue({ attachments: [] });
  calls.listTestCases.mockResolvedValue({ items: [] });
  calls.listReleases.mockResolvedValue({ releases: [] });
  calls.listTaskReviews.mockResolvedValue({ column: "todo", rounds: [] });
  calls.listAgents.mockResolvedValue({ agents: [{ id: "a-1", name: "Reviewer", enabled: true }] });
  calls.listInitiativeProjects.mockResolvedValue({ projects: [] });
  calls.listRepositories.mockResolvedValue({ repositories: [] });
}

describe("TaskDetailDrawer comment box", () => {
  beforeEach(() => {
    for (const fn of Object.values(calls)) fn.mockReset();
    answerDetailPoll();
  });

  it("opens the mention menu when '@' is typed and lets the user pick an option", async () => {
    renderDrawer();
    const field = await screen.findByPlaceholderText("Write a revision note or explanation...");
    fireEvent.change(field, { target: { value: "cc @Rev" } });
    fireEvent.click(field);

    const option = await screen.findByText("Reviewer");
    fireEvent.mouseDown(option);

    expect((field as HTMLTextAreaElement).value).toBe("cc @Reviewer ");
  });

  it("adds a picked file as a pending attachment and submits it with the comment", async () => {
    const meta: AttachmentMeta = {
      id: "att-1",
      filename: "spec.pdf",
      content_type: "application/pdf",
      size_bytes: 5,
      sha256: "x",
      created_by_type: "user",
      created_at: stamp,
    };
    calls.uploadAttachment.mockResolvedValue(meta);
    calls.createTaskComment.mockResolvedValue({
      id: "c-1",
      task_id: "t-1",
      author_type: "user",
      author_id: "me",
      content: "see attached",
      created_at: stamp,
      attachments: [meta],
    });

    renderDrawer();
    const field = await screen.findByPlaceholderText("Write a revision note or explanation...");
    fireEvent.change(field, { target: { value: "see attached" } });

    const file = new File(["content"], "spec.pdf", { type: "application/pdf" });
    const inputs = document.querySelectorAll('input[type="file"]');
    const commentInput = inputs[inputs.length - 1] as HTMLInputElement;
    fireEvent.change(commentInput, { target: { files: [file] } });

    await screen.findByText("spec.pdf");

    fireEvent.click(screen.getByRole("button", { name: "Add Comment" }));

    await waitFor(() =>
      expect(calls.createTaskComment).toHaveBeenCalledWith("repo-1", "t-1", "see attached", ["att-1"]),
    );
  });

  it("shows an attachment linked to a loaded comment", async () => {
    answerDetailPoll([
      {
        id: "c-1",
        task_id: "t-1",
        author_type: "user",
        author_id: "me",
        content: "see attached",
        created_at: stamp,
        attachments: [
          {
            id: "att-1",
            filename: "spec.pdf",
            content_type: "application/pdf",
            size_bytes: 5,
            sha256: "x",
            created_by_type: "user",
            created_at: stamp,
          },
        ],
      },
    ]);
    renderDrawer();
    expect(await screen.findByText("spec.pdf")).toBeInTheDocument();
  });
});
