import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ApiError, type AIJob } from "@/api";
import { useAIJob } from "@/hooks/useAIJob";
import { readCache, writeCache } from "@/lib/uiCache";

const { getAIJob } = vi.hoisted(() => ({ getAIJob: vi.fn() }));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return { ...actual, api: { ...actual.api, getAIJob } };
});

function job(overrides: Partial<AIJob> = {}): AIJob {
  return {
    id: "j-1",
    kind: "define_errors",
    repository_id: "r-shop",
    component_id: null,
    environment: "production",
    status: "running",
    session_id: "s-1",
    result_summary: "",
    error: "",
    created: [],
    created_at: "2026-10-17T10:00:00Z",
    updated_at: "2026-10-17T10:00:00Z",
    finished_at: null,
    ...overrides,
  };
}

const KEY = "operations.aiJob.test";

describe("useAIJob", () => {
  beforeEach(() => {
    sessionStorage.clear();
    writeCache(KEY, null);
    getAIJob.mockReset();
    vi.useFakeTimers({ toFake: ["setInterval", "clearInterval"] });
  });
  afterEach(() => vi.useRealTimers());

  it("starts a job, reads it every 3 s while it runs and reports the finish once", async () => {
    const onFinished = vi.fn();
    getAIJob.mockResolvedValueOnce(job()).mockResolvedValue(job({ status: "done", created: [{ kind: "error_definition", id: "d-1", name: "Stripe timeouts" }] }));
    const { result } = renderHook(() => useAIJob(KEY, onFinished));

    await act(async () => {
      await result.current.start(() => Promise.resolve({ job_id: "j-1", job: job() }));
    });
    expect(result.current.job?.status).toBe("running");
    expect(getAIJob).toHaveBeenCalledTimes(1);

    await act(() => vi.advanceTimersByTimeAsync(2_999));
    expect(getAIJob).toHaveBeenCalledTimes(1);
    await act(() => vi.advanceTimersByTimeAsync(1));
    expect(getAIJob).toHaveBeenCalledTimes(2);
    expect(result.current.job?.status).toBe("done");
    expect(onFinished).toHaveBeenCalledTimes(1);
    expect(onFinished.mock.calls[0][0].created).toHaveLength(1);

    await act(() => vi.advanceTimersByTimeAsync(9_000));
    expect(getAIJob).toHaveBeenCalledTimes(2);
  });

  it("says no release engineer is on, and starts nothing", async () => {
    const { result } = renderHook(() => useAIJob(KEY));
    await act(async () => {
      await result.current.start(() => Promise.reject(new ApiError("no release engineer", 409, "no_release_engineer")));
    });
    expect(result.current.refusal).toBe("no_release_engineer");
    expect(result.current.job).toBeNull();
    expect(getAIJob).not.toHaveBeenCalled();
  });

  it("keeps any other refusal in the server's words", async () => {
    const { result } = renderHook(() => useAIJob(KEY));
    await act(async () => {
      await result.current.start(() => Promise.reject(new ApiError("repository_id: required", 400, "", { repository_id: "required" })));
    });
    expect(result.current.refusal).toBeNull();
    expect(result.current.startError).toBe("repository_id: required");
  });

  it("picks a running job back up from the session and forgets it on dismiss", async () => {
    writeCache(KEY, job());
    getAIJob.mockResolvedValue(job({ status: "failed", error: "the release engineer stopped on a question; answer it in the chat" }));
    const { result } = renderHook(() => useAIJob(KEY));
    await act(async () => {});
    expect(getAIJob).toHaveBeenCalledWith("j-1");
    expect(result.current.job?.status).toBe("failed");

    act(() => result.current.dismiss());
    expect(result.current.job).toBeNull();
    expect(readCache(KEY)).toBeNull();
  });

  it("keeps the last job read when a progress read fails", async () => {
    writeCache(KEY, job());
    getAIJob.mockRejectedValue(new Error("network down"));
    const { result } = renderHook(() => useAIJob(KEY));
    await act(async () => {});
    expect(result.current.pollError).toBe("network down");
    expect(result.current.job?.status).toBe("running");
  });
});
