import { describe, expect, it } from "vitest";
import type { StoreTestBuild, StoreTestGroup } from "@/api";
import {
  autoGroupSet,
  buildsByPlatform,
  isTestBuildActive,
  mergeTestBuilds,
  needsBetaReview,
  testBuildLabel,
  testBuildStatusBadgeVariant,
  testBuildStatusLabelKey,
  testGroupName,
} from "@/components/operations/storeTestBuilds";
import { en } from "@/locales/en";

function build(overrides: Partial<StoreTestBuild> = {}): StoreTestBuild {
  return {
    id: "b1",
    repository_id: "repo-1",
    platform: "ios",
    attempt: 1,
    sequence: 412,
    build_number: "412.54.1",
    status: "queued",
    has_artifact: false,
    groups: null,
    trigger: "human_uat",
    created_at: "2026-10-01T00:00:00Z",
    updated_at: "2026-10-01T00:00:00Z",
    ...overrides,
  };
}

function group(overrides: Partial<StoreTestGroup> = {}): StoreTestGroup {
  return {
    id: "g1",
    name: "QA",
    platform: "ios",
    kind: "internal",
    tester_count: 3,
    auto_distribute: false,
    ...overrides,
  };
}

describe("test build status vocabulary", () => {
  it("has an English label for every status it maps, and an Unknown for the rest", () => {
    const statuses = ["queued", "building", "processing", "action_required", "ready", "failed", "dispatched"];
    for (const status of statuses) {
      const key = testBuildStatusLabelKey(status);
      expect(key).toBe(`operations.storeTest.status.${status}`);
      expect(en.operations.storeTest.status).toHaveProperty(status);
    }
    expect(testBuildStatusLabelKey("exploded")).toBe("operations.storeTest.status.unknown");
  });

  it("colours a verdict only when the store gave one", () => {
    expect(testBuildStatusBadgeVariant("ready")).toBe("success");
    expect(testBuildStatusBadgeVariant("failed")).toBe("destructive");
    expect(testBuildStatusBadgeVariant("action_required")).toBe("warning");
    expect(testBuildStatusBadgeVariant("building")).toBe("info");
    expect(testBuildStatusBadgeVariant("dispatched")).toBe("info");
    expect(testBuildStatusBadgeVariant("exploded")).toBe("outline");
  });

  it("polls only while a build moves on its own", () => {
    expect(isTestBuildActive("queued")).toBe(true);
    expect(isTestBuildActive("processing")).toBe(true);
    expect(isTestBuildActive("action_required")).toBe(false);
    expect(isTestBuildActive("ready")).toBe(false);
    expect(isTestBuildActive("dispatched")).toBe(false);
  });
});

describe("testBuildLabel", () => {
  it("names a build the way the server does", () => {
    expect(testBuildLabel(build({ task_key: "T-54", attempt: 2, commit_sha: "a1b2c3d4e5f6" }))).toBe("T-54 · #2 · a1b2c3d");
    expect(testBuildLabel(build({ attempt: 0, commit_sha: "a1b2c3d4e5f6" }))).toBe("a1b2c3d");
  });
});

describe("buildsByPlatform / mergeTestBuilds", () => {
  it("keeps the newest build per platform as latest and the rest as older attempts", () => {
    const split = buildsByPlatform([
      build({ id: "i2" }),
      build({ id: "a1", platform: "android" }),
      build({ id: "i1" }),
    ]);
    expect(split.ios.latest?.id).toBe("i2");
    expect(split.ios.older.map((b) => b.id)).toEqual(["i1"]);
    expect(split.android.latest?.id).toBe("a1");
  });

  it("replaces a known build in place and puts a new one on top", () => {
    const list = [build({ id: "b2" }), build({ id: "b1" })];
    const merged = mergeTestBuilds(list, [build({ id: "b1", status: "ready" }), build({ id: "b3" })]);
    expect(merged.map((b) => b.id)).toEqual(["b3", "b2", "b1"]);
    expect(merged[2].status).toBe("ready");
  });
});

describe("test groups", () => {
  it("never sends an all_builds group in the auto set", () => {
    const groups = [
      group({ id: "all", all_builds: true, auto_distribute: true }),
      group({ id: "qa", auto_distribute: true }),
      group({ id: "beta", kind: "external" }),
    ];
    expect(autoGroupSet(groups, "beta", true)).toEqual(["qa", "beta"]);
    expect(autoGroupSet(groups, "qa", false)).toEqual([]);
  });

  it("marks only external TestFlight groups as needing Beta App Review", () => {
    expect(needsBetaReview(group({ kind: "external" }))).toBe(true);
    expect(needsBetaReview(group({ kind: "internal" }))).toBe(false);
    expect(needsBetaReview(group({ platform: "android", kind: "open" }))).toBe(false);
  });

  it("falls back to the id, which on Play is the track name", () => {
    expect(testGroupName("g1", [group()])).toBe("QA");
    expect(testGroupName("internal", null)).toBe("internal");
  });
});
