import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { CatalogSyncProgress } from "@/api";
import { useCatalogSync } from "@/hooks/useCatalogSync";

const { getCatalogStatus } = vi.hoisted(() => ({ getCatalogStatus: vi.fn() }));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return { ...actual, api: { ...actual.api, getCatalogStatus } };
});

const adding = (added: string[]): CatalogSyncProgress => ({
  running: true, agent: "security-agent", new_agent: true,
  agents_done: 8, agents_total: 11, skills_done: 4, skills_total: 24, agents_added: added,
});

describe("useCatalogSync", () => {
  beforeEach(() => {
    getCatalogStatus.mockReset();
    vi.useFakeTimers({ toFake: ["setInterval", "clearInterval"] });
  });
  afterEach(() => vi.useRealTimers());

  it("refreshes the roster as each new agent lands and when the sync ends", async () => {
    const onAgentsChanged = vi.fn();
    getCatalogStatus.mockResolvedValue({ configured: true, progress: adding(["data-scientist"]) });
    const { result } = renderHook(() => useCatalogSync(onAgentsChanged));

    await act(async () => {});
    expect(result.current.busy).toBe(true);
    expect(onAgentsChanged).toHaveBeenCalledTimes(1);

    getCatalogStatus.mockResolvedValue({ configured: true, progress: adding(["data-scientist", "game-developer"]) });
    await act(() => vi.advanceTimersByTimeAsync(2000));
    expect(onAgentsChanged).toHaveBeenCalledTimes(2);

    getCatalogStatus.mockResolvedValue({
      configured: true,
      progress: { running: false, new_agent: false, agents_done: 0, agents_total: 0, skills_done: 0, skills_total: 0 },
    });
    await act(() => vi.advanceTimersByTimeAsync(2000));
    expect(result.current.busy).toBe(false);
    expect(onAgentsChanged).toHaveBeenCalledTimes(3);
  });

  it("treats a server without a catalog as idle", async () => {
    getCatalogStatus.mockResolvedValue({ configured: false });
    const { result } = renderHook(() => useCatalogSync());
    await act(async () => {});
    expect(getCatalogStatus).toHaveBeenCalled();
    expect(result.current.busy).toBe(false);
    expect(result.current.progress).toBeNull();
  });
});
