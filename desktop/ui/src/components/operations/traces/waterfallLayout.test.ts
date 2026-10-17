import { describe, expect, it } from "vitest";
import type { TraceSpan } from "@/api";
import {
  MIN_BAR_WIDTH,
  SERVICE_SLOTS,
  buildWaterfall,
  formatSpanDuration,
  normalizeTraceId,
  parseMinDuration,
  spanAttributes,
  spanStartMs,
  traceWindow,
  visibleRows,
  waterfallTicks,
} from "@/components/operations/traces/waterfallLayout";

const T0 = "2026-10-17T10:00:00";

function span(id: string, startMs: number, durationMs: number, overrides: Partial<TraceSpan> = {}): TraceSpan {
  const at = new Date(Date.parse(`${T0}Z`) + startMs).toISOString();
  return { id, service: "api", name: id, start: at, duration_ms: durationMs, status: "ok", attributes: {}, ...overrides };
}

describe("buildWaterfall", () => {
  it("lists spans in tree order, children under their parent by start, with depth", () => {
    const wf = buildWaterfall({
      spans: [
        span("db", 40, 20, { parent_id: "handler", service: "db" }),
        span("root", 0, 100),
        span("handler", 10, 80, { parent_id: "root" }),
        span("cache", 15, 5, { parent_id: "handler", service: "cache" }),
      ],
    });
    expect(wf.rows.map((r) => [r.span.id, r.depth])).toEqual([
      ["root", 0],
      ["handler", 1],
      ["cache", 2],
      ["db", 2],
    ]);
    expect(wf.rows.find((r) => r.span.id === "handler")?.childCount).toBe(2);
    expect(wf.rows.find((r) => r.span.id === "db")?.ancestors).toEqual(["root", "handler"]);
  });

  it("places each bar on the trace's time axis", () => {
    const wf = buildWaterfall({ spans: [span("root", 0, 200), span("child", 50, 100, { parent_id: "root" })] });
    expect(wf.durationMs).toBe(200);
    const child = wf.rows[1];
    expect(child.offsetMs).toBe(50);
    expect(child.left).toBeCloseTo(0.25);
    expect(child.width).toBeCloseTo(0.5);
    expect(wf.rows[0]).toMatchObject({ left: 0, width: 1 });
  });

  it("keeps a tiny span visible and inside the axis", () => {
    const wf = buildWaterfall({ spans: [span("root", 0, 1000), span("blip", 1000, 0, { parent_id: "root" })] });
    const blip = wf.rows[1];
    expect(blip.width).toBe(MIN_BAR_WIDTH);
    expect(blip.left + blip.width).toBeLessThanOrEqual(1);
  });

  it("starts a tree of its own for a span whose parent the trace lacks, and survives a parent cycle", () => {
    const wf = buildWaterfall({
      spans: [span("a", 0, 10, { parent_id: "b" }), span("b", 5, 10, { parent_id: "a" }), span("orphan", 2, 3, { parent_id: "gone" })],
    });
    expect(wf.rows).toHaveLength(3);
    expect(wf.rows.find((r) => r.span.id === "orphan")?.depth).toBe(0);
    expect(new Set(wf.rows.map((r) => r.span.id))).toEqual(new Set(["a", "b", "orphan"]));
  });

  it("gives each service a colour slot in order of appearance, cycling past the slots, and counts errors", () => {
    const spans = Array.from({ length: SERVICE_SLOTS + 1 }, (_, i) =>
      span(`s${i}`, i, 1, { service: `svc-${i}`, status: i === 2 ? "error" : "ok" }),
    );
    const wf = buildWaterfall({ spans });
    expect(wf.services.map((s) => s.colorIndex)).toEqual([1, 2, 3, 4, 5, 6, 7, 1]);
    expect(wf.errors).toBe(1);
    expect(wf.rows.find((r) => r.span.id === "s2")?.error).toBe(true);
    expect(wf.services[2]).toMatchObject({ name: "svc-2", spans: 1, errors: 1 });
  });

  it("answers an empty waterfall for no spans or unreadable starts", () => {
    expect(buildWaterfall({ spans: [] }).rows).toEqual([]);
    expect(buildWaterfall({ spans: [{ ...span("x", 0, 1), start: "not a time" }] }).rows).toEqual([]);
  });
});

describe("waterfall helpers", () => {
  it("keeps sub-millisecond starts apart", () => {
    expect(spanStartMs(`${T0}.000250Z`) - spanStartMs(`${T0}.000Z`)).toBeCloseTo(0.25, 6);
    expect(spanStartMs(`${T0}.123456789Z`)).toBeCloseTo(Date.parse(`${T0}.123Z`) + 0.456789, 6);
  });

  it("hides the rows under a folded span", () => {
    const wf = buildWaterfall({
      spans: [span("root", 0, 10), span("a", 1, 5, { parent_id: "root" }), span("a1", 2, 1, { parent_id: "a" }), span("b", 6, 2, { parent_id: "root" })],
    });
    expect(visibleRows(wf.rows, new Set(["a"])).map((r) => r.span.id)).toEqual(["root", "a", "b"]);
    expect(visibleRows(wf.rows, new Set(["root"])).map((r) => r.span.id)).toEqual(["root"]);
  });

  it("marks round times across the trace", () => {
    expect(waterfallTicks(1000).map((t) => t.ms)).toEqual([0, 200, 400, 600, 800, 1000]);
    expect(waterfallTicks(730).map((t) => t.ms)).toEqual([0, 200, 400, 600]);
    expect(waterfallTicks(0)).toEqual([{ ms: 0, at: 0 }]);
  });

  it("formats durations as people read them", () => {
    expect(formatSpanDuration(0.25, "en")).toBe("250 µs");
    expect(formatSpanDuration(3.456, "en")).toBe("3.46 ms");
    expect(formatSpanDuration(250, "en")).toBe("250 ms");
    expect(formatSpanDuration(1534, "en")).toBe("1.53 s");
    expect(formatSpanDuration(90_000, "en")).toBe("1.5 min");
  });

  it("splits a span's own attributes from its resource's", () => {
    expect(spanAttributes({ attributes: { "http.route": "/x", "resource.service.name": "api", "a.b": "1" } })).toEqual({
      span: [
        ["a.b", "1"],
        ["http.route", "/x"],
      ],
      resource: [["service.name", "api"]],
    });
    expect(spanAttributes({ attributes: null })).toEqual({ span: [], resource: [] });
  });

  it("cuts the search window to the last 7 days of the range", () => {
    const to = Date.parse("2026-10-17T00:00:00Z");
    expect(traceWindow({ from: to - 3_600_000, to })).toEqual({ since: to - 3_600_000, until: to, clamped: false });
    const long = traceWindow({ from: to - 30 * 86_400_000, to });
    expect(long).toEqual({ since: to - 7 * 86_400_000, until: to, clamped: true });
  });

  it("reads a minimum duration and a trace id the way the server takes them", () => {
    expect(parseMinDuration("250")).toBe(250);
    expect(parseMinDuration(" 12.6 ")).toBe(13);
    expect(parseMinDuration("0")).toBeUndefined();
    expect(parseMinDuration("-5")).toBeUndefined();
    expect(parseMinDuration("fast")).toBeUndefined();
    expect(normalizeTraceId("ABCDEF0123")).toBe("abcdef0123");
    expect(normalizeTraceId("x".repeat(4))).toBeUndefined();
    expect(normalizeTraceId("a".repeat(33))).toBeUndefined();
  });
});
