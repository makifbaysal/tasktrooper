import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { OrchestrationPlan, SessionRun, SessionStep } from "@/api";
import { useRunActivity } from "@/hooks/useRunActivity";
import { useSessionActivity } from "@/hooks/useSessionActivity";

const { runSteps, getRunPlan } = vi.hoisted(() => ({ runSteps: vi.fn(), getRunPlan: vi.fn() }));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return { ...actual, api: { ...actual.api, runSteps, getRunPlan } };
});

const planStep: SessionStep = {
  id: "s1",
  run_id: "r1",
  step_type: "orchestration_plan_created",
  payload: null,
  created_at: "2026-10-01T10:00:00Z",
};

function plan(status: string): OrchestrationPlan {
  return { id: "p1", run_id: "r1", status, summary: "", tasks: [] };
}

beforeEach(() => {
  vi.useFakeTimers();
  runSteps.mockReset().mockResolvedValue({ steps: [planStep] });
  getRunPlan.mockReset().mockResolvedValue(plan("running"));
});

afterEach(() => {
  vi.useRealTimers();
});

describe("useRunActivity", () => {
  it("re-reads the plan once when the run turns terminal, though no new step landed", async () => {
    const { result, rerender } = renderHook(({ status }) => useRunActivity("r1", true, status), {
      initialProps: { status: "running" as string },
    });
    await act(() => vi.advanceTimersByTimeAsync(0));
    expect(result.current.plan?.status).toBe("running");
    expect(getRunPlan).toHaveBeenCalledTimes(1);

    await act(() => vi.advanceTimersByTimeAsync(4000));
    expect(getRunPlan).toHaveBeenCalledTimes(1);

    getRunPlan.mockResolvedValue(plan("completed"));
    rerender({ status: "completed" });
    await act(() => vi.advanceTimersByTimeAsync(0));
    expect(result.current.plan?.status).toBe("completed");
    expect(getRunPlan).toHaveBeenCalledTimes(2);

    await act(() => vi.advanceTimersByTimeAsync(10000));
    expect(getRunPlan).toHaveBeenCalledTimes(2);
  });

  it("still re-reads the plan when the run ends while a tick is in flight", async () => {
    const { result, rerender } = renderHook(({ status }) => useRunActivity("r1", true, status), {
      initialProps: { status: "running" as string },
    });
    await act(() => vi.advanceTimersByTimeAsync(0));

    let finishTick!: () => void;
    runSteps.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          finishTick = () => resolve({ steps: [] });
        }),
    );
    await act(() => vi.advanceTimersByTimeAsync(2000));
    getRunPlan.mockResolvedValue(plan("completed"));
    rerender({ status: "completed" });
    await act(async () => {
      finishTick();
      await vi.advanceTimersByTimeAsync(0);
    });
    expect(result.current.plan?.status).toBe("completed");
  });
});

describe("useSessionActivity", () => {
  const run = (status: string): SessionRun => ({ id: "r1", request_id: "q1", status, started_at: "2026-10-01T10:00:00Z" });

  it("re-reads a run's plan on the read that settles it", async () => {
    const { result, rerender } = renderHook(({ runs }) => useSessionActivity(runs, true), {
      initialProps: { runs: [run("running")] },
    });
    await act(() => vi.advanceTimersByTimeAsync(0));
    expect(result.current.inputs[0]?.plan?.status).toBe("running");
    const reads = getRunPlan.mock.calls.length;

    getRunPlan.mockResolvedValue(plan("completed"));
    rerender({ runs: [run("completed")] });
    await act(() => vi.advanceTimersByTimeAsync(0));
    expect(result.current.inputs[0]?.plan?.status).toBe("completed");
    expect(getRunPlan.mock.calls.length).toBe(reads + 1);

    await act(() => vi.advanceTimersByTimeAsync(10000));
    expect(getRunPlan.mock.calls.length).toBe(reads + 1);
  });
});
