import { describe, expect, it } from "vitest";
import {
  ALL,
  DEFAULT_EXPLORER_FILTER,
  NO_PROJECT,
  buildExplorerRows,
  entityPath,
  filterRows,
  healthCounts,
  issuesPath,
  openIncidentsElsewhere,
  openIncidentsIn,
  releaseLabel,
  rowMatches,
  typeCounts,
  uniqueIncidents,
  type ExplorerFilter,
} from "@/components/operations/console/opsEntities";
import { buildOpsDirectory } from "@/components/operations/shell/useOpsEntities";
import { incident, opsIncidents, opsOverview, opsReleases } from "@/test/opsConsoleFixtures";

const directory = buildOpsDirectory(opsOverview);
const rowsIn = (environment: "production" | "staging") =>
  buildExplorerRows({
    entities: directory.listed,
    repositories: directory.repositories,
    incidents: opsIncidents,
    releases: opsReleases,
    environment,
  });

const prod = rowsIn("production");
const byName = (rows: typeof prod, name: string) => rows.find((r) => r.name === name)!;

describe("buildExplorerRows", () => {
  it("lists every watched component plus a row for an incident no component can carry, worst first", () => {
    expect(prod.map((r) => r.name)).toEqual(["api", "docs", "site", "web", "jobs"]);
    expect(prod.map((r) => r.health.status)).toEqual(["critical", "critical", "critical", "critical", "warning"]);
    const docs = byName(prod, "docs");
    expect(docs.kind).toBe("repository");
    expect(docs.type).toBe("repository");
    expect(docs.incidents.map((i) => i.id)).toEqual(["i-docs"]);
  });

  it("gives a repository's incident to the components running in that environment", () => {
    expect(byName(prod, "api").incidents.map((i) => i.id)).toEqual(["i-shop"]);
    expect(byName(prod, "web").incidents.map((i) => i.id)).toEqual(["i-shop"]);
    const jobs = byName(prod, "jobs");
    expect(jobs.incidents.map((i) => i.id)).toEqual(["i-tools"]);
    expect(jobs.environment).toBeUndefined();
    expect(jobs.health.reason).toBe("incidentWarning");
  });

  it("treats a failing resource as critical, the same as everywhere else", () => {
    expect(byName(prod, "site").health).toEqual({ status: "critical", reason: "resourceFailed" });
  });

  it("never lets a preprod incident or release reach production", () => {
    expect(prod.flatMap((r) => r.incidents).some((i) => i.id === "i-pre")).toBe(false);
    expect(byName(prod, "api").lastRelease?.version).toBe("1.4.0");
  });

  it("picks the newest shipped release, one with no component going to a repository's only one", () => {
    expect(byName(prod, "web").lastRelease).toBeUndefined();
    expect(byName(prod, "site").lastRelease?.id).toBe("site-9");
    expect(releaseLabel({ ...opsReleases[4], version: "" })).toBe("abcdef1");
  });

  it("reads another environment's own health, incidents and releases", () => {
    const staging = rowsIn("staging");
    const api = byName(staging, "api");
    expect(api.incidents.map((i) => i.id)).toEqual(["i-pre"]);
    expect(api.health.status).toBe("critical");
    expect(api.lastRelease?.version).toBe("1.5.0-rc");
    const web = byName(staging, "web");
    expect(web.environment).toBeUndefined();
    expect(web.health).toEqual({ status: "notReporting", reason: "notConnected" });
    expect(staging.some((r) => r.kind === "repository")).toBe(false);
  });
});

describe("filters and facets", () => {
  const f = (patch: Partial<ExplorerFilter>): ExplorerFilter => ({ ...DEFAULT_EXPLORER_FILTER, ...patch });
  const names = (filter: ExplorerFilter) => filterRows(prod, filter).map((r) => r.name);

  it("filters by type, health, project and search", () => {
    expect(names(f({ type: "web" }))).toEqual(["site", "web"]);
    expect(names(f({ health: "warning" }))).toEqual(["jobs"]);
    expect(names(f({ projectId: "p-shop" }))).toEqual(["api", "web"]);
    expect(names(f({ projectId: NO_PROJECT }))).toEqual(["docs", "site"]);
    expect(names(f({ query: "SHO" }))).toEqual(["api", "web"]);
    expect(names(f({ type: "workers", health: "critical" }))).toEqual([]);
  });

  it("counts each health facet over the other filters, not over itself", () => {
    expect(healthCounts(prod, f({}))).toEqual({ critical: 4, warning: 1, healthy: 0, notReporting: 0 });
    expect(healthCounts(prod, f({ health: "warning" }))).toEqual({ critical: 4, warning: 1, healthy: 0, notReporting: 0 });
    expect(healthCounts(prod, f({ projectId: "p-tools" }))).toEqual({ critical: 0, warning: 1, healthy: 0, notReporting: 0 });
  });

  it("counts types the same way", () => {
    const counts = typeCounts(prod, f({ type: "web" }));
    expect(Object.fromEntries(counts)).toEqual({ services: 1, repository: 1, web: 2, workers: 1 });
    expect(rowMatches(byName(prod, "docs"), f({ type: "services" }))).toBe(false);
  });

  it("totals open issues from every incident in the environment, those without a component included", () => {
    const visible = prod.filter((r) => rowMatches(r, f({}), "health"));
    expect(uniqueIncidents(visible).map((i) => i.id).sort()).toEqual(openIncidentsIn(opsIncidents, "production").map((i) => i.id).sort());
    expect(uniqueIncidents(visible)).toHaveLength(3);
    expect(uniqueIncidents(filterRows(prod, f({ projectId: "p-shop" })))).toHaveLength(1);
    expect(uniqueIncidents(filterRows(prod, f({ projectId: ALL })))).toHaveLength(3);
  });

  it("counts the open incidents no environment answers to apart, never in an environment's rows", () => {
    const incidents = [...opsIncidents, incident("i-canary", "r-shop", "high", { env: "canary-eu" }), incident("i-edge", "r-site", "low", { env: "edge", status: "resolved" })];
    const rows = buildExplorerRows({
      entities: directory.listed,
      repositories: directory.repositories,
      incidents,
      releases: opsReleases,
      environment: "production",
    });
    expect(uniqueIncidents(rows).map((i) => i.id)).not.toContain("i-canary");
    expect(openIncidentsElsewhere(incidents).map((i) => i.id)).toEqual(["i-canary"]);
    expect(openIncidentsElsewhere(incidents, new Set(["r-site"]))).toEqual([]);
  });
});

describe("paths", () => {
  it("builds the entity and issues addresses", () => {
    expect(entityPath("c-api")).toBe("/operations/entity/c-api/summary");
    expect(entityPath("c-api", "changes")).toBe("/operations/entity/c-api/changes");
    expect(issuesPath({ entityId: "c-api" })).toBe("/operations/issues?entity=c-api");
    expect(issuesPath({ repositoryId: "r-docs" })).toBe("/operations/issues?repository=r-docs");
    expect(issuesPath()).toBe("/operations/issues");
  });
});
