import { describe, expect, it } from "vitest";
import type { ComponentSignals, SignalSeries } from "@/api";
import {
  backlogTrend,
  combinePoints,
  hasQueueBinding,
  isLiveSignal,
  queueSignalValue,
  queueSuggestionCount,
  tableSignal,
} from "@/components/operations/queues/queueSignals";

const NOW = Date.parse("2026-10-17T10:00:00Z");

function sig(partial: Partial<SignalSeries>): SignalSeries {
  return { signal: "queue.backlog", unit: "messages", bound: true, series: [], previous: [], error: null, ...partial };
}

describe("queue signals", () => {
  it("adds series up per instant, or takes the largest, a gap staying a gap", () => {
    const series = [
      { name: "a", labels: {}, points: [[1, 2], [2, null], [3, 5]] as [number, number | null][] },
      { name: "b", labels: {}, points: [[1, 3], [2, null], [3, 1]] as [number, number | null][] },
    ];
    expect(combinePoints(series, "sum")).toEqual([[1, 5], [2, null], [3, 6]]);
    expect(combinePoints(series, "max")).toEqual([[1, 3], [2, null], [3, 5]]);
  });

  it("takes a signal as live only when the server flags it, never from its shape", () => {
    const point = [{ name: "lag", labels: {}, points: [[NOW, 42]] as [number, number | null][] }];
    expect(isLiveSignal(sig({ live: true, series: point }))).toBe(true);
    expect(isLiveSignal(sig({ series: point }))).toBe(false);
    expect(isLiveSignal(sig({ live: true, bound: false, series: point }))).toBe(false);
    expect(isLiveSignal(sig({ live: true, series: [], error: { code: "timeout", message: "slow" } }))).toBe(false);
    expect(isLiveSignal(undefined)).toBe(false);
    expect(queueSignalValue(sig({ live: true, series: point }), "sum")).toMatchObject({ value: 42, live: true });
  });

  it("gives the tile its latest value, the previous period's and whether it is live", () => {
    const read = queueSignalValue(
      sig({
        series: [{ name: "x", labels: {}, points: [[NOW - 60_000, 10], [NOW, 12]] }],
        previous: [{ name: "x", labels: {}, points: [[NOW - 60_000, 8], [NOW, null]] }],
      }),
      "sum",
    );
    expect(read).toMatchObject({ value: 12, at: NOW, previous: 8, live: false });
  });

  it("says whether the backlog grows or drains, a jitter being balanced", () => {
    expect(backlogTrend(10, 20)).toBe("growing");
    expect(backlogTrend(20, 10)).toBe("draining");
    expect(backlogTrend(100, 101)).toBe("steady");
    expect(backlogTrend(undefined, 10)).toBeUndefined();
  });

  it("finds queue bindings, waiting suggestions and the signal a table was read through", () => {
    const signals = {
      component_id: "c",
      role: "worker",
      environment: "production",
      signals: [
        { signal: "process.errors", kind: "metric", unit: "errors/s", golden: true, status: "bound", binding: { id: "b-0", data_source_id: "ds-1" }, suggestions: [] },
        { signal: "queue.backlog", kind: "metric", unit: "messages", golden: true, status: "unbound", binding: null, suggestions: [{ id: "s-1" }, { id: "s-2" }] },
        { signal: "queue.consumers", kind: "metric", unit: "count", golden: false, status: "bound", binding: { id: "b-1", data_source_id: "ds-2" }, suggestions: [] },
      ],
    } as unknown as ComponentSignals;
    expect(hasQueueBinding(signals)).toBe(true);
    expect(hasQueueBinding({ ...signals, signals: signals.signals.slice(0, 2) })).toBe(false);
    expect(hasQueueBinding(null)).toBeUndefined();
    expect(queueSuggestionCount(signals)).toBe(2);
    expect(tableSignal(signals, { signal_binding_id: "b-1", data_source_id: "x" })).toBe("queue.consumers");
    expect(tableSignal(signals, { signal_binding_id: "gone", data_source_id: "ds-2" })).toBe("queue.consumers");
    expect(tableSignal(signals, { signal_binding_id: "gone", data_source_id: "ds-1" })).toBeUndefined();
    expect(tableSignal(signals, { signal_binding_id: "b-1", data_source_id: "ds-2", signal: "queue.backlog" })).toBe("queue.backlog");
  });
});
