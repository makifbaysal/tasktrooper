import { describe, expect, it } from "vitest";
import {
  entitiesForIncident,
  groupEntitiesByRepository,
  issueInWindow,
  issueMatches,
  otherEnvironmentIssues,
  scopeIssues,
  siblingsOf,
  sortIssues,
  DEFAULT_ISSUE_FILTER,
} from "@/components/operations/issues/issueList";
import { toOpsEntities } from "@/components/operations/shell/useOpsEntities";
import { incident, opsOverview } from "@/test/opsConsoleFixtures";

const entities = toOpsEntities(opsOverview);
const byId = (id: string) => entities.find((e) => e.id === id)!;

describe("scopeIssues", () => {
  const incidents = [
    incident("prod", "r-shop", "high"),
    incident("stage", "r-shop", "medium", { env: "stage" }),
    incident("other", "r-site", "low"),
  ];

  it("keeps an environment's incidents across repositories, or one repository's", () => {
    expect(scopeIssues(incidents, { env: "production" }).map((i) => i.id)).toEqual(["prod", "other"]);
    expect(scopeIssues(incidents, { env: "production", repositoryId: "r-shop" }).map((i) => i.id)).toEqual(["prod"]);
    expect(scopeIssues(incidents, { env: "staging" }).map((i) => i.id)).toEqual(["stage"]);
  });

  it("gives an entity its repository's incidents when it runs in that environment", () => {
    const api = byId("c-api");
    const siblings = siblingsOf(api, entities);
    expect(siblings.map((e) => e.id).sort()).toEqual(["c-api", "c-web"]);
    expect(scopeIssues(incidents, { env: "production", entity: api, siblings }).map((i) => i.id)).toEqual(["prod"]);
    expect(scopeIssues(incidents, { env: "staging", entity: api, siblings }).map((i) => i.id)).toEqual(["stage"]);
  });

  it("leaves an entity that does not run there out when a sibling does", () => {
    const web = byId("c-web");
    expect(scopeIssues(incidents, { env: "staging", entity: web, siblings: siblingsOf(web, entities) })).toEqual([]);
  });
});

describe("otherEnvironmentIssues", () => {
  const incidents = [
    incident("prod", "r-shop", "high"),
    incident("canary", "r-shop", "medium", { env: "canary-eu" }),
    incident("edge", "r-site", "low", { env: "edge" }),
  ];

  it("keeps the incidents no environment answers to, so no pick hides them", () => {
    expect(otherEnvironmentIssues(incidents, {}).map((i) => i.id)).toEqual(["canary", "edge"]);
    expect(otherEnvironmentIssues(incidents, { repositoryId: "r-site" }).map((i) => i.id)).toEqual(["edge"]);
    expect(otherEnvironmentIssues(incidents, { entity: byId("c-web") }).map((i) => i.id)).toEqual(["canary"]);
    expect(scopeIssues(incidents, { env: "production" }).map((i) => i.id)).toEqual(["prod"]);
  });
});

describe("entitiesForIncident", () => {
  it("names the repository's components running in the incident's environment", () => {
    const byRepo = groupEntitiesByRepository(entities);
    expect(entitiesForIncident(incident("x", "r-shop", "low", { env: "stage" }), byRepo).map((e) => e.id)).toEqual(["c-api"]);
    expect(entitiesForIncident(incident("y", "r-shop", "low"), byRepo).map((e) => e.id).sort()).toEqual(["c-api", "c-web"]);
    expect(entitiesForIncident(incident("z", "r-docs", "low"), byRepo)).toEqual([]);
  });
});

describe("window, filters and order", () => {
  const range = { from: Date.parse("2026-10-15T09:00:00Z"), to: Date.parse("2026-10-15T12:00:00Z") };
  const now = Date.parse("2026-10-15T12:00:00Z");

  it("always keeps a live issue, and a closed one when it overlapped the window", () => {
    expect(issueInWindow(incident("live", "r", "low", { first_seen_at: "2026-10-01T00:00:00Z" }), range, now)).toBe(true);
    const closedBefore = incident("a", "r", "low", {
      status: "resolved",
      first_seen_at: "2026-10-14T00:00:00Z",
      resolved_at: "2026-10-14T01:00:00Z",
    });
    const closedDuring = incident("b", "r", "low", {
      status: "resolved",
      first_seen_at: "2026-10-15T08:00:00Z",
      resolved_at: "2026-10-15T09:30:00Z",
    });
    expect(issueInWindow(closedBefore, range, now)).toBe(false);
    expect(issueInWindow(closedDuring, range, now)).toBe(true);
  });

  it("matches severity, status and text", () => {
    const i = incident("i", "r", "high", { title: "Checkout is down", source: "alertmanager", fingerprint: "fp-1" });
    expect(issueMatches(i, DEFAULT_ISSUE_FILTER)).toBe(true);
    expect(issueMatches(i, { ...DEFAULT_ISSUE_FILTER, severity: "low" })).toBe(false);
    expect(issueMatches(i, { ...DEFAULT_ISSUE_FILTER, status: "resolved" })).toBe(false);
    expect(issueMatches(i, { ...DEFAULT_ISSUE_FILTER, query: "checkout" })).toBe(true);
    expect(issueMatches(i, { ...DEFAULT_ISSUE_FILTER, query: "shop" }, "shop")).toBe(true);
    expect(issueMatches(i, { ...DEFAULT_ISSUE_FILTER, query: "nothing" })).toBe(false);
  });

  it("puts live issues first, the worst first, then the latest seen", () => {
    const sorted = sortIssues([
      incident("low", "r", "low"),
      incident("closed", "r", "critical", { status: "resolved" }),
      incident("high-old", "r", "high", { last_seen_at: "2026-10-15T08:00:00Z" }),
      incident("high-new", "r", "high", { last_seen_at: "2026-10-15T11:00:00Z" }),
      incident("crit", "r", "critical"),
    ]);
    expect(sorted.map((i) => i.id)).toEqual(["crit", "high-new", "high-old", "low", "closed"]);
  });
});
