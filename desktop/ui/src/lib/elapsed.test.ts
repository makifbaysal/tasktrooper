import { describe, expect, it } from "vitest";
import { ELAPSED_COARSE_TICK_MS, ELAPSED_FINE_TICK_MS, elapsedTickMs, formatLiveElapsed } from "@/lib/elapsed";

describe("elapsedTickMs", () => {
  it("ticks every second during the first minute, then every 30 seconds", () => {
    expect(ELAPSED_FINE_TICK_MS).toBe(1000);
    expect(ELAPSED_COARSE_TICK_MS).toBe(30_000);
    expect(elapsedTickMs(0)).toBe(1000);
    expect(elapsedTickMs(59_999)).toBe(1000);
    expect(elapsedTickMs(60_000)).toBe(30_000);
    expect(elapsedTickMs(3 * 3_600_000)).toBe(30_000);
  });
});

describe("formatLiveElapsed", () => {
  it("shows seconds only in the first minute", () => {
    expect(formatLiveElapsed(0)).toBe("0:00");
    expect(formatLiveElapsed(42_900)).toBe("0:42");
    expect(formatLiveElapsed(60_000)).toBe("1m");
    expect(formatLiveElapsed(12 * 60_000 + 59_000)).toBe("12m");
  });

  it("adds hours past the first hour", () => {
    expect(formatLiveElapsed(3_600_000)).toBe("1h 0m");
    expect(formatLiveElapsed(3_600_000 + 5 * 60_000)).toBe("1h 5m");
  });

  it("never goes negative", () => {
    expect(formatLiveElapsed(-5000)).toBe("0:00");
  });
});
