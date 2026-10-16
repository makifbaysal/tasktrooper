import { describe, expect, it } from "vitest";
import type { MetricSeries, SignalSeries } from "@/api";
import { autoStepSeconds, headlinePoints, signalChartSeries } from "@/components/operations/charts/signalChart";

const labels = { signal: "Response time", previous: (label: string) => `${label} · previous period` };

function series(name: string, points: Array<[number, number | null]>, extra: Partial<MetricSeries> = {}): MetricSeries {
  return { name, labels: {}, points, ...extra };
}

function signal(partial: Partial<SignalSeries>): SignalSeries {
  return { signal: "http.latency", unit: "ms", bound: true, series: [], previous: [], error: null, ...partial };
}

describe("signalChartSeries", () => {
  it("draws a latency signal's quantiles in p50, p95, p99 order, each in its own colour", () => {
    const lines = signalChartSeries(
      signal({ series: [series("p99", [[1, 300]]), series("p50", [[1, 40]]), series("p95", [[1, 120]])] }),
      false,
      labels,
    );
    expect(lines.map((l) => [l.label, l.colorIndex, l.dashed ?? false])).toEqual([
      ["p50", 1, false],
      ["p95", 2, false],
      ["p99", 3, false],
    ]);
    expect(new Set(lines.map((l) => l.id)).size).toBe(3);
  });

  it("adds the previous period as dashed lines in the colour of the line each matches, only with compare on", () => {
    const sig = signal({
      series: [series("p50", [[1, 40]]), series("p95", [[1, 120]])],
      previous: [series("p95", [[1, 100]]), series("p50", [[1, 35]])],
    });
    expect(signalChartSeries(sig, false, labels)).toHaveLength(2);
    const lines = signalChartSeries(sig, true, labels);
    expect(lines.map((l) => [l.label, l.colorIndex, l.dashed ?? false])).toEqual([
      ["p50", 1, false],
      ["p95", 2, false],
      ["p50 · previous period", 1, true],
      ["p95 · previous period", 2, true],
    ]);
  });

  it("names a lone series after the signal and several by their labels", () => {
    const one = signalChartSeries(signal({ signal: "http.requests", series: [series("", [[1, 5]])] }), false, {
      ...labels,
      signal: "Throughput",
    });
    expect(one.map((l) => l.label)).toEqual(["Throughput"]);
    const many = signalChartSeries(
      signal({
        signal: "queue.backlog",
        series: [series("", [[1, 5]], { labels: { queue: "jobs" } }), series("", [[1, 7]], { labels: { queue: "mail" } })],
      }),
      false,
      labels,
    );
    expect(many.map((l) => [l.label, l.colorIndex])).toEqual([
      ['{queue="jobs"}', 1],
      ['{queue="mail"}', 2],
    ]);
  });

  it("keeps a recipe's several series per quantile under the server's names", () => {
    const lines = signalChartSeries(
      signal({ series: [series("p95 api", [[1, 120]]), series("p50 api", [[1, 40]]), series("p95 web", [[1, 90]])] }),
      false,
      labels,
    );
    expect(lines.map((l) => l.label)).toEqual(["p50 api", "p95 api", "p95 web"]);
  });
});

describe("headlinePoints / autoStepSeconds", () => {
  it("stands a quantile signal for its p95, anything else for its first series", () => {
    expect(headlinePoints([series("p50", [[1, 1]]), series("p95", [[1, 2]])])).toEqual([[1, 2]]);
    expect(headlinePoints([series("p95 api", [[1, 3]])])).toEqual([[1, 3]]);
    expect(headlinePoints([series("", [[1, 4]])])).toEqual([[1, 4]]);
    expect(headlinePoints(undefined)).toEqual([]);
  });

  it("picks a round step of about 240 points, never under 15 s", () => {
    expect(autoStepSeconds(30 * 60_000)).toBe(15);
    expect(autoStepSeconds(60 * 60_000)).toBe(15);
    expect(autoStepSeconds(24 * 3_600_000)).toBe(600);
    expect(autoStepSeconds(7 * 24 * 3_600_000)).toBe(3600);
  });
});
