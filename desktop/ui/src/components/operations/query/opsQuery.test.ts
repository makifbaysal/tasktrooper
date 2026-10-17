import { describe, expect, it } from "vitest";
import type { OpsLogEntry } from "@/api";
import {
  appendOlder,
  compareTimestamps,
  errorsWindow,
  formatIdList,
  mergeNewer,
  newestTimestamp,
  oldestTimestamp,
  parseIdList,
  tailSince,
  windowKey,
} from "@/components/operations/query/opsQuery";

const DAY = 86_400_000;
const NOW = Date.parse("2026-10-16T12:00:00Z");

function entry(timestamp: string, message: string, overrides: Partial<OpsLogEntry> = {}): OpsLogEntry {
  return { timestamp, severity: "info", message, component_id: "c-api", environment_id: "e-api", ...overrides };
}

describe("errorsWindow", () => {
  it("reads a preset window from its start, open to now", () => {
    expect(errorsWindow({ from: NOW - DAY, to: NOW }, NOW)).toEqual({ since: NOW - DAY, clamped: false, openEnded: false });
  });

  it("clamps a range further back than 30 days and says so", () => {
    const w = errorsWindow({ from: NOW - 45 * DAY, to: NOW }, NOW);
    expect(w.since).toBe(NOW - 30 * DAY);
    expect(w.clamped).toBe(true);
  });

  it("flags a custom range that ends in the past: errors have no end time", () => {
    expect(errorsWindow({ from: NOW - 3 * DAY, to: NOW - DAY }, NOW).openEnded).toBe(true);
  });
});

describe("query params", () => {
  it("keys a sliding preset by its name and a custom window by its bounds", () => {
    expect(windowKey({ preset: "1h", from: 1, to: 2 })).toBe("1h");
    expect(windowKey({ preset: "custom", from: 1, to: 2 })).toBe("custom:1-2");
  });

  it("reads and writes the entity list without blanks or repeats", () => {
    expect(parseIdList(" a, b,,a ")).toEqual(["a", "b"]);
    expect(parseIdList(null)).toEqual([]);
    expect(formatIdList(["a", "a", "b"])).toBe("a,b");
    expect(formatIdList([])).toBeNull();
  });
});

describe("timestamps", () => {
  it("orders below the millisecond", () => {
    expect(compareTimestamps("2026-10-16T10:00:00.123456789Z", "2026-10-16T10:00:00.123456Z")).toBeGreaterThan(0);
    expect(compareTimestamps("2026-10-16T10:00:00.1Z", "2026-10-16T10:00:00.100000000Z")).toBe(0);
    expect(compareTimestamps("2026-10-16T13:00:00+03:00", "2026-10-16T10:00:00Z")).toBe(0);
  });

  it("finds the oldest and newest shown, keeping the string as returned", () => {
    const shown = [entry("2026-10-16T10:00:02.000000500Z", "b"), entry("2026-10-16T10:00:01.000000001Z", "a")];
    expect(oldestTimestamp(shown)).toBe("2026-10-16T10:00:01.000000001Z");
    expect(newestTimestamp(shown)).toBe("2026-10-16T10:00:02.000000500Z");
  });
});

describe("appendOlder", () => {
  const boundary = "2026-10-16T10:00:00.000000123Z";

  it("drops the entries an inclusive `until` sends back again", () => {
    const shown = [entry("2026-10-16T10:05:00Z", "newest"), entry(boundary, "edge-1"), entry(boundary, "edge-2")];
    const older = [entry(boundary, "edge-1"), entry(boundary, "edge-2"), entry(boundary, "edge-3"), entry("2026-10-16T09:59:00Z", "older")];
    const merged = appendOlder(shown, older);
    expect(merged.added).toBe(2);
    expect(merged.entries.map((e) => e.message)).toEqual(["newest", "edge-1", "edge-2", "edge-3", "older"]);
  });

  it("keeps two identical lines at one instant, as many as were not shown yet", () => {
    const shown = [entry(boundary, "same")];
    const merged = appendOlder(shown, [entry(boundary, "same"), entry(boundary, "same")]);
    expect(merged.added).toBe(1);
    expect(merged.entries).toHaveLength(2);
  });

  it("tells environments apart at the same instant", () => {
    const shown = [entry(boundary, "x")];
    const merged = appendOlder(shown, [entry(boundary, "x", { environment_id: "e-web" })]);
    expect(merged.added).toBe(1);
  });

  it("adds nothing and keeps the list when the page holds only repeats", () => {
    const shown = [entry(boundary, "x")];
    const merged = appendOlder(shown, [entry(boundary, "x")]);
    expect(merged.added).toBe(0);
    expect(merged.entries).toBe(shown);
  });
});

describe("mergeNewer", () => {
  const since = "2026-10-16T09:59:55Z";

  it("drops what the overlap read again and puts the rest on top", () => {
    const newest = "2026-10-16T10:00:00Z";
    const shown = [entry(newest, "a"), entry("2026-10-16T09:59:58Z", "near"), entry("2026-10-16T09:00:00Z", "old")];
    const merged = mergeNewer(shown, [entry("2026-10-16T10:00:05Z", "b"), entry(newest, "a"), entry("2026-10-16T09:59:58Z", "near")], since);
    expect(merged.entries.map((e) => e.message)).toEqual(["b", "a", "near", "old"]);
    expect(merged.added).toBe(1);
    expect(merged.trimmed).toBe(false);
  });

  it("files a late-ingested entry by its time, below newer ones already shown", () => {
    const shown = [entry("2026-10-16T10:00:00Z", "a"), entry("2026-10-16T09:00:00Z", "old")];
    const merged = mergeNewer(shown, [entry("2026-10-16T10:00:00Z", "a"), entry("2026-10-16T09:59:57Z", "late")], since);
    expect(merged.entries.map((e) => e.message)).toEqual(["a", "late", "old"]);
    expect(merged.added).toBe(1);
  });

  it("keeps two identical lines at one instant, but neither twice", () => {
    const at = "2026-10-16T10:00:00Z";
    const merged = mergeNewer([entry(at, "x")], [entry(at, "x"), entry(at, "x")], since);
    expect(merged.entries).toHaveLength(2);
  });

  it("drops the oldest past the cap and says it did", () => {
    const shown = [entry("2026-10-16T10:00:00Z", "a"), entry("2026-10-16T09:00:00Z", "b")];
    const merged = mergeNewer(shown, [entry("2026-10-16T10:01:00Z", "c")], since, 2);
    expect(merged.entries.map((e) => e.message)).toEqual(["c", "a"]);
    expect(merged.trimmed).toBe(true);
  });
});

describe("tailSince", () => {
  const now = Date.parse("2026-10-16T12:00:00Z");

  it("starts a few seconds before the newest shown", () => {
    expect(tailSince("2026-10-16T11:50:00.123456789Z", 0, now, 5_000, 1_000)).toBe("2026-10-16T11:49:55.123Z");
    expect(tailSince(undefined, now - 60_000, now, 5_000, 1_000)).toBe("2026-10-16T11:59:00.000Z");
  });

  it("never reaches the server's now, whatever the clocks say", () => {
    expect(tailSince("2026-10-16T12:00:30Z", 0, now, 5_000, 1_000)).toBe("2026-10-16T11:59:59.000Z");
    expect(tailSince(undefined, now, now, 5_000, 1_000)).toBe("2026-10-16T11:59:59.000Z");
  });
});
