import { describe, expect, it } from "vitest";
import {
  flameCells,
  flameNodeAt,
  formatBytes,
  formatDuration,
  formatProfileValue,
  infraOpsStatus,
  mapLayout,
  runSeriesPoints,
  usagePercent,
  usageTone,
  vitalRating,
  type TelemetryFlameNode,
} from "@/lib/telemetryViews";

describe("infra status", () => {
  it("reads stale, idle and missing as not reporting", () => {
    expect(infraOpsStatus("critical")).toBe("critical");
    expect(infraOpsStatus("ok")).toBe("healthy");
    expect(infraOpsStatus("stale")).toBe("notReporting");
    expect(infraOpsStatus("idle")).toBe("notReporting");
    expect(infraOpsStatus(undefined)).toBe("notReporting");
  });
});

describe("service map layout", () => {
  const node = (id: string, tier: string, status: "ok" | "warning" | "critical" = "ok", rpm = 1) => ({ id, name: id, tier, kind: "http", status, rpm, errorRate: 0, p95: 0 });

  it("puts tiers in columns left to right, worst first, and skips empty tiers", () => {
    const layout = mapLayout({
      nodes: [node("db", "data"), node("web", "client"), node("ok-api", "service", "ok", 100), node("bad-api", "service", "critical", 1)],
      edges: [
        { id: "e1", from: "web", to: "bad-api", callsPerMin: 5, errorRate: 0, p95: 0, status: "ok" },
        { id: "e2", from: "bad-api", to: "db", callsPerMin: 5, errorRate: 0, p95: 0, status: "ok" },
        { id: "e3", from: "bad-api", to: "gone", callsPerMin: 5, errorRate: 0, p95: 0, status: "ok" },
      ],
    });
    expect(layout.columns.map((c) => c.tier)).toEqual(["client", "service", "data"]);
    const services = layout.nodes.filter((n) => n.tier === "service");
    expect(services.map((n) => n.id)).toEqual(["bad-api", "ok-api"]);
    expect(services[0].y).toBeLessThan(services[1].y);
    expect(layout.edges.map((e) => e.id)).toEqual(["e1", "e2"]);
    expect(layout.edges[0].path.startsWith("M ")).toBe(true);
  });

  it("is empty without nodes", () => {
    expect(mapLayout({ nodes: null, edges: null }).nodes).toEqual([]);
  });
});

describe("flame graph", () => {
  const root: TelemetryFlameNode = {
    name: "all",
    value: 100,
    self: 0,
    children: [
      { name: "main", value: 90, self: 10, children: [{ name: "parse", value: 80, self: 80, children: null }] },
      { name: "tiny", value: 0.1, self: 0.1, children: null },
      { name: "gc", value: 9.9, self: 9.9, children: null },
    ],
  };

  it("lays out cells by share, dropping the ones too narrow to see", () => {
    const cells = flameCells(root);
    expect(cells.map((c) => c.name)).toEqual(["all", "main", "parse", "gc"]);
    const gc = cells.find((c) => c.name === "gc");
    expect(gc?.x).toBeCloseTo(0.901);
    expect(gc?.depth).toBe(1);
  });

  it("zooms to a path and falls back to the root when it no longer exists", () => {
    const zoomed = flameCells(root, [0]);
    expect(zoomed[0]).toMatchObject({ name: "main", w: 1, depth: 0 });
    expect(zoomed[1].w).toBeCloseTo(80 / 90);
    expect(flameNodeAt(root, [5, 1]).path).toEqual([]);
  });

  it("formats nanoseconds and bytes", () => {
    expect(formatProfileValue(2_500_000_000, "nanoseconds", "en")).toBe("2.5 s");
    expect(formatProfileValue(3_000_000, "nanoseconds", "en")).toBe("3 ms");
    expect(formatProfileValue(2048, "bytes", "en")).toBe("2 KB");
  });
});

describe("formatting", () => {
  it("formats bytes, durations and usage", () => {
    expect(formatBytes(512, "en")).toBe("512 B");
    expect(formatBytes(1536 * 1024, "en")).toBe("1.5 MB");
    expect(formatDuration(90_061)).toBe("1d 1h");
    expect(formatDuration(3725)).toBe("1h 2m");
    expect(formatDuration(42)).toBe("42s");
    expect(usagePercent(3, 4)).toBe(75);
    expect(usagePercent(1, 0)).toBe(0);
    expect(usageTone(95)).toBe("destructive");
    expect(usageTone(80)).toBe("warning");
    expect(usageTone(10)).toBe("success");
  });

  it("rates a vital against its thresholds", () => {
    expect(vitalRating({ value: 2000, good: 2500, poor: 4000 })).toBe("good");
    expect(vitalRating({ value: 3000, good: 2500, poor: 4000 })).toBe("needsImprovement");
    expect(vitalRating({ value: 5000, good: 2500, poor: 4000 })).toBe("poor");
  });

  it("turns a run timeline column into chart points", () => {
    expect(runSeriesPoints({ ts: [1_700_000_000, 1_700_000_005], vus: [1, 2], rps: null, p95: null, errorRate: [0.1, Number.NaN] }, "errorRate", 100)).toEqual([
      [1_700_000_000_000, 10],
      [1_700_000_005_000, null],
    ]);
  });
});
