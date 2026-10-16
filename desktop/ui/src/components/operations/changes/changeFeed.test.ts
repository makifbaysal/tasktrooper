import { describe, expect, it } from "vitest";
import {
  CHANGES_IDLE_POLL_MS,
  CHANGES_LIVE_POLL_MS,
  ROLLBACK_WINDOW_MS,
  changeMatches,
  changeOwner,
  changeStatusGroup,
  changesPollMs,
  daysAgo,
  groupByDay,
  impactChips,
  inChangeWindow,
  releaseActions,
  statusGroupCounts,
} from "@/components/operations/changes/changeFeed";
import type { OpsEntity, OpsRepository } from "@/components/operations/shell/useOpsEntities";
import { release } from "@/test/opsConsoleFixtures";

// Noon UTC stays on the same calendar day in every time zone from UTC-11 to UTC+11.
const noon = (day: string) => `${day}T12:00:00Z`;

describe("groupByDay", () => {
  it("groups by local day, newest day and newest change first", () => {
    const releases = [
      release("a", "r", { deployed_at: noon("2026-10-14") }),
      release("b", "r", { deployed_at: noon("2026-10-16") }),
      release("c", "r", { deployed_at: "2026-10-16T12:30:00Z" }),
      release("d", "r", { deployed_at: noon("2026-10-15") }),
    ];
    const days = groupByDay(releases);
    expect(days.map((d) => d.releases.map((r) => r.id))).toEqual([["c", "b"], ["d"], ["a"]]);
    expect(new Date(days[0].start).getDate()).toBe(new Date(noon("2026-10-16")).getDate());
  });

  it("reads a release's time as deployed, else deploy started, else opened", () => {
    const days = groupByDay([
      release("opened", "r", { deployed_at: undefined, created_at: noon("2026-10-10") }),
      release("started", "r", { deployed_at: undefined, deploy_started_at: noon("2026-10-12"), created_at: noon("2026-10-01") }),
    ]);
    expect(days.map((d) => d.releases[0].id)).toEqual(["started", "opened"]);
  });

  it("counts days back in local time", () => {
    const [today] = groupByDay([release("x", "r", { deployed_at: noon("2026-10-16") })]);
    expect(daysAgo(today.start, Date.parse("2026-10-16T12:30:00Z"))).toBe(0);
    expect(daysAgo(today.start, Date.parse(noon("2026-10-17")))).toBe(1);
  });
});

describe("status groups", () => {
  it("folds the release statuses into the feed's facets", () => {
    expect(changeStatusGroup("verifying")).toBe("inProgress");
    expect(changeStatusGroup("awaiting_verdict")).toBe("inProgress");
    expect(changeStatusGroup("pending")).toBe("pending");
    expect(changeStatusGroup("rolled_back")).toBe("rolledBack");
    const counts = statusGroupCounts([
      release("1", "r", { status: "released" }),
      release("2", "r", { status: "deploying" }),
      release("3", "r", { status: "failed" }),
      release("4", "r", { status: "released" }),
    ]);
    expect(counts).toEqual({ inProgress: 1, pending: 0, released: 2, rolledBack: 0, failed: 1 });
  });
});

describe("impactChips", () => {
  it("counts health samples and smoke results, failing ones in the destructive tone", () => {
    const chips = impactChips(
      {
        health: [
          { at: noon("2026-10-16"), ok: true, latency_ms: 40 },
          { at: noon("2026-10-16"), ok: false, error: "timeout" },
        ],
        smoke: [{ check: { path: "/" }, url: "https://x/", at: noon("2026-10-16"), ok: true }],
      },
      "released",
    );
    expect(chips).toEqual([
      { kind: "health", ok: 1, failed: 1, tone: "destructive" },
      { kind: "smoke", passed: 1, failed: 0, tone: "success" },
    ]);
  });

  it("adds new error groups, an early stop and what could not be checked", () => {
    const chips = impactChips(
      {
        new_errors: [{ fingerprint: "f", message: "boom", count: 3, first_seen: "", last_seen: "", new: true }],
        early_stop: "3 new error groups",
        notes: ["logs not readable"],
      },
      "failed",
    );
    expect(chips.map((c) => c.kind)).toEqual(["newErrors", "earlyStop", "gaps"]);
    expect(chips[0]).toMatchObject({ count: 1, tone: "warning" });
  });

  it("says a moving release is not checked yet and a finished one recorded nothing", () => {
    expect(impactChips({}, "verifying")).toEqual([{ kind: "checking", tone: "muted" }]);
    expect(impactChips(undefined, "released")).toEqual([{ kind: "notChecked", tone: "muted" }]);
  });
});

describe("live polling", () => {
  it("polls every 15 s while a listed release is moving, every minute otherwise", () => {
    expect(changesPollMs([release("a", "r", { status: "released" }), release("b", "r", { status: "verifying" })])).toBe(CHANGES_LIVE_POLL_MS);
    expect(changesPollMs([release("a", "r", { status: "rolling_back" })])).toBe(CHANGES_LIVE_POLL_MS);
    expect(changesPollMs([release("a", "r", { status: "released" }), release("b", "r", { status: "pending" })])).toBe(CHANGES_IDLE_POLL_MS);
    expect(changesPollMs([])).toBe(CHANGES_IDLE_POLL_MS);
    expect(CHANGES_LIVE_POLL_MS).toBe(15_000);
  });

  it("keeps an in-flight release on the feed whatever the window", () => {
    const range = { from: Date.parse(noon("2026-10-16")), to: Date.parse(noon("2026-10-16")) + 3_600_000 };
    expect(inChangeWindow(release("old", "r", { deployed_at: noon("2026-10-10"), status: "released" }), range)).toBe(false);
    expect(inChangeWindow(release("soak", "r", { deployed_at: noon("2026-10-10"), status: "verifying" }), range)).toBe(true);
    expect(inChangeWindow(release("in", "r", { deployed_at: "2026-10-16T12:30:00Z" }), range)).toBe(true);
  });
});

describe("releaseActions", () => {
  const now = Date.parse(noon("2026-10-16"));

  it("offers Deploy only to a pending dispatch or batch release", () => {
    expect(releaseActions(release("a", "r", { status: "pending", mode: "dispatch" }), now).deploy).toBe(true);
    expect(releaseActions(release("a", "r", { status: "pending", mode: "batch" }), now).deploy).toBe(true);
    expect(releaseActions(release("a", "r", { status: "pending", mode: "on_merge" }), now).deploy).toBe(false);
  });

  it("offers Finish while awaiting a verdict and on a failed release", () => {
    expect(releaseActions(release("a", "r", { status: "awaiting_verdict" }), now).finish).toBe(true);
    expect(releaseActions(release("a", "r", { status: "failed" }), now).finish).toBe(true);
    expect(releaseActions(release("a", "r", { status: "released" }), now).finish).toBe(false);
  });

  it("offers Roll back within 24 h of the verdict, and says so after", () => {
    const recent = release("a", "r", { status: "released", finished_at: new Date(now - 3_600_000).toISOString() });
    const old = release("b", "r", { status: "released", finished_at: new Date(now - ROLLBACK_WINDOW_MS - 1).toISOString() });
    expect(releaseActions(recent, now)).toMatchObject({ rollback: true, rollbackExpired: false });
    expect(releaseActions(old, now)).toMatchObject({ rollback: false, rollbackExpired: true });
    expect(releaseActions(release("c", "r", { status: "rolled_back" }), now).rollback).toBe(false);
  });
});

describe("changeOwner and search", () => {
  const api = { id: "c-api", name: "api", repositoryId: "r-shop", repositoryName: "shop" } as OpsEntity;
  const site = { id: "c-site", name: "site", repositoryId: "r-site", repositoryName: "site" } as OpsEntity;
  const repos = new Map<string, OpsRepository>([
    ["r-shop", { id: "r-shop", name: "shop", projectIds: [], componentCount: 3 }],
    ["r-site", { id: "r-site", name: "site", projectIds: [], componentCount: 1 }],
  ]);
  const byId = new Map([
    ["c-api", api],
    ["c-site", site],
  ]);
  const byRepo = new Map([
    ["r-shop", [api]],
    ["r-site", [site]],
  ]);

  it("names the component, or a repository's only component when the release names none", () => {
    expect(changeOwner(release("a", "r-shop", { component_id: "c-api" }), byId, repos, byRepo).entity).toBe(api);
    expect(changeOwner(release("b", "r-site"), byId, repos, byRepo).entity).toBe(site);
    const orphan = changeOwner(release("c", "r-shop"), byId, repos, byRepo);
    expect(orphan.entity).toBeUndefined();
    expect(orphan.name).toBe("shop");
  });

  it("searches version, commit, tasks and the owner's names", () => {
    const r = release("1.4.0", "r-shop", { commit_sha: "abcdef123", tasks: [{ id: "t1", key: "SHOP-12", title: "Faster checkout" }] });
    const owner = { name: "api", repositoryName: "shop" };
    expect(changeMatches(r, "1.4", owner)).toBe(true);
    expect(changeMatches(r, "ABCDEF", owner)).toBe(true);
    expect(changeMatches(r, "checkout", owner)).toBe(true);
    expect(changeMatches(r, "shop-12", owner)).toBe(true);
    expect(changeMatches(r, "web", owner)).toBe(false);
  });
});
