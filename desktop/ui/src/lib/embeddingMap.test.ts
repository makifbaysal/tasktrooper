import { describe, expect, it } from "vitest";
import { buildGroupScale, groupColorAt, otherGroupColor } from "@/lib/embeddingMap";

function entries(counts: Record<string, number>) {
  return Object.entries(counts).flatMap(([id, count]) =>
    Array.from({ length: count }, () => ({ groupId: id, label: id })),
  );
}

describe("buildGroupScale", () => {
  it("gives up to eight groups distinct palette colors in count order", () => {
    const counts = Object.fromEntries(Array.from({ length: 8 }, (_, i) => [`g${i}`, 20 - i]));
    const scale = buildGroupScale(entries(counts), "light");
    expect(scale.otherGroupIds.size).toBe(0);
    expect(scale.legend.map((g) => g.id)).toEqual(Object.keys(counts));
    scale.legend.forEach((group, i) => expect(group.color).toBe(groupColorAt(i, "light")));
    expect(new Set(scale.legend.map((g) => g.color)).size).toBe(8);
  });

  it("folds groups past eight into other", () => {
    const counts = Object.fromEntries(Array.from({ length: 10 }, (_, i) => [`g${i}`, 20 - i]));
    const scale = buildGroupScale(entries(counts), "dark");
    expect([...scale.otherGroupIds].sort()).toEqual(["g8", "g9"]);
    expect(scale.legend.length).toBe(8);
    expect(scale.colorByGroupId.get("g9")).toBe(otherGroupColor("dark"));
  });

  it("lists overflow groups on their own neutral rows when folding is off", () => {
    const counts = Object.fromEntries(Array.from({ length: 10 }, (_, i) => [`g${i}`, 20 - i]));
    const scale = buildGroupScale([...entries(counts), ...entries({ noise: 3 })], "light", {
      neutralGroupIds: new Set(["noise"]),
      foldOverflow: false,
    });
    expect(scale.otherGroupIds.size).toBe(0);
    expect(scale.legend.map((g) => g.id)).toEqual([...Object.keys(counts), "noise"]);
    expect(scale.colorByGroupId.get("g8")).toBe(otherGroupColor("light"));
    expect(scale.colorByGroupId.get("g7")).toBe(groupColorAt(7, "light"));
  });

  it("keeps list-index slots for an explicit order", () => {
    const scale = buildGroupScale(entries({ c: 3, d: 2 }), "light", { order: ["a", "b", "c", "d"] });
    expect(scale.colorByGroupId.get("c")).toBe(groupColorAt(2, "light"));
    expect(scale.colorByGroupId.get("d")).toBe(groupColorAt(3, "light"));
    expect(scale.otherGroupIds.size).toBe(0);
  });

  it("draws neutral groups without consuming a slot", () => {
    const scale = buildGroupScale(entries({ noise: 50, a: 10, b: 5 }), "light", {
      neutralGroupIds: new Set(["noise"]),
    });
    expect(scale.colorByGroupId.get("noise")).toBe(otherGroupColor("light"));
    expect(scale.colorByGroupId.get("a")).toBe(groupColorAt(0, "light"));
    expect(scale.colorByGroupId.get("b")).toBe(groupColorAt(1, "light"));
    expect(scale.legend.map((g) => g.id)).toEqual(["a", "b", "noise"]);
    expect(scale.otherGroupIds.size).toBe(0);
  });
});

describe("groupColorAt", () => {
  it("returns the neutral past the palette", () => {
    expect(groupColorAt(8, "light")).toBe(otherGroupColor("light"));
  });
});
