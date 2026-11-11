import { describe, expect, it } from "vitest";
import {
  endpointLabel,
  formatMs,
  formatRatio,
  logSeverity,
  logVolumeBuckets,
  seriesFromSet,
  telemetryHealthStatus,
  telemetryRange,
  toConsoleTrace,
  toLogEntries,
} from "@/lib/telemetryHub";
import { telemetryProxyPath } from "@/api";

const MIN = 60_000;

describe("telemetryRange", () => {
  it("rounds the console's window up to a window the product answers", () => {
    const now = 1_700_000_000_000;
    const of = (ms: number) => telemetryRange({ from: now - ms, to: now });
    expect(of(10 * MIN)).toBe("15m");
    expect(of(30 * MIN)).toBe("30m");
    expect(of(60 * MIN)).toBe("1h");
    expect(of(3 * 60 * MIN)).toBe("6h");
    expect(of(12 * 60 * MIN)).toBe("24h");
    expect(of(72 * 60 * MIN)).toBe("7d");
  });
});

describe("telemetryProxyPath", () => {
  it("drops empty query values and a leading slash", () => {
    expect(telemetryProxyPath("/services", { range: "1h", env: undefined, q: "" })).toBe("/v1/telemetry/proxy/services?range=1h");
    expect(telemetryProxyPath("traces/abc")).toBe("/v1/telemetry/proxy/traces/abc");
  });

  it("encodes values", () => {
    expect(telemetryProxyPath("logs", { q: "a b&c" })).toBe("/v1/telemetry/proxy/logs?q=a+b%26c");
  });
});

describe("seriesFromSet", () => {
  it("pairs timestamps with values and keeps gaps as null", () => {
    const set = { timestamps: [1_700_000_000_000, 1_700_000_060_000], series: [{ id: "rpm", label: "rpm", data: [3, Number.NaN] }, { id: "p95", label: "p95", data: [1, 2] }] };
    const [rpm] = seriesFromSet(set, ["rpm"]);
    expect(rpm.points).toEqual([
      [1_700_000_000_000, 3],
      [1_700_000_060_000, null],
    ]);
    expect(seriesFromSet(set)).toHaveLength(2);
  });

  it("reads second timestamps as milliseconds and survives null arrays", () => {
    const [s] = seriesFromSet({ timestamps: [1_700_000_000], series: [{ id: "a", label: "a", data: null }] });
    expect(s.points[0]).toEqual([1_700_000_000_000, null]);
    expect(seriesFromSet(undefined)).toEqual([]);
    expect(seriesFromSet({ timestamps: null, series: null })).toEqual([]);
  });
});

describe("logVolumeBuckets", () => {
  it("stacks in the histogram's severity order and sums warn aliases", () => {
    const buckets = logVolumeBuckets({
      timestamps: [1_700_000_000_000, 1_700_000_060_000],
      series: [
        { id: "error", label: "error", data: [1, 0] },
        { id: "warn", label: "warn", data: [2, 4] },
        { id: "info", label: "info", data: [5, 6] },
      ],
    });
    expect(buckets[0]).toEqual({ from: 1_700_000_000_000, to: 1_700_000_060_000, counts: [0, 1, 2, 5, 0] });
    expect(buckets[1].counts).toEqual([0, 0, 4, 6, 0]);
  });
});

describe("logs", () => {
  it("maps the product's levels to the console's severities", () => {
    expect(logSeverity("WARN")).toBe("warning");
    expect(logSeverity("fatal")).toBe("critical");
    expect(logSeverity("trace")).toBe("debug");
    expect(logSeverity("whatever")).toBe("info");
  });

  it("builds console entries with no component", () => {
    const [entry] = toLogEntries([{ id: "1", ts: 1_700_000_000_000, level: "error", service: "api", message: "boom", traceId: "t1", route: "/x" }]);
    expect(entry).toMatchObject({ severity: "error", message: "boom", source: "api", trace_id: "t1", path: "/x", component_id: "" });
    expect(entry.timestamp).toBe(new Date(1_700_000_000_000).toISOString());
  });
});

describe("toConsoleTrace", () => {
  it("places spans on the trace's clock and prefixes resource attributes", () => {
    const trace = toConsoleTrace({
      id: "t1",
      ts: 1_700_000_000_000,
      dur: 30,
      errors: 1,
      services: ["api"],
      spans: [
        { spanId: "a", parentSpanId: "0000000000000000", service: "api", name: "GET /x", kind: "server", start: 0, dur: 30, status: 500, failed: true, errorType: "Timeout", attrs: { k: "v" }, resource: { "k8s.pod.name": "p" } },
        { spanId: "b", parentSpanId: "a", service: "db", name: "select", kind: "client", start: 5, dur: 10, status: 0, failed: false },
      ],
    });
    expect(trace.spans[0]).toMatchObject({ id: "a", parent_id: undefined, status: "error", status_message: "Timeout" });
    expect(trace.spans[0].attributes).toMatchObject({ k: "v", "resource.k8s.pod.name": "p", "status.code": "500" });
    expect(trace.spans[1]).toMatchObject({ parent_id: "a", status: "ok", duration_ms: 10 });
    expect(Date.parse(trace.spans[1].start) - Date.parse(trace.spans[0].start)).toBe(5);
  });
});

describe("formatting", () => {
  it("formats ratios and durations", () => {
    expect(formatRatio(0, "en")).toBe("0%");
    expect(formatRatio(0.0123, "en")).toBe("1.23%");
    expect(formatRatio(0.25, "en")).toBe("25%");
    expect(formatMs(0.5, "en")).toBe("500 µs");
    expect(formatMs(120, "en")).toBe("120 ms");
    expect(formatMs(2500, "en")).toBe("2.5 s");
  });

  it("labels endpoints by route or process", () => {
    expect(endpointLabel({ method: "GET", route: "/orders", sub: null })).toBe("GET /orders");
    expect(endpointLabel({ method: "", route: "", sub: { process: "orders-consumer", source: "kafka", topic: "t", lag: 0 } })).toBe("orders-consumer");
  });

  it("maps health to the console's statuses", () => {
    expect(telemetryHealthStatus("critical")).toBe("critical");
    expect(telemetryHealthStatus("warning")).toBe("warning");
    expect(telemetryHealthStatus("ok")).toBe("healthy");
    expect(telemetryHealthStatus(undefined)).toBe("healthy");
  });
});
