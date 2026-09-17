import { describe, expect, it } from "vitest";
import type { SessionStep } from "@/api";
import { buildSessionGraph, groupSessionGraphPhases, isLowSignalGraphEvent } from "@/lib/sessionGraph";

function step(id: string, step_type: string, payload: Record<string, unknown> | null = {}): SessionStep {
  return { id, run_id: "run-1", step_type, payload, created_at: `2026-01-01T00:00:0${id}.000Z` };
}

const steps: SessionStep[] = [
  step("1", "goal_intake_start"),
  step("2", "goal_intake_complete", { goal: "Ship the thing" }),
  step("3", "planner_start"),
  step("4", "planner_complete", { task_count: 2 }),
  step("5", "orchestration_plan_created", { plan_id: "p1", summary: "Do it", task_count: 2 }),
  step("6", "subtask_started", { task_key: "T-1", title: "First subtask" }),
  step("7", "claude_code_session", { model: "claude", cli_session_id: "abc" }),
  step("8", "iteration_start", { iteration: 1, message_count: 0 }),
  step("9", "tool_call_start", { tool: "bash", call_id: "c1", arguments: "{}" }),
  step("a", "tool_call_result", { tool: "bash", call_id: "c1", content: "ok", is_error: false }),
  step("b", "subtask_completed", { task_key: "T-1", result: "done" }),
  step("c", "subtask_started", { task_key: "T-2", title: "Second subtask" }),
  step("d", "iteration_start", { iteration: 1, message_count: 0 }),
  step("e", "subtask_completed", { task_key: "T-2", result: "done" }),
  step("f", "verification_start"),
  step("g", "verification_complete", { passed: true, summary: "All good" }),
  step("h", "assistant_message", { content: "All done!" }),
];

describe("groupSessionGraphPhases", () => {
  it("groups entries into labelled phases in order, one execution phase per subtask", () => {
    const entries = buildSessionGraph(steps, null, false);
    const phases = groupSessionGraphPhases(entries);

    expect(phases.map((p) => p.kind)).toEqual(["planning", "execution", "execution", "verification", "messages"]);
  });

  it("merges consecutive same-classification entries into one phase", () => {
    const entries = buildSessionGraph(steps, null, false);
    const phases = groupSessionGraphPhases(entries);
    const planningPhase = phases[0];
    // goal_intake_start, goal_intake_complete, planner_start, planner_complete, orchestration_plan_created
    expect(planningPhase.entries.length).toBe(5);
  });

  it("attaches low-signal events to the enclosing phase instead of dropping them", () => {
    const entries = buildSessionGraph(steps, null, false);
    const phases = groupSessionGraphPhases(entries);
    const firstExecutionPhase = phases.find((p) => p.kind === "execution" && p.entries[0]?.kind === "subtask" && (p.entries[0] as { taskKey: string }).taskKey === "T-1");
    expect(firstExecutionPhase).toBeDefined();
    expect(firstExecutionPhase!.entries.some((e) => e.kind === "event" && e.stepType === "claude_code_session")).toBe(true);
  });

  it("never drops an entry: total entries across phases equals input entry count", () => {
    const entries = buildSessionGraph(steps, null, false);
    const phases = groupSessionGraphPhases(entries);
    const total = phases.reduce((sum, phase) => sum + phase.entries.length, 0);
    expect(total).toBe(entries.length);
  });

  it("keeps subtasks in their own execution phase even when consecutive", () => {
    const entries = buildSessionGraph(steps, null, false);
    const phases = groupSessionGraphPhases(entries);
    const executionPhases = phases.filter((p) => p.kind === "execution");
    expect(executionPhases).toHaveLength(2);
    expect((executionPhases[0].entries[0] as { taskKey: string }).taskKey).toBe("T-1");
    expect((executionPhases[1].entries[0] as { taskKey: string }).taskKey).toBe("T-2");
  });
});

describe("isLowSignalGraphEvent", () => {
  it("flags internal chrome events as low signal", () => {
    const entries = buildSessionGraph(steps, null, false);
    const ccSession = entries.find((e) => e.kind === "event" && e.stepType === "claude_code_session");
    expect(ccSession).toBeDefined();
    expect(isLowSignalGraphEvent(ccSession!)).toBe(true);
  });

  it("does not flag planning/verification events as low signal", () => {
    const entries = buildSessionGraph(steps, null, false);
    const plannerStart = entries.find((e) => e.kind === "event" && e.stepType === "planner_start");
    expect(plannerStart).toBeDefined();
    expect(isLowSignalGraphEvent(plannerStart!)).toBe(false);
  });
});
