import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { HEALTH_POLL_MS, useHealth } from "@/hooks/useHealth";

const api = vi.hoisted(() => ({ health: vi.fn() }));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return { ...actual, api: { ...actual.api, ...api } };
});

let online = true;

beforeEach(() => {
  vi.useFakeTimers();
  online = true;
  Object.defineProperty(navigator, "onLine", { configurable: true, get: () => online });
  api.health.mockReset().mockResolvedValue({ status: "ok", llm: "ok" });
});

afterEach(() => {
  vi.useRealTimers();
});

async function mount() {
  const hook = renderHook(() => useHealth());
  await act(() => vi.advanceTimersByTimeAsync(0));
  return hook;
}

describe("useHealth", () => {
  it("checks once on mount, then once a minute", async () => {
    expect(HEALTH_POLL_MS).toBe(60_000);
    const { result } = await mount();
    expect(api.health).toHaveBeenCalledTimes(1);
    expect(result.current.loading).toBe(false);

    await act(() => vi.advanceTimersByTimeAsync(HEALTH_POLL_MS - 1));
    expect(api.health).toHaveBeenCalledTimes(1);
    await act(() => vi.advanceTimersByTimeAsync(1));
    expect(api.health).toHaveBeenCalledTimes(2);
  });

  it("skips the poll while the machine is offline and checks again once it is back", async () => {
    await mount();
    online = false;
    await act(() => vi.advanceTimersByTimeAsync(3 * HEALTH_POLL_MS));
    expect(api.health).toHaveBeenCalledTimes(1);

    online = true;
    await act(async () => {
      window.dispatchEvent(new Event("online"));
    });
    expect(api.health).toHaveBeenCalledTimes(2);
  });

  it("refreshes at once when the window gets focus", async () => {
    await mount();
    await act(async () => {
      window.dispatchEvent(new Event("focus"));
    });
    expect(api.health).toHaveBeenCalledTimes(2);
  });

  it("still makes the first check offline, so the badge never sits on loading", async () => {
    online = false;
    const { result } = await mount();
    expect(api.health).toHaveBeenCalledTimes(1);
    expect(result.current.loading).toBe(false);
  });
});
