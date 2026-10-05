import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import type { Agent, BoardTask, TaskAgentRun } from "@/api";
import { TaskAgentRunsSection } from "@/components/board/TaskAgentRunsSection";
import { I18nProvider } from "@/hooks/useI18n";

vi.mock("@/hooks/useRunActivity", () => ({
  useRunActivity: (runId: string | null, _enabled: boolean, status?: string | null) => ({
    steps: runId
      ? [{ id: `${runId}-1`, run_id: runId, step_type: "assistant_message", payload: { content: `said in ${runId}` }, created_at: "2026-01-01T10:00:00Z" }]
      : [],
    plan: null,
    liveSummary: null,
    isLive: status === "running",
    loading: false,
    error: null,
  }),
}));

const task = { id: "t1", blocked_at: null } as unknown as BoardTask;
const agents = [{ id: "a1", name: "Developer" }, { id: "a2", name: "Reviewer" }] as Agent[];

function run(over: Partial<TaskAgentRun>): TaskAgentRun {
  return {
    id: "x",
    task_id: "t1",
    agent_id: "a1",
    board_event_id: "e",
    status: "completed",
    summary: "",
    created_at: "2026-01-01T10:00:00Z",
    updated_at: "2026-01-01T10:05:00Z",
    ...over,
  };
}

function renderSection(runs: TaskAgentRun[], extra: Partial<React.ComponentProps<typeof TaskAgentRunsSection>> = {}) {
  return render(
    <I18nProvider>
      <TaskAgentRunsSection
        task={task}
        runs={runs}
        agents={agents}
        agentNameMap={{}}
        active
        runActionId={null}
        onStop={() => {}}
        onRerun={() => {}}
        {...extra}
      />
    </I18nProvider>,
  );
}

describe("TaskAgentRunsSection", () => {
  it("shows the live run, offers Stop, and lists the others", () => {
    const onStop = vi.fn();
    renderSection(
      [
        run({ id: "old", session_run_id: "sr-old", summary: "old summary", created_at: "2026-01-01T09:00:00Z", agent_id: "a2" }),
        run({ id: "live", session_run_id: "sr-live", status: "running", created_at: "2026-01-01T10:00:00Z" }),
      ],
      { onStop },
    );
    expect(screen.getByText("said in sr-live")).toBeInTheDocument();
    expect(screen.getByText("Other runs (1)")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Stop" }));
    expect(onStop).toHaveBeenCalledWith("live");
  });

  it("switches to another run when its row is clicked and offers Re-run for a finished one", () => {
    const onRerun = vi.fn();
    renderSection(
      [
        run({ id: "new", session_run_id: "sr-new", created_at: "2026-01-01T10:00:00Z" }),
        run({ id: "old", session_run_id: "sr-old", summary: "older work", created_at: "2026-01-01T09:00:00Z", agent_id: "a2" }),
      ],
      { onRerun },
    );
    expect(screen.getByText("said in sr-new")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: /Reviewer/ }));
    expect(screen.getByText("said in sr-old")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Re-run" }));
    expect(onRerun).toHaveBeenCalledWith("old");
  });

  it("says a pending run with no session has not started", () => {
    renderSection([run({ id: "p", status: "pending" })]);
    expect(screen.getByText("Queued, not started yet")).toBeInTheDocument();
  });

  it("hides Re-run on a blocked task", () => {
    renderSection([run({ id: "d", session_run_id: "s" })], { task: { id: "t1", blocked_at: "2026-01-01T00:00:00Z" } as unknown as BoardTask });
    expect(screen.queryByRole("button", { name: "Re-run" })).toBeNull();
  });

  it("explains an empty list", () => {
    renderSection([]);
    expect(screen.getByText("No agent runs for this task yet.")).toBeInTheDocument();
  });
});
