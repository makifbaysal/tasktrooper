import { describe, expect, it } from "vitest";
import { ApiError, type StoreTracks, type TrackRelease } from "@/api";
import {
  androidRolloutState,
  cleanServerMessage,
  describeRefusal,
  formatPercent,
  iosProductionState,
  isValidRolloutTarget,
  orphanStoreAppRows,
  parseRolloutTarget,
  phasedReleaseDay,
  pickApp,
  promoteStep,
  promoteSteps,
  rolloutPercent,
} from "@/components/operations/releases/mobileReleases";

const empty: TrackRelease = { has_release: false, status: "none" };
const rel = (overrides: Partial<TrackRelease> = {}): TrackRelease => ({ has_release: true, version: "1.0", build: "10", status: "live", ...overrides });
const tracks = (overrides: Partial<StoreTracks> = {}): StoreTracks => ({ internal: rel(), external: rel(), production: rel(), ...overrides });

describe("rolloutPercent", () => {
  it("reads a completed Play release with no fraction as everyone", () => {
    expect(rolloutPercent(rel({ status: "live" }))).toBe(100);
  });

  it("reads a staged or halted release from its fraction", () => {
    expect(rolloutPercent(rel({ status: "rolling_out", user_fraction: 0.2 }))).toBe(20);
    expect(rolloutPercent(rel({ status: "halted", user_fraction: 0.05 }))).toBe(5);
    expect(rolloutPercent(rel({ status: "rolling_out", user_fraction: 1 }))).toBe(100);
  });

  it("says nobody for a draft or one in review, and nothing for an empty channel or a fraction the store left out", () => {
    expect(rolloutPercent(rel({ status: "draft" }))).toBe(0);
    expect(rolloutPercent(rel({ status: "in_review" }))).toBe(0);
    expect(rolloutPercent(empty)).toBeNull();
    expect(rolloutPercent(rel({ status: "halted" }))).toBeNull();
  });

  it("formats a small share with one decimal and a large one whole", () => {
    expect(formatPercent(0.5, "en")).toBe("0.5");
    expect(formatPercent(2, "en")).toBe("2");
    expect(formatPercent(33.333, "en")).toBe("33");
  });
});

describe("phasedReleaseDay", () => {
  it("maps Apple's daily shares back to the day", () => {
    expect(phasedReleaseDay("ios", rel({ status: "rolling_out", user_fraction: 0.01 }))).toBe(1);
    expect(phasedReleaseDay("ios", rel({ status: "rolling_out", user_fraction: 0.2 }))).toBe(5);
    expect(phasedReleaseDay("ios", rel({ status: "halted", user_fraction: 0.5 }))).toBe(6);
  });

  it("is nothing for Play, a share that is not one of the days, or a release not phasing", () => {
    expect(phasedReleaseDay("android", rel({ status: "rolling_out", user_fraction: 0.2 }))).toBeNull();
    expect(phasedReleaseDay("ios", rel({ status: "rolling_out", user_fraction: 0.33 }))).toBeNull();
    expect(phasedReleaseDay("ios", rel({ status: "live" }))).toBeNull();
  });
});

describe("promoteSteps", () => {
  it("only ever promotes into the next channel", () => {
    const steps = promoteSteps({ state: "live" }, tracks());
    expect(steps.map((s) => [s.from, s.to])).toEqual([
      ["internal", "external"],
      ["external", "production"],
    ]);
    expect(promoteStep({ state: "live" }, tracks(), "production")).toBeNull();
    expect(steps.some((s) => s.from === "internal" && s.to === "production")).toBe(false);
  });

  it("asks for the typed phrase only on the way into production", () => {
    const [toExternal, toProduction] = promoteSteps({ state: "live" }, tracks());
    expect(toExternal.needsPhrase).toBe(false);
    expect(toProduction.needsPhrase).toBe(true);
  });

  it("blocks an empty source, production before live, and anything before onboarding is done", () => {
    expect(promoteStep({ state: "live" }, tracks({ internal: empty }), "internal")?.blocked).toBe("emptySource");
    expect(promoteStep({ state: "test_ready" }, tracks(), "external")?.blocked).toBe("notLive");
    expect(promoteStep({ state: "test_ready" }, tracks(), "internal")?.blocked).toBeNull();
    expect(promoteStep({ state: "onboarding" }, tracks(), "internal")?.blocked).toBe("notReady");
  });
});

describe("androidRolloutState", () => {
  it("lets a rollout in progress grow or halt, and a halted one only resume", () => {
    expect(androidRolloutState({ state: "live" }, rel({ status: "rolling_out", user_fraction: 0.1 }))).toEqual({
      current: 10,
      change: null,
      halt: null,
      resume: "notHalted",
    });
    expect(androidRolloutState({ state: "live" }, rel({ status: "halted", user_fraction: 0.1 }))).toMatchObject({
      change: "halted",
      halt: "alreadyHalted",
      resume: null,
    });
  });

  it("closes everything before the app is live", () => {
    expect(androidRolloutState({ state: "test_ready" }, rel({ status: "rolling_out", user_fraction: 0.1 }))).toMatchObject({
      change: "notLive",
      halt: "notLive",
      resume: "notLive",
    });
  });

  it("only grows the share, up to everyone", () => {
    expect(isValidRolloutTarget(20, 10)).toBe(true);
    expect(isValidRolloutTarget(10, 10)).toBe(false);
    expect(isValidRolloutTarget(5, 10)).toBe(false);
    expect(isValidRolloutTarget(101, 10)).toBe(false);
    expect(isValidRolloutTarget(0, null)).toBe(false);
  });
});

describe("parseRolloutTarget", () => {
  it("takes a share in hundredths of a percent, comma or dot", () => {
    expect(parseRolloutTarget("12.5")).toEqual({ percent: 12.5, fraction: 0.125 });
    expect(parseRolloutTarget("0,5")).toEqual({ percent: 0.5, fraction: 0.005 });
    expect(parseRolloutTarget("100")).toEqual({ percent: 100, fraction: 1 });
  });

  it("refuses what rounds to nothing (Play's 'completed'), more than everyone, and not-a-number", () => {
    expect(parseRolloutTarget("0.004")).toBeNull();
    expect(parseRolloutTarget("0")).toBeNull();
    expect(parseRolloutTarget("100.01")).toBeNull();
    expect(parseRolloutTarget("ten")).toBeNull();
    expect(parseRolloutTarget(" ")).toBeNull();
  });

  it("reads a Play share without floating-point noise", () => {
    expect(rolloutPercent(rel({ status: "rolling_out", user_fraction: 0.07 }))).toBe(7);
    expect(rolloutPercent(rel({ status: "rolling_out", user_fraction: 0.29 }))).toBe(29);
  });
});

describe("iosProductionState", () => {
  const live = { state: "live" as const };

  it("submits a drafted version and releases an approved one, from the channel as read", () => {
    expect(iosProductionState(live, rel({ status: "live", pending_version: "2.0", pending_status: "draft" }))).toEqual({
      queued: { version: "2.0", status: "draft" },
      submit: null,
      release: "notApproved",
    });
    expect(iosProductionState(live, rel({ status: "live", pending_version: "2.0", pending_status: "approved" }))).toMatchObject({
      submit: "alreadyApproved",
      release: null,
    });
    expect(iosProductionState(live, rel({ status: "live", pending_version: "2.0", pending_status: "in_review" }))).toMatchObject({
      submit: "inReview",
      release: "inReview",
    });
  });

  it("has nothing to do with only the version on sale, and nothing before the app is live or the channel read", () => {
    expect(iosProductionState(live, rel({ status: "live" }))).toMatchObject({ submit: "nothingQueued", release: "nothingQueued" });
    expect(iosProductionState({ state: "test_ready" }, rel({ status: "draft" }))).toMatchObject({ submit: "notLive", release: "notLive" });
    expect(iosProductionState(live, undefined)).toMatchObject({ submit: "notRead", release: "notRead" });
  });
});

describe("orphanStoreAppRows", () => {
  const apps = [
    { repository_id: "r-app", repository_name: "app", platform: "android" as const },
    { repository_id: "r-app", repository_name: "app", platform: "ios" as const },
    { repository_id: "r-mobile", repository_name: "mobile", platform: "ios" as const },
  ];

  it("lists a repository's apps only when no mobile component carries them", () => {
    const rows = orphanStoreAppRows(
      apps,
      [
        { role: "mobile", repositoryId: "r-mobile" },
        { role: "desktop", repositoryId: "r-app" },
      ],
      [{ id: "r-app", name: "app", projectIds: ["p1"] }],
    );
    expect(rows).toHaveLength(1);
    expect(rows[0]).toMatchObject({
      kind: "storeApps",
      name: "app",
      type: "mobile",
      href: "/operations/mobile/app/r-app",
      platforms: ["ios", "android"],
      projectIds: ["p1"],
    });
  });
});

describe("refusals", () => {
  it("keeps the server's sentence without its Go package prefixes", () => {
    expect(cleanServerMessage("storeops: promoting com.acme from internal to external: appstore: app 1 has no external TestFlight group to promote into")).toBe(
      "Promoting com.acme from internal to external: app 1 has no external TestFlight group to promote into",
    );
  });

  it("names the kind of no by status", () => {
    expect(describeRefusal(new ApiError("storeops: confirmation phrase does not match the repository name", 400)).kind).toBe("invalid");
    expect(describeRefusal(new ApiError("x", 409)).kind).toBe("notReady");
    expect(describeRefusal(new ApiError("x", 424)).kind).toBe("credentials");
    expect(describeRefusal(new Error("boom"))).toEqual({ kind: "failed", detail: "Boom" });
  });
});

describe("pickApp", () => {
  it("takes the platform asked for, else the first", () => {
    const apps = [{ platform: "ios" as const }, { platform: "android" as const }];
    expect(pickApp(apps, "android")).toBe(apps[1]);
    expect(pickApp(apps, "web")).toBe(apps[0]);
    expect(pickApp([], "ios")).toBeUndefined();
  });
});
