import { describe, expect, it } from "vitest";
import { groupMembers, nearestMembers, placeMarkers, topFiles } from "@/lib/embeddingMapSearch";

describe("placeMarkers", () => {
  const positions = new Float32Array([0.1, 0.2, 0.5, 0.5]);

  it("ranks by hit order and skips hits without an anchor", () => {
    const markers = placeMarkers(
      [{ id: "a" }, { id: "missing" }, { id: "b" }],
      new Map([
        ["a", 0],
        ["b", 1],
      ]),
      positions,
    );
    expect(markers.map((m) => [m.hitId, m.rank])).toEqual([
      ["a", 1],
      ["b", 3],
    ]);
    expect(markers[0].x).toBeCloseTo(0.1);
    expect(markers[1].y).toBeCloseTo(0.5);
  });

  it("spreads hits that share an anchor deterministically", () => {
    const anchors = new Map([
      ["a", 0],
      ["b", 0],
      ["c", 0],
    ]);
    const hits = [{ id: "a" }, { id: "b" }, { id: "c" }];
    const first = placeMarkers(hits, anchors, positions);
    const again = placeMarkers(hits, anchors, positions);
    expect(first).toEqual(again);
    expect(first[0].x).toBeCloseTo(0.1);
    expect(Math.hypot(first[1].x - 0.1, first[1].y - 0.2)).toBeCloseTo(0.006, 5);
    expect(Math.hypot(first[2].x - 0.1, first[2].y - 0.2)).toBeCloseTo(0.012, 5);
  });
});

describe("group helpers", () => {
  it("lists members of a group", () => {
    expect(groupMembers(["a", "b", "a"], "a")).toEqual([0, 2]);
  });

  it("orders files by count then path", () => {
    const paths = ["b.go", "a.go", "b.go", "c.go", "a.go"];
    expect(topFiles(paths, [0, 1, 2, 3, 4])).toEqual([
      { path: "a.go", count: 2 },
      { path: "b.go", count: 2 },
      { path: "c.go", count: 1 },
    ]);
    expect(topFiles(paths, [0, 1, 2, 3, 4], 1)).toHaveLength(1);
  });

  it("picks the members closest to their mean", () => {
    const positions = new Float32Array([0, 0, 0.5, 0.5, 0.52, 0.5, 1, 1]);
    expect(nearestMembers(positions, [0, 1, 2, 3], 2)).toEqual([1, 2]);
    expect(nearestMembers(positions, [])).toEqual([]);
  });
});
