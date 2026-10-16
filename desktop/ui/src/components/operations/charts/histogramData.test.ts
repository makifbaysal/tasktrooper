import { describe, expect, it } from "vitest";
import { bucketTotal, bucketize, histogramStep, niceCeiling } from "@/components/operations/charts/histogramData";
import { SEVERITY_STACK, severityIndex } from "@/components/operations/logs/severity";

const MINUTE = 60_000;
const HOUR = 60 * MINUTE;

describe("histogramStep", () => {
  it("picks the smallest round width that fits the target bar count", () => {
    expect(histogramStep(HOUR)).toBe(MINUTE);
    expect(histogramStep(30 * MINUTE)).toBe(30_000);
    expect(histogramStep(24 * HOUR)).toBe(30 * MINUTE);
    expect(histogramStep(7 * 24 * HOUR)).toBe(3 * HOUR);
    expect(histogramStep(0)).toBe(1_000);
  });
});

describe("bucketize", () => {
  const from = Date.parse("2026-10-16T10:00:00Z");
  const to = from + 10 * MINUTE;
  const entries = [
    { at: from + 30_000, severity: "error" },
    { at: from + 45_000, severity: "error" },
    { at: from + 50_000, severity: "debug" },
    { at: from + 9 * MINUTE + 59_000, severity: "critical" },
    { at: to, severity: "warning" },
    { at: from - 1, severity: "info" },
    { at: to + MINUTE, severity: "info" },
  ];

  it("counts each loaded entry once, by severity, in round-edged buckets over the window", () => {
    const buckets = bucketize(
      entries,
      (e) => e.at,
      (e) => severityIndex(e.severity),
      SEVERITY_STACK.length,
      from,
      to,
      MINUTE,
    );
    expect(buckets).toHaveLength(10);
    expect(buckets[0].from).toBe(from);
    expect(buckets[0].counts[SEVERITY_STACK.indexOf("error")]).toBe(2);
    expect(buckets[0].counts[SEVERITY_STACK.indexOf("debug")]).toBe(1);
    expect(buckets[9].counts[SEVERITY_STACK.indexOf("critical")]).toBe(1);
    expect(buckets[9].counts[SEVERITY_STACK.indexOf("warning")]).toBe(1);
    expect(buckets.reduce((sum, b) => sum + bucketTotal(b), 0)).toBe(5);
  });

  it("aligns the first bucket to the step even when the window does not", () => {
    const buckets = bucketize([{ at: from + 90_000 }], (e) => e.at, () => 0, 1, from + 20_000, from + 5 * MINUTE, MINUTE);
    expect(buckets[0].from).toBe(from);
    expect(buckets[1].counts[0]).toBe(1);
  });

  it("returns nothing for an empty window", () => {
    expect(bucketize(entries, (e) => e.at, () => 0, 1, to, from)).toEqual([]);
  });
});

describe("niceCeiling", () => {
  it("rounds an axis top up to 1, 2 or 5 times a power of ten", () => {
    expect(niceCeiling(0)).toBe(1);
    expect(niceCeiling(1)).toBe(1);
    expect(niceCeiling(3)).toBe(5);
    expect(niceCeiling(7)).toBe(10);
    expect(niceCeiling(140)).toBe(200);
  });
});
