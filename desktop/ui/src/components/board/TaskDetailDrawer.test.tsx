import "@testing-library/jest-dom/vitest";
import { act, render, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { BoardTask, TaskAgentRun } from "@/api";
import { TaskDetailDrawer } from "@/components/board/TaskDetailDrawer";
import { I18nProvider } from "@/hooks/useI18n";

const detail = vi.hoisted(() => ({
  listTaskComments: vi.fn(),
  listTaskAgentRuns: vi.fn(),
  listTaskDocuments: vi.fn(),
  listAcceptanceCriteria: vi.fn(),
  listTaskAttachments: vi.fn(),
  listTestCases: vi.fn(),
  listReleases: vi.fn(),
}));

// Everything the drawer's child sections fetch on their own never answers:
// this suite is only about the drawer's own detail poll.
vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  const pending = new Proxy(actual.api, {
    get: (target, key: string) =>
      key in detail ? detail[key as keyof typeof detail] : typeof (target as Record<string, unknown>)[key] === "function"
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

const run = (status: string): TaskAgentRun => ({
  id: "run-1",
  task_id: "t-1",
  agent_id: "agent-1",
  board_event_id: "evt-1",
  status,
  summary: "",
  created_at: stamp,
  updated_at: stamp,
});

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

function answerWithRuns(runs: TaskAgentRun[]) {
  detail.listTaskComments.mockResolvedValue({ comments: [] });
  detail.listTaskAgentRuns.mockResolvedValue({ runs });
  detail.listTaskDocuments.mockResolvedValue({ documents: [] });
  detail.listAcceptanceCriteria.mockResolvedValue({ items: [] });
  detail.listTaskAttachments.mockResolvedValue({ attachments: [] });
  detail.listTestCases.mockResolvedValue({ items: [] });
  detail.listReleases.mockResolvedValue({ releases: [] });
}

describe("TaskDetailDrawer detail poll", () => {
  beforeEach(() => {
    for (const fn of Object.values(detail)) fn.mockReset();
    vi.useFakeTimers({ toFake: ["setInterval", "clearInterval"] });
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("reads each detail endpoint once on open, not twice", async () => {
    answerWithRuns([]);
    renderDrawer();
    await waitFor(() => expect(detail.listReleases).toHaveBeenCalled());
    await act(async () => {});
    for (const fn of Object.values(detail)) expect(fn).toHaveBeenCalledTimes(1);
  });

  it("polls every 10s while no run is going", async () => {
    answerWithRuns([run("completed")]);
    renderDrawer();
    await waitFor(() => expect(detail.listTaskComments).toHaveBeenCalledTimes(1));

    await act(() => vi.advanceTimersByTimeAsync(2000));
    expect(detail.listTaskComments).toHaveBeenCalledTimes(1);
    await act(() => vi.advanceTimersByTimeAsync(8000));
    expect(detail.listTaskComments).toHaveBeenCalledTimes(2);
  });

  it("polls every 2s while a run is going", async () => {
    answerWithRuns([run("running")]);
    renderDrawer();
    // The first answer shows a live run, which restarts the poll at the fast rate.
    await waitFor(() => expect(detail.listTaskComments).toHaveBeenCalledTimes(2));

    await act(() => vi.advanceTimersByTimeAsync(2000));
    expect(detail.listTaskComments).toHaveBeenCalledTimes(3);
  });
});
