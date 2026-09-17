import { fireEvent, render, screen, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { SessionStep } from "@/api";
import { SessionGraphView } from "@/components/chat/SessionGraphView";
import { I18nProvider } from "@/hooks/useI18n";

function step(id: string, step_type: string, payload: Record<string, unknown> | null = {}): SessionStep {
  return { id, run_id: "run-1", step_type, payload, created_at: `2026-01-01T00:00:0${id}.000Z` };
}

function renderGraph(steps: SessionStep[], isLive = false) {
  render(
    <I18nProvider>
      <SessionGraphView steps={steps} plan={null} isLive={isLive} />
    </I18nProvider>,
  );
}

const completedMultiSubtaskSteps: SessionStep[] = [
  step("1", "goal_intake_start"),
  step("2", "goal_intake_complete", { goal: "Ship the thing" }),
  step("3", "planner_start"),
  step("4", "planner_complete", { task_count: 2 }),
  step("5", "subtask_started", { task_key: "T-1", title: "First subtask" }),
  step("6", "claude_code_session", { model: "claude", cli_session_id: "abc" }),
  step("7", "iteration_start", { iteration: 1, message_count: 0 }),
  step("8", "tool_call_start", { tool: "bash", call_id: "c1", arguments: "{}" }),
  step("9", "tool_call_result", { tool: "bash", call_id: "c1", content: "tool call output content", is_error: false }),
  step("a", "subtask_completed", { task_key: "T-1", result: "done" }),
  step("b", "subtask_started", { task_key: "T-2", title: "Second subtask" }),
  step("c", "iteration_start", { iteration: 1, message_count: 0 }),
  step("d", "subtask_completed", { task_key: "T-2", result: "done" }),
  step("e", "verification_start"),
  step("f", "verification_complete", { passed: true, summary: "All good" }),
];

beforeEach(() => {
  localStorage.clear();
  window.HTMLElement.prototype.scrollIntoView = vi.fn();
});

describe("SessionGraphView phase grouping", () => {
  it("groups entries under phase headers instead of one flat list", () => {
    renderGraph(completedMultiSubtaskSteps);
    expect(screen.getByText("Planning")).toBeInTheDocument();
    expect(screen.getAllByText("Execution").length).toBe(2);
    expect(screen.getByText("Verification")).toBeInTheDocument();
  });

  it("collapses a completed subtask's detail by default and expands it on click", () => {
    renderGraph(completedMultiSubtaskSteps);
    const timeline = screen.getByTestId("session-graph-timeline");
    expect(within(timeline).queryByText("tool call output content")).not.toBeInTheDocument();

    fireEvent.click(within(timeline).getAllByText("Execution")[0]);
    fireEvent.click(within(timeline).getByText("First subtask"));
    fireEvent.click(within(timeline).getByText("Iteration 1"));
    fireEvent.click(within(timeline).getByText("bash"));
    expect(within(timeline).getByText("tool call output content")).toBeInTheDocument();
  });

  it("auto-expands the currently running subtask while everything else stays collapsed", () => {
    const liveSteps: SessionStep[] = [
      ...completedMultiSubtaskSteps.slice(0, 5),
      step("6b", "tool_call_start", { tool: "bash", call_id: "c2", arguments: "{}" }),
    ];
    renderGraph(liveSteps, true);
    expect(screen.getByText("First subtask")).toBeInTheDocument();
    expect(screen.getByText("bash")).toBeInTheDocument();
  });

  it("hides low-signal internal events behind a single toggle", () => {
    renderGraph(completedMultiSubtaskSteps);
    const timeline = screen.getByTestId("session-graph-timeline");
    fireEvent.click(within(timeline).getAllByText("Execution")[0]);
    expect(within(timeline).queryByText(/Claude Code session/i)).not.toBeInTheDocument();
    const toggle = within(timeline).getByText(/Show internal events/);
    fireEvent.click(toggle);
    expect(within(timeline).getByText(/Claude Code session/i)).toBeInTheDocument();
  });
});

describe("SessionGraphView outline rail", () => {
  it("renders one outline entry per subtask when there are 2 or more", () => {
    renderGraph(completedMultiSubtaskSteps);
    const outline = screen.getByTestId("session-graph-outline");
    expect(within(outline).getByText("First subtask")).toBeInTheDocument();
    expect(within(outline).getByText("Second subtask")).toBeInTheDocument();
  });

  it("scrolls the matching section into view when an outline entry is clicked", () => {
    renderGraph(completedMultiSubtaskSteps);
    const outline = screen.getByTestId("session-graph-outline");
    fireEvent.click(within(outline).getByText("Second subtask"));
    expect(window.HTMLElement.prototype.scrollIntoView).toHaveBeenCalled();
  });

  it("is absent for a single-subtask run", () => {
    const singleSubtaskSteps = completedMultiSubtaskSteps.filter((s) => !["b", "c", "d"].includes(s.id));
    renderGraph(singleSubtaskSteps);
    expect(screen.queryByTestId("session-graph-outline")).not.toBeInTheDocument();
  });

  it("is absent when there are no subtasks", () => {
    renderGraph([step("1", "goal_intake_start"), step("2", "goal_intake_complete", { goal: "x" })]);
    expect(screen.queryByTestId("session-graph-outline")).not.toBeInTheDocument();
  });
});

describe("SessionGraphView Turkish locale", () => {
  it("renders translated phase, toggle and outline labels instead of English fallback", () => {
    localStorage.setItem("bridge_locale", "tr");
    renderGraph(completedMultiSubtaskSteps);
    expect(screen.getByText("Planlama")).toBeInTheDocument();
    expect(screen.getAllByText("Yürütme").length).toBe(2);
    expect(screen.getByText("Doğrulama")).toBeInTheDocument();
    expect(screen.getAllByText("Alt göreve git").length).toBeGreaterThan(0);

    const timeline = screen.getByTestId("session-graph-timeline");
    fireEvent.click(within(timeline).getAllByText("Yürütme")[0]);
    expect(within(timeline).getByText(/İç olayları göster/)).toBeInTheDocument();
  });
});
