import { describe, expect, it } from "vitest";
import type { CloudDeployment, EnvironmentSummary, ProjectsOverview, Release, RepositorySummary } from "@/api";
import {
  DEFAULT_FILTER,
  NO_PROJECT,
  buildCatalog,
  buildFeed,
  historyMatches,
  itemMatches,
  liveOf,
  mergeReleasePages,
  releaseEnvironment,
} from "@/lib/deployFeed";

function env(overrides: Partial<EnvironmentSummary> = {}): EnvironmentSummary {
  return {
    id: "env-site-prod",
    component_id: "comp-site",
    environment: "production",
    provider: "vercel",
    url: "https://www.tasktrooper.ai",
    status: "confirmed",
    health: "healthy",
    error_count_24h: 0,
    ...overrides,
  };
}

function repo(id: string, name: string, environments: EnvironmentSummary[]): RepositorySummary {
  return {
    id,
    name,
    description: "",
    shape: "single",
    components: [{ id: `comp-${name}`, path: ".", name, role: "frontend", stack_summary: "", checks: 0, required_checks: 0 }],
    review_count: 0,
    environments,
    updated_at: "2026-09-27T00:00:00Z",
  } as RepositorySummary;
}

const site = repo("repo-site", "site", [env()]);
const pishio = repo("repo-pishio", "pishio-web", [
  env({ id: "env-pishio-prod", component_id: "comp-pishio-web" }),
  env({ id: "env-pishio-suggested", component_id: "comp-pishio-web", environment: "staging", status: "suggested" }),
]);
const loose = repo("repo-loose", "loose", []);

const overview = {
  projects: [
    { id: "p-tt", name: "Task Trooper", repositories: [site] },
    { id: "p-pishio", name: "Pishio", repositories: [pishio] },
    { id: "p-both", name: "Shared", repositories: [site] },
  ],
  unassigned: [loose],
} as unknown as ProjectsOverview;

function release(overrides: Partial<Release> = {}): Release {
  return {
    id: "rel-1",
    repository_id: "repo-site",
    component_id: "comp-site",
    version: "9bf8db5dd66e",
    mode: "on_merge",
    executor: "vercel",
    status: "released",
    commit_sha: "9bf8db5dd66e9e424f626b1fa88fdd6c146a85ed",
    profile: {} as Release["profile"],
    checks: {} as Release["checks"],
    tasks: [],
    created_at: "2026-09-27T01:41:13Z",
    updated_at: "2026-09-27T01:51:00Z",
    deploy_started_at: "2026-09-27T01:41:13Z",
    finished_at: "2026-09-27T01:51:38Z",
    ...overrides,
  };
}

function deployment(overrides: Partial<CloudDeployment> = {}): CloudDeployment {
  return {
    id: "dpl-1",
    status: "ready",
    environment: "production",
    commit_sha: "9bf8db5dd66e9e424f626b1fa88fdd6c146a85ed",
    commit_message: "feat: landing hero",
    created_at: "2026-09-27T01:41:20Z",
    ready_at: "2026-09-27T01:42:00Z",
    creator: "makif",
    ...overrides,
  };
}

describe("buildCatalog", () => {
  const catalog = buildCatalog(overview);

  it("lists a repository once with every project it belongs to", () => {
    expect(catalog.repos.map((r) => r.name)).toEqual(["loose", "pishio-web", "site"]);
    expect(catalog.repos.find((r) => r.id === "repo-site")?.projectIds).toEqual(["p-tt", "p-both"]);
  });

  it("keeps only confirmed environments", () => {
    expect(catalog.envs.map((e) => e.env.id)).toEqual(["env-pishio-prod", "env-site-prod"]);
  });
});

describe("buildFeed", () => {
  const catalog = buildCatalog(overview);

  it("folds the provider deployment a release produced into the release row", () => {
    const feed = buildFeed(catalog, [release()], {
      "env-site-prod": [deployment(), deployment({ id: "dpl-0", commit_sha: "aaaaaaa1111", created_at: "2026-09-26T10:00:00Z" })],
    });
    expect(feed.map((i) => i.key)).toEqual(["release:rel-1", "deployment:env-site-prod:dpl-0"]);
    expect(feed[0].deployment?.id).toBe("dpl-1");
    expect(feed[0].commitMessage).toBe("feat: landing hero");
    expect(feed[0].envId).toBe("env-site-prod");
  });

  it("lists git-push deploys nobody released through TaskTrooper, newest first", () => {
    const feed = buildFeed(catalog, [], {
      "env-pishio-prod": [
        deployment({ id: "a", commit_sha: "1111111aaaa", created_at: "2026-09-25T10:00:00Z" }),
        deployment({ id: "b", commit_sha: "2222222bbbb", created_at: "2026-09-26T10:00:00Z", status: "building" }),
      ],
    });
    expect(feed.map((i) => [i.deployment?.id, i.state, i.repo?.name])).toEqual([
      ["b", "active", "pishio-web"],
      ["a", "success", "pishio-web"],
    ]);
  });

  it("skips drafts: a batch that was never cut deployed nothing", () => {
    expect(buildFeed(catalog, [release({ status: "draft" })], {})).toEqual([]);
  });
});

describe("filters", () => {
  const catalog = buildCatalog(overview);
  const feed = buildFeed(catalog, [release()], {
    "env-pishio-prod": [deployment({ id: "p1", commit_sha: "3333333cccc" })],
  });

  it("scopes by project, including a repository shared by two projects", () => {
    const names = (projectId: string) =>
      feed.filter((i) => itemMatches(i, { ...DEFAULT_FILTER, projectId })).map((i) => i.repo?.name);
    expect(names("p-tt")).toEqual(["site"]);
    expect(names("p-both")).toEqual(["site"]);
    expect(names("p-pishio")).toEqual(["pishio-web"]);
    expect(names(NO_PROJECT)).toEqual([]);
  });

  it("keeps in-flight deploys out of the history", () => {
    expect(historyMatches("active", "all")).toBe(false);
    expect(historyMatches("pending", "all")).toBe(false);
    expect(historyMatches("rolled_back", "failed")).toBe(true);
    expect(historyMatches("success", "failed")).toBe(false);
  });
});

describe("liveOf", () => {
  it("is the newest successful deploy, with a newer in-flight one reported beside it", () => {
    const catalog = buildCatalog(overview);
    const feed = buildFeed(catalog, [], {
      "env-site-prod": [
        deployment({ id: "new", commit_sha: "4444444dddd", status: "building", created_at: "2026-09-27T09:00:00Z" }),
        deployment({ id: "ok", commit_sha: "5555555eeee", created_at: "2026-09-27T08:00:00Z" }),
        deployment({ id: "old", commit_sha: "6666666ffff", created_at: "2026-09-26T08:00:00Z" }),
      ],
    });
    const { live, inFlight } = liveOf("env-site-prod", feed);
    expect(live?.deployment?.id).toBe("ok");
    expect(inFlight?.deployment?.id).toBe("new");
  });

  it("counts a deployed release under verification as what production serves", () => {
    const catalog = buildCatalog(overview);
    const verifying = release({
      id: "rel-v",
      status: "verifying",
      commit_sha: "7777777aaaa",
      created_at: "2026-09-27T10:00:00Z",
      deploy_started_at: "2026-09-27T10:00:00Z",
      deployed_at: "2026-09-27T10:01:00Z",
    });
    const feed = buildFeed(catalog, [verifying, release()], {});
    const { live, inFlight } = liveOf("env-site-prod", feed);
    expect(live?.release?.id).toBe("rel-v");
    expect(inFlight).toBeUndefined();
  });
});

describe("releaseEnvironment", () => {
  it("reads the deploy watch's shorthand and defaults to production", () => {
    expect(releaseEnvironment(release())).toBe("production");
    expect(releaseEnvironment(release({ deploy: { env: "stage" } as Release["deploy"] }))).toBe("staging");
    expect(releaseEnvironment(release({ deploy: { env: "prod" } as Release["deploy"] }))).toBe("production");
  });

  it("never reads a pre-production or unknown label as production", () => {
    const at = (env: string) => releaseEnvironment(release({ deploy: { env } as Release["deploy"] }));
    expect(at("preprod")).toBe("staging");
    expect(at("uat")).toBe("staging");
    expect(at("qa")).toBe("staging");
    expect(at("live")).toBe("production");
    expect(at("canary-eu")).toBeUndefined();
  });
});

describe("mergeReleasePages", () => {
  it("keeps older pages the viewer loaded when the newest page refreshes", () => {
    const r = (id: string, at: string) => release({ id, created_at: at });
    const previous = [r("c", "2026-09-27T03:00:00Z"), r("b", "2026-09-27T02:00:00Z"), r("a", "2026-09-27T01:00:00Z")];
    const fresh = [r("d", "2026-09-27T04:00:00Z"), r("c", "2026-09-27T03:00:00Z")];
    expect(mergeReleasePages(fresh, previous).map((x) => x.id)).toEqual(["d", "c", "b", "a"]);
  });
});
