import { describe, expect, it } from "vitest";
import { keepEqual, keepMap, keepRows, sameRows } from "@/lib/stableState";

const byId = (row: { id: string; status: string }) => `${row.id}:${row.status}`;

describe("sameRows / keepRows", () => {
  it("keeps the previous array when every row has the same key in the same order", () => {
    const prev = [{ id: "a", status: "running" }, { id: "b", status: "done" }];
    const next = [{ id: "a", status: "running" }, { id: "b", status: "done" }];
    expect(sameRows(prev, next, byId)).toBe(true);
    expect(keepRows(prev, next, byId)).toBe(prev);
  });

  it("takes the new array when a key, the order or the length changed", () => {
    const prev = [{ id: "a", status: "running" }, { id: "b", status: "done" }];
    const changed = [{ id: "a", status: "completed" }, { id: "b", status: "done" }];
    const reordered = [prev[1], prev[0]];
    const longer = [...prev, { id: "c", status: "pending" }];
    expect(keepRows(prev, changed, byId)).toBe(changed);
    expect(keepRows(prev, reordered, byId)).toBe(reordered);
    expect(keepRows(prev, longer, byId)).toBe(longer);
  });

  it("treats two empty arrays as the same", () => {
    const prev: { id: string; status: string }[] = [];
    expect(keepRows(prev, [], byId)).toBe(prev);
  });
});

describe("keepEqual", () => {
  it("keeps the previous value when the payload is structurally equal", () => {
    const prev = { items: [{ id: "x", n: 1 }], total: 1 };
    expect(keepEqual(prev, { items: [{ id: "x", n: 1 }], total: 1 })).toBe(prev);
  });

  it("takes the new value when anything differs, including null", () => {
    const prev = { items: [{ id: "x", n: 1 }] };
    const next = { items: [{ id: "x", n: 2 }] };
    expect(keepEqual(prev, next)).toBe(next);
    expect(keepEqual<typeof prev | null>(prev, null)).toBeNull();
  });
});

describe("keepMap", () => {
  const key = (release: { id: string; status: string }) => `${release.id}:${release.status}`;

  it("keeps the previous map when every entry is equal under the key", () => {
    const prev = new Map([["t1", { id: "r1", status: "verifying" }]]);
    const next = new Map([["t1", { id: "r1", status: "verifying" }]]);
    expect(keepMap(prev, next, key)).toBe(prev);
  });

  it("takes the new map when an entry changed, appeared or went away", () => {
    const prev = new Map([["t1", { id: "r1", status: "verifying" }]]);
    const changed = new Map([["t1", { id: "r1", status: "released" }]]);
    const added = new Map([...prev, ["t2", { id: "r2", status: "deploying" }]]);
    const swapped = new Map([["t2", { id: "r1", status: "verifying" }]]);
    expect(keepMap(prev, changed, key)).toBe(changed);
    expect(keepMap(prev, added, key)).toBe(added);
    expect(keepMap(prev, swapped, key)).toBe(swapped);
  });
});
