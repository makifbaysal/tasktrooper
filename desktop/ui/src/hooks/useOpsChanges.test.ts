import { beforeEach, describe, expect, it, vi } from "vitest";
import { ApiError, type Release, type ReleaseStatus } from "@/api";
import { release } from "@/test/opsConsoleFixtures";

const { listAllReleases, getRelease } = vi.hoisted(() => ({ listAllReleases: vi.fn(), getRelease: vi.fn() }));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return { ...actual, api: { ...actual.api, listAllReleases, getRelease } };
});

import {
  applyChangeHead,
  CHANGE_MAX_PAGES,
  CHANGE_PAGE_SIZE,
  fetchChangeFeed,
  fetchChangeHead,
  type ChangeFeedSnapshot,
} from "@/hooks/useOpsChanges";

const HOUR = 3_600_000;
const NOW = Date.parse("2026-10-16T12:00:00Z");
const IN_FLIGHT = ["pending", "deploying", "verifying", "awaiting_verdict", "rolling_back"];
const CHANGE_STATUSES = ["pending", "deploying", "verifying", "awaiting_verdict", "rolling_back", "released", "rolled_back", "failed"];

type Query = { statuses?: ReleaseStatus[]; before?: string; componentId?: string; limit?: number };

const isInFlightRead = (opts: Query) => JSON.stringify(opts.statuses) === JSON.stringify(IN_FLIGHT);

function page(startHoursAgo: number, count = CHANGE_PAGE_SIZE) {
  return Array.from({ length: count }, (_, i) =>
    release(`r-${startHoursAgo}-${i}`, "r", { created_at: new Date(NOW - (startHoursAgo + i) * HOUR).toISOString() }),
  );
}

/** Paged reads answer from `pages` in order; the in-flight read answers `inFlight`. */
function serve(pages: Release[][], inFlight: Release[] = []) {
  let next = 0;
  listAllReleases.mockImplementation((opts: Query) =>
    Promise.resolve({ releases: isInFlightRead(opts) ? inFlight : (pages[next++] ?? []) }),
  );
}

const pagedCalls = () => listAllReleases.mock.calls.filter(([opts]) => !isInFlightRead(opts));

beforeEach(() => {
  listAllReleases.mockReset();
  getRelease.mockReset();
});

describe("fetchChangeFeed", () => {
  it("asks for pending and shipped releases of the scope, newest page first, and what is in flight beside it", async () => {
    serve([page(0, 3)]);
    const feed = await fetchChangeFeed({ componentId: "c-api" }, NOW - 24 * HOUR);
    expect(listAllReleases).toHaveBeenCalledWith({ componentId: "c-api", statuses: CHANGE_STATUSES, limit: CHANGE_PAGE_SIZE, before: undefined });
    expect(listAllReleases).toHaveBeenCalledWith({ componentId: "c-api", statuses: IN_FLIGHT, limit: CHANGE_PAGE_SIZE });
    expect(feed.releases).toHaveLength(3);
    expect(feed.coveredFrom).toBeNull();
    expect(feed.fetchedUntil).toBeNull();
  });

  it("pages back with `before` until the window's start is covered", async () => {
    serve([page(0), page(100)]);
    const feed = await fetchChangeFeed({ all: true }, NOW - 150 * HOUR);
    expect(pagedCalls()).toHaveLength(2);
    expect(pagedCalls()[1][0].before).toBe(new Date(NOW - 99 * HOUR).toISOString());
    expect(feed.releases).toHaveLength(2 * CHANGE_PAGE_SIZE);
    expect(feed.coveredFrom).toBe(NOW - 199 * HOUR);
  });

  it("pages a custom range back from its end, not from now", async () => {
    serve([page(48, 2)]);
    const until = NOW - 48 * HOUR;
    const feed = await fetchChangeFeed({ all: true }, NOW - 72 * HOUR, until);
    expect(pagedCalls()[0][0].before).toBe(new Date(until).toISOString());
    expect(feed.fetchedUntil).toBe(until);
  });

  it("keeps an old pending release the pages never reach", async () => {
    const stale = release("0.1.0", "r", { id: "old-pending", status: "pending", created_at: new Date(NOW - 9_000 * HOUR).toISOString() });
    serve(Array.from({ length: CHANGE_MAX_PAGES + 1 }, (_, p) => page(p * 100)), [stale]);
    const feed = await fetchChangeFeed({ all: true }, NOW - 10_000 * HOUR);
    expect(pagedCalls()).toHaveLength(CHANGE_MAX_PAGES);
    expect(feed.coveredFrom).toBeGreaterThan(NOW - 10_000 * HOUR);
    expect(feed.releases.some((r) => r.id === "old-pending")).toBe(true);
  });
});

describe("fetchChangeHead and applyChangeHead", () => {
  const snapshot = (releases: Release[]): ChangeFeedSnapshot => ({ releases, coveredFrom: null, fetchedFrom: 0, fetchedUntil: null });

  it("reads one page and the in-flight list, and reads back by id only what settled", async () => {
    const moving = release("2.0.0", "r", { id: "moving", status: "deploying" });
    const settledAway = release("1.9.0", "r", { id: "settled", status: "verifying" });
    const old = release("1.0.0", "r", { id: "old", status: "released" });
    serve([[]], [moving]);
    getRelease.mockResolvedValue({ ...settledAway, status: "released" });

    const head = await fetchChangeHead({ all: true }, undefined, [moving, settledAway, old]);
    expect(pagedCalls()).toHaveLength(1);
    expect(getRelease).toHaveBeenCalledTimes(1);
    expect(getRelease).toHaveBeenCalledWith("settled");

    const next = applyChangeHead(snapshot([moving, settledAway, old]), head);
    expect(next.releases.map((r) => [r.id, r.status])).toEqual([
      ["moving", "deploying"],
      ["settled", "released"],
      ["old", "released"],
    ]);
  });

  it("puts a new release on top and drops one the server no longer has", async () => {
    const fresh = release("3.0.0", "r", { id: "new", status: "pending" });
    const removed = release("2.0.0", "r", { id: "removed", status: "deploying" });
    serve([[fresh]]);
    getRelease.mockRejectedValue(new ApiError("release not found", 404));
    const head = await fetchChangeHead({ all: true }, undefined, [removed]);
    expect(head.gone).toEqual(["removed"]);
    expect(applyChangeHead(snapshot([removed]), head).releases.map((r) => r.id)).toEqual(["new"]);
  });
});
