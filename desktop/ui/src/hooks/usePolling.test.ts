import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { usePolling } from "@/hooks/usePolling";

let visibility: DocumentVisibilityState = "visible";

function setVisibility(next: DocumentVisibilityState) {
  visibility = next;
  act(() => {
    document.dispatchEvent(new Event("visibilitychange"));
  });
}

beforeEach(() => {
  vi.useFakeTimers();
  visibility = "visible";
  Object.defineProperty(document, "visibilityState", { configurable: true, get: () => visibility });
});

afterEach(() => {
  vi.useRealTimers();
});

describe("usePolling", () => {
  it("runs at once and then on every interval", async () => {
    const callback = vi.fn();
    renderHook(() => usePolling(callback, 1000, true));
    expect(callback).toHaveBeenCalledTimes(1);
    await act(() => vi.advanceTimersByTimeAsync(3000));
    expect(callback).toHaveBeenCalledTimes(4);
  });

  it("skips ticks while the previous call is still pending", async () => {
    let finish!: () => void;
    const callback = vi.fn(
      () =>
        new Promise<void>((resolve) => {
          finish = resolve;
        }),
    );
    renderHook(() => usePolling(callback, 1000, true));
    await act(() => vi.advanceTimersByTimeAsync(5000));
    expect(callback).toHaveBeenCalledTimes(1);

    await act(async () => finish());
    await act(() => vi.advanceTimersByTimeAsync(1000));
    expect(callback).toHaveBeenCalledTimes(2);
  });

  it("with leading: false waits for the first interval, yet refreshes at once when the tab is shown again", async () => {
    const callback = vi.fn();
    renderHook(() => usePolling(callback, 1000, true, { leading: false }));
    expect(callback).not.toHaveBeenCalled();
    await act(() => vi.advanceTimersByTimeAsync(1000));
    expect(callback).toHaveBeenCalledTimes(1);

    setVisibility("hidden");
    await act(() => vi.advanceTimersByTimeAsync(5000));
    expect(callback).toHaveBeenCalledTimes(1);

    setVisibility("visible");
    expect(callback).toHaveBeenCalledTimes(2);
  });

  it("stops when disabled", async () => {
    const callback = vi.fn();
    const { rerender } = renderHook(({ enabled }) => usePolling(callback, 1000, enabled), {
      initialProps: { enabled: true },
    });
    rerender({ enabled: false });
    await act(() => vi.advanceTimersByTimeAsync(5000));
    expect(callback).toHaveBeenCalledTimes(1);
  });
});
