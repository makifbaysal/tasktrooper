import { describe, expect, it } from "vitest";
import {
  alignSeries,
  chartTableRows,
  hasChartData,
  nearestMarker,
  seriesColorIndex,
  withAlpha,
} from "@/components/operations/charts/chartData";
import { sparklineGeometry } from "@/components/operations/charts/Sparkline";

const a = { id: "a", label: "A", points: [[1000, 1], [2000, 2], [3000, null]] as Array<[number, number | null]> };
const b = { id: "b", label: "B", points: [[2000, 20], [4000, 40]] as Array<[number, number | null]> };

describe("alignSeries", () => {
  it("joins every series onto one sorted time axis", () => {
    const { xs, ys } = alignSeries([b, a]);
    expect(xs).toEqual([1000, 2000, 3000, 4000]);
    expect(ys[0]).toEqual([undefined, 20, undefined, 40]);
    expect(ys[1]).toEqual([1, 2, null, undefined]);
  });

  it("keeps an explicit null as a gap and turns a non-finite value into one", () => {
    const { ys } = alignSeries([{ id: "c", label: "C", points: [[1, Number.NaN], [2, 3]] }]);
    expect(ys[0]).toEqual([null, 3]);
  });

  it("is empty for no series", () => {
    expect(alignSeries([])).toEqual({ xs: [], ys: [] });
  });
});

describe("hasChartData", () => {
  it("needs at least one real value", () => {
    expect(hasChartData([])).toBe(false);
    expect(hasChartData([{ id: "x", label: "X", points: [] }])).toBe(false);
    expect(hasChartData([{ id: "x", label: "X", points: [[1, null]] }])).toBe(false);
    expect(hasChartData([a])).toBe(true);
  });
});

describe("chartTableRows", () => {
  it("gives one row per timestamp with a value or null per series", () => {
    expect(chartTableRows([a, b])).toEqual([
      { at: 1000, values: [1, null] },
      { at: 2000, values: [2, 20] },
      { at: 3000, values: [null, null] },
      { at: 4000, values: [null, 40] },
    ]);
  });
});

describe("seriesColorIndex", () => {
  it("follows the position unless the series names its slot", () => {
    expect(seriesColorIndex({}, 0)).toBe(1);
    expect(seriesColorIndex({}, 3)).toBe(4);
    expect(seriesColorIndex({ colorIndex: 6 }, 0)).toBe(6);
  });

  it("stays inside the eight slots", () => {
    expect(seriesColorIndex({}, 8)).toBe(1);
    expect(seriesColorIndex({ colorIndex: 9 }, 0)).toBe(1);
    expect(seriesColorIndex({ colorIndex: 0 }, 0)).toBe(8);
  });
});

describe("withAlpha", () => {
  it("handles hex, rgb and oklch", () => {
    expect(withAlpha("#2a78d6", 0.1)).toBe("rgba(42, 120, 214, 0.1)");
    expect(withAlpha("#fff", 0.5)).toBe("rgba(255, 255, 255, 0.5)");
    expect(withAlpha("rgb(1, 2, 3)", 0.2)).toBe("rgb(1 2 3 / 0.2)");
    expect(withAlpha("oklch(0.5 0.1 200)", 0.1)).toBe("oklch(0.5 0.1 200 / 0.1)");
    expect(withAlpha("oklch(0.5 0.1 200 / 0.8)", 0.1)).toBe("oklch(0.5 0.1 200 / 0.1)");
  });
});

describe("nearestMarker", () => {
  const markers = [{ at: 1000, label: "v1" }, { at: 5000, label: "v2" }];
  it("finds the closest marker within the tolerance", () => {
    expect(nearestMarker(markers, 1200, 500)?.label).toBe("v1");
    expect(nearestMarker(markers, 4900, 500)?.label).toBe("v2");
    expect(nearestMarker(markers, 3000, 500)).toBeUndefined();
  });
});

describe("sparklineGeometry", () => {
  it("draws one path per unbroken run and ends on the last value", () => {
    const g = sparklineGeometry([[0, 0], [1, 10], [2, null], [3, 5], [4, 10]], 100, 20);
    expect(g.segments).toHaveLength(2);
    expect(g.segments[0].startsWith("M")).toBe(true);
    expect(g.last).toEqual({ x: 97, y: 3 });
  });

  it("is empty without values", () => {
    expect(sparklineGeometry([[0, null]], 100, 20)).toEqual({ segments: [] });
  });

  it("centres a flat line", () => {
    const g = sparklineGeometry([[0, 4], [1, 4]], 100, 20);
    expect(g.segments[0]).toBe("M3 10 L97 10");
  });
});
