import { describe, expect, it } from "vitest";
import { OPS_GOLDEN_MAX_WINDOW_MS, type OpsGolden } from "@/api";
import { goldenCell, goldenWindow, indexGolden } from "@/components/operations/console/goldenMetrics";

const DAY = 24 * 3_600_000;

const answer: OpsGolden = {
  generated_at: "",
  components: [
    {
      component_id: "c-api",
      signals: [
        { signal: "http.requests", unit: "req/s", value: 35, previous: 30, spark: [[1, 35]] },
        { signal: "http.error_ratio", unit: "%", bound: false, value: null, previous: null, spark: [] },
        { signal: "http.latency", unit: "ms", bound: true, value: null, previous: null, spark: [] },
      ],
    },
    { component_id: "c-old", signals: [{ signal: "http.requests", unit: "req/s", value: null, previous: null, spark: [] }] },
  ],
};

describe("goldenWindow", () => {
  it("keeps a window the server reads, and asks for only the last 30 days of a longer one", () => {
    expect(goldenWindow({ from: 0, to: 7 * DAY })).toEqual({ from: 0, to: 7 * DAY, clamped: false });
    expect(goldenWindow({ from: 0, to: 45 * DAY })).toEqual({ from: 15 * DAY, to: 45 * DAY, clamped: true });
    expect(OPS_GOLDEN_MAX_WINDOW_MS).toBe(30 * DAY);
  });
});

describe("goldenCell", () => {
  const index = indexGolden(answer);
  const read = { data: answer, loading: false };

  it("tells a value, an unbound signal, a bound one with no data and an unflagged null apart", () => {
    expect(goldenCell(read, index, "c-api", "http.requests")).toMatchObject({ kind: "value", value: { value: 35 } });
    expect(goldenCell(read, index, "c-api", "http.error_ratio")).toEqual({ kind: "unbound" });
    expect(goldenCell(read, index, "c-api", "http.latency")).toEqual({ kind: "noData" });
    expect(goldenCell(read, index, "c-old", "http.requests")).toEqual({ kind: "unknown" });
  });

  it("reads a component the answer leaves out as unbound, but never while the read is in flight or failed", () => {
    expect(goldenCell(read, index, "c-web", "web.lcp")).toEqual({ kind: "unbound" });
    expect(goldenCell({ data: null, loading: true }, indexGolden(null), "c-api", "http.requests")).toEqual({ kind: "loading" });
    expect(goldenCell({ data: null, loading: false, error: "timeout" }, indexGolden(null), "c-api", "http.requests")).toEqual({
      kind: "failed",
      error: "timeout",
    });
    expect(goldenCell({ ...read, error: "refresh failed" }, index, "c-api", "http.requests")).toMatchObject({ kind: "value" });
  });
});
