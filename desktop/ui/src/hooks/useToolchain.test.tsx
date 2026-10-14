import { act, renderHook, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { agentRestarter, markAgentsRestarted, useRestartPending, useToolchain } from "@/hooks/useToolchain";
import type {
  DesktopRunnerHost,
  DesktopToolchainHost,
  ToolchainJobState,
  ToolchainReport,
} from "@/lib/desktop-bridge";

const REPORT: ToolchainReport = { generatedAt: 1, platform: "darwin", items: [] };
const RUNNING: ToolchainJobState = {
  phase: "running",
  percent: 0,
  steps: [{ id: "node", label: "Node.js", state: "running", installKind: "auto" }],
};

function stubToolchain(initial: ToolchainJobState) {
  let push: (state: ToolchainJobState) => void = () => {};
  const toolchain: DesktopToolchainHost = {
    report: vi.fn().mockResolvedValue(REPORT),
    plan: vi.fn().mockResolvedValue(["homebrew", "node"]),
    state: vi.fn().mockResolvedValue(initial),
    subscribe: vi.fn((cb: (state: ToolchainJobState) => void) => {
      push = cb;
      return () => {};
    }),
    install: vi.fn().mockResolvedValue(RUNNING),
    cancel: vi.fn(),
    dismiss: vi.fn(),
    openPage: vi.fn(),
  };
  window.__tasktrooperDesktop = { toolchain };
  return { toolchain, push: (state: ToolchainJobState) => act(() => push(state)) };
}

afterEach(() => {
  delete window.__tasktrooperDesktop;
});

describe("useToolchain", () => {
  it("is unavailable without the bridge, and does nothing", async () => {
    const { result } = renderHook(() => useToolchain());
    expect(result.current.available).toBe(false);
    expect(await result.current.plan(["node"])).toEqual([]);
  });

  it("detects on mount, follows the job, and re-detects with force once it finishes", async () => {
    const { toolchain, push } = stubToolchain(RUNNING);
    const { result } = renderHook(() => useToolchain());
    await waitFor(() => expect(result.current.report).toEqual(REPORT));
    expect(toolchain.report).toHaveBeenCalledWith(false);
    await waitFor(() => expect(result.current.state?.phase).toBe("running"));

    push({ ...RUNNING, phase: "done", steps: [{ ...RUNNING.steps[0], state: "done" }], percent: 100 });
    await waitFor(() => expect(toolchain.report).toHaveBeenCalledWith(true));
  });

  it("takes the state install answers with", async () => {
    const { toolchain } = stubToolchain({ phase: "idle", steps: [], percent: 0 });
    const { result } = renderHook(() => useToolchain({ report: false }));
    await act(async () => {
      await result.current.install(["node"]);
    });
    expect(toolchain.install).toHaveBeenCalledWith(["node"]);
    expect(result.current.state).toEqual(RUNNING);
    expect(toolchain.report).not.toHaveBeenCalled();
  });
});

describe("agentRestarter", () => {
  it("is null without a runner bridge", () => {
    expect(agentRestarter()).toBeNull();
  });

  it("restarts the backend in local mode and the runner in account mode", async () => {
    const runner = {
      restartChild: vi.fn().mockResolvedValue(null),
      restart: vi.fn().mockResolvedValue(null),
    } as unknown as DesktopRunnerHost;

    window.__tasktrooperDesktop = { runner };
    await agentRestarter()?.();
    expect(runner.restartChild).toHaveBeenCalledWith("agent-server");

    window.__tasktrooperDesktop = { runner, mode: "account" };
    await agentRestarter()?.();
    expect(runner.restart).toHaveBeenCalledTimes(1);
  });
});

describe("useRestartPending", () => {
  const done = (jobId: string, restartRecommended = true): ToolchainJobState => ({
    phase: "done",
    jobId,
    percent: 100,
    steps: [],
    restartRecommended,
  });

  it("asks until the agents were restarted for that job, and again for the next one", () => {
    window.localStorage.clear();
    const { result, rerender } = renderHook(({ state }) => useRestartPending(state), {
      initialProps: { state: done("job-1") },
    });
    expect(result.current).toBe(true);

    act(() => markAgentsRestarted(done("job-1")));
    expect(result.current).toBe(false);

    rerender({ state: done("job-2") });
    expect(result.current).toBe(true);
    rerender({ state: done("job-3", false) });
    expect(result.current).toBe(false);
  });

  it("asks again when the restart it was marked for failed", () => {
    window.localStorage.clear();
    const { result } = renderHook(() => useRestartPending(done("job-4")));
    let undo: () => void = () => {};
    act(() => {
      undo = markAgentsRestarted(done("job-4"));
    });
    expect(result.current).toBe(false);
    act(() => undo());
    expect(result.current).toBe(true);
  });
});
