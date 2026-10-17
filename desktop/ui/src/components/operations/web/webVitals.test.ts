import { describe, expect, it } from "vitest";
import type { SignalSeries } from "@/api";
import { bandPosition, bandSegments, cwvBand, lastValue, vitalRead } from "@/components/operations/web/webVitals";

function sig(series: Array<{ name: string; points: Array<[number, number | null]> }>): SignalSeries {
  return { signal: "web.lcp", unit: "ms", bound: true, series: series.map((s) => ({ ...s, labels: {} })), previous: [], error: null };
}

describe("Core Web Vitals bands", () => {
  it("rates LCP, INP and CLS against Google's thresholds, boundaries included", () => {
    expect(cwvBand("web.lcp", 2500)).toBe("good");
    expect(cwvBand("web.lcp", 2501)).toBe("needsImprovement");
    expect(cwvBand("web.lcp", 4000)).toBe("needsImprovement");
    expect(cwvBand("web.lcp", 4001)).toBe("poor");
    expect(cwvBand("web.inp", 200)).toBe("good");
    expect(cwvBand("web.inp", 350)).toBe("needsImprovement");
    expect(cwvBand("web.inp", 501)).toBe("poor");
    expect(cwvBand("web.cls", 0.1)).toBe("good");
    expect(cwvBand("web.cls", 0.2)).toBe("needsImprovement");
    expect(cwvBand("web.cls", 0.26)).toBe("poor");
    expect(cwvBand("web.ttfb", 100)).toBeUndefined();
    expect(cwvBand("web.lcp", null)).toBeUndefined();
  });

  it("lays the meter out with segments ending at the thresholds", () => {
    const seg = bandSegments("web.lcp");
    expect(seg.good + seg.needsImprovement + seg.poor).toBeCloseTo(1);
    expect(bandPosition("web.lcp", 2500)).toBeCloseTo(seg.good);
    expect(bandPosition("web.lcp", 99_999)).toBe(1);
    expect(bandPosition("web.inp", -1)).toBe(0);
  });
});

describe("vitalRead", () => {
  it("reads the p75 series when the answer has one", () => {
    const read = vitalRead(sig([
      { name: "p50", points: [[1, 1000]] },
      { name: "p75", points: [[1, 2000], [2, 2600]] },
      { name: "p95", points: [[1, 5000]] },
    ]));
    expect(read).toMatchObject({ last: { at: 2, value: 2600 } });
  });

  it("takes a source's own percentile from a single series, and never stands a p95 in for p75", () => {
    expect(vitalRead(sig([{ name: "", points: [[1, 1800]] }]))).toMatchObject({ last: { value: 1800 } });
    expect(vitalRead(sig([{ name: "p50", points: [[1, 1]] }, { name: "p95", points: [[1, 9]] }, { name: "p99", points: [[1, 20]] }]))).toEqual({
      points: [],
      last: undefined,
    });
    expect(vitalRead(sig([{ name: "p95", points: [[1, 9]] }])).last).toBeUndefined();
    expect(vitalRead(undefined)).toEqual({ points: [], last: undefined });
  });

  it("finds the newest point with a value", () => {
    expect(lastValue([[1, 3], [2, null], [3, Number.NaN]])).toEqual({ at: 1, value: 3 });
    expect(lastValue([])).toBeUndefined();
  });
});
