import { describe, expect, it } from "vitest";
import {
  SIGNAL_TREND,
  formatSignalValue,
  signalDelta,
  signalLabel,
  signalValueParts,
  type SignalFormatContext,
} from "@/components/operations/console/signalFormat";
import { en } from "@/locales/en";

function lookup(key: string): string {
  const value = key.split(".").reduce<unknown>((node, part) => (node as Record<string, unknown> | undefined)?.[part], en);
  return typeof value === "string" ? value : key;
}

const ctx: SignalFormatContext = { locale: "en-US", t: (key) => lookup(key) };

describe("signal values", () => {
  it("scales durations: ms past a second as seconds, seconds past two minutes as minutes, then hours", () => {
    expect(formatSignalValue(120, "ms", ctx)).toBe("120 ms");
    expect(formatSignalValue(1500, "ms", ctx)).toBe("1.5 s");
    expect(formatSignalValue(45, "s", ctx)).toBe("45 s");
    expect(formatSignalValue(600, "s", ctx)).toBe("10 min");
    expect(formatSignalValue(3 * 3600, "s", ctx)).toBe("3 h");
  });

  it("writes percentages, rates and counts with their units", () => {
    expect(formatSignalValue(4.25, "%", ctx)).toBe("4.25%");
    expect(formatSignalValue(35, "req/s", ctx)).toBe("35 req/s");
    expect(formatSignalValue(2, "msg/s", ctx)).toBe("2 msg/s");
    expect(formatSignalValue(0.5, "errors/s", ctx)).toBe("0.5 errors/s");
    expect(formatSignalValue(12, "messages", ctx)).toBe("12 messages");
    expect(formatSignalValue(3, "count", ctx)).toBe("3");
    expect(formatSignalValue(1.5, "cores", ctx)).toBe("1.5 cores");
    expect(formatSignalValue(3 * 1024 * 1024, "bytes", ctx)).toBe("3 MiB");
    expect(formatSignalValue(512, "bytes", ctx)).toBe("512 B");
    expect(formatSignalValue(7, "widgets", ctx)).toBe("7 widgets");
  });

  it("splits the number from the unit for a tile", () => {
    expect(signalValueParts(1500, "ms", ctx)).toEqual({ value: "1.5", unit: "s" });
    expect(signalValueParts(4, "%", ctx)).toEqual({ value: "4", unit: "%" });
  });
});

describe("signalDelta", () => {
  it("colours a rise by what the signal means: worse for latency, errors and backlog, better for throughput and crash-free users", () => {
    expect(signalDelta("http.latency", 150, 100, "ms", ctx)).toMatchObject({ text: "+50%", direction: "up", tone: "bad" });
    expect(signalDelta("http.error_ratio", 2, 1, "%", ctx)).toMatchObject({ text: "+1 pp", direction: "up", tone: "bad" });
    expect(signalDelta("queue.backlog", 30, 10, "messages", ctx)).toMatchObject({ direction: "up", tone: "bad" });
    expect(signalDelta("http.requests", 120, 100, "req/s", ctx)).toMatchObject({ text: "+20%", direction: "up", tone: "good" });
    expect(signalDelta("mobile.crash_free_users", 99.5, 98, "%", ctx)).toMatchObject({ text: "+1.5 pp", direction: "up", tone: "good" });
  });

  it("colours a fall the other way round", () => {
    expect(signalDelta("http.latency", 80, 100, "ms", ctx)).toMatchObject({ text: "−20%", direction: "down", tone: "good" });
    expect(signalDelta("http.requests", 50, 100, "req/s", ctx)).toMatchObject({ direction: "down", tone: "bad" });
  });

  it("never colours a neutral signal, and reads a tiny change as no change", () => {
    expect(SIGNAL_TREND["infra.cpu"]).toBeUndefined();
    expect(signalDelta("infra.cpu", 2, 1, "cores", ctx)).toMatchObject({ direction: "up", tone: "flat" });
    expect(signalDelta("http.latency", 100.4, 100, "ms", ctx)).toMatchObject({ text: "No change", direction: "flat", tone: "flat" });
  });

  it("says a percentage's change in percentage points, never as a relative percent", () => {
    expect(signalDelta("http.error_ratio", 3, 2, "%", ctx)).toMatchObject({ text: "+1 pp", tone: "bad" });
    expect(signalDelta("mobile.crash_free_users", 98.25, 99.5, "%", ctx)).toMatchObject({ text: "−1.25 pp", direction: "down", tone: "bad" });
    expect(signalDelta("http.error_ratio", 0.2, 0.1, "%", ctx)).toMatchObject({ text: "+0.1 pp" });
    expect(signalDelta("http.error_ratio", 1.001, 1, "%", ctx)).toMatchObject({ text: "No change", tone: "flat" });
    const tr = { locale: "tr-TR", t: (key: string) => (key === "operations.units.percentPoints" ? "puan" : lookup(key)) };
    expect(signalDelta("http.error_ratio", 3.5, 2, "%", tr)?.text).toBe("+1,5 puan");
  });

  it("says the difference itself when the previous period was zero, and nothing without both values", () => {
    expect(signalDelta("http.error_ratio", 2.5, 0, "%", ctx)).toMatchObject({ text: "+2.5 pp", tone: "bad" });
    expect(signalDelta("queue.backlog", 4, 0, "messages", ctx)).toMatchObject({ text: "+4 messages", tone: "bad" });
    expect(signalDelta("http.latency", 0, 0, "ms", ctx)).toMatchObject({ text: "No change", tone: "flat" });
    expect(signalDelta("http.latency", null, 10, "ms", ctx)).toBeUndefined();
    expect(signalDelta("http.latency", 10, null, "ms", ctx)).toBeUndefined();
    expect(signalDelta("http.latency", 150, 100, "ms", ctx)?.label).toBe("vs previous period");
  });
});

describe("signalLabel", () => {
  it("names a catalog signal in the reader's language, else by its id", () => {
    expect(signalLabel(ctx.t, "http.latency")).toBe("Response time");
    expect(signalLabel(ctx.t, "web.lcp", true)).toBe("LCP");
    expect(signalLabel(ctx.t, "infra.memory")).toBe("Memory");
    expect(signalLabel(ctx.t, "custom.thing")).toBe("custom.thing");
  });
});
