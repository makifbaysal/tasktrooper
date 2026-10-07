import { act, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { Elapsed } from "@/components/ui/elapsed";
import { formatLiveElapsed } from "@/lib/elapsed";

beforeEach(() => {
  vi.useFakeTimers();
  vi.setSystemTime(new Date("2026-10-01T10:00:00Z"));
});

afterEach(() => {
  vi.useRealTimers();
});

describe("Elapsed", () => {
  it("counts seconds through the first minute, then switches to whole minutes", () => {
    render(<Elapsed since={Date.now()} />);
    expect(screen.getByText("0:00")).toBeInTheDocument();

    act(() => vi.advanceTimersByTime(1000));
    expect(screen.getByText("0:01")).toBeInTheDocument();

    act(() => vi.advanceTimersByTime(58_000));
    expect(screen.getByText("0:59")).toBeInTheDocument();

    act(() => vi.advanceTimersByTime(1000));
    expect(screen.getByText("1m")).toBeInTheDocument();

    act(() => vi.advanceTimersByTime(11 * 60_000));
    expect(screen.getByText("12m")).toBeInTheDocument();
  });

  it("re-renders every second in the first minute and every 30 seconds after", () => {
    const format = vi.fn(formatLiveElapsed);
    render(<Elapsed since={Date.now()} format={format} />);
    const secondBySecond = (seconds: number) => {
      for (let i = 0; i < seconds; i += 1) act(() => vi.advanceTimersByTime(1000));
    };

    format.mockClear();
    secondBySecond(60);
    expect(format).toHaveBeenCalledTimes(60);

    format.mockClear();
    secondBySecond(10 * 60);
    expect(format).toHaveBeenCalledTimes(20);
  });

  it("starts on the slow cadence for a run already past its first minute", () => {
    const format = vi.fn(formatLiveElapsed);
    render(<Elapsed since={Date.now() - 5 * 60_000} format={format} />);
    expect(screen.getByText("5m")).toBeInTheDocument();

    format.mockClear();
    act(() => vi.advanceTimersByTime(29_999));
    expect(format).not.toHaveBeenCalled();
    act(() => vi.advanceTimersByTime(1));
    expect(format).toHaveBeenCalledTimes(1);
  });

  it("stops ticking once unmounted", () => {
    const { unmount } = render(<Elapsed since={Date.now()} />);
    unmount();
    expect(vi.getTimerCount()).toBe(0);
  });
});
