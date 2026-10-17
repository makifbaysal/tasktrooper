import { describe, expect, it } from "vitest";
import { goldenColumnsFor, roleHasGolden } from "@/components/operations/console/entityTypes";

const ids = (specs: Array<{ id: string }>) => specs.map((s) => s.id);

describe("goldenColumnsFor", () => {
  it("gives a type list the golden signals of every role it lists, the lead role's first", () => {
    expect(ids(goldenColumnsFor("mobile"))).toEqual(["mobile.crash_free_users", "mobile.cold_start", "mobile.anr_rate", "mobile.crash_rate"]);
    expect(ids(goldenColumnsFor("services"))).toEqual(["http.latency", "http.requests", "http.error_ratio"]);
    expect(goldenColumnsFor("other")).toEqual([]);
  });

  it("shows only the columns some listed row has", () => {
    expect(ids(goldenColumnsFor("mobile", ["mobile"]))).toEqual(["mobile.crash_free_users", "mobile.cold_start", "mobile.anr_rate"]);
    expect(ids(goldenColumnsFor("mobile", ["desktop"]))).toEqual(["mobile.crash_free_users", "mobile.crash_rate", "mobile.cold_start"]);
    expect(ids(goldenColumnsFor("mobile", []))).toHaveLength(4);
  });

  it("knows which column is a row's own golden signal", () => {
    expect(roleHasGolden("desktop", "mobile.crash_rate")).toBe(true);
    expect(roleHasGolden("desktop", "mobile.anr_rate")).toBe(false);
    expect(roleHasGolden("mobile", "mobile.anr_rate")).toBe(true);
    expect(roleHasGolden(undefined, "mobile.anr_rate")).toBe(false);
  });
});
