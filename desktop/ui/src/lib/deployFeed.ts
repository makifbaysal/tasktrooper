import type {
  CloudDeployment,
  CloudDeploymentStatus,
  ComponentSummary,
  DeployEnvironment,
  EnvironmentSummary,
  ProjectsOverview,
  Release,
  ReleaseStatus,
  RepositorySummary,
} from "@/api";
import { environmentFromLabel } from "@/lib/environmentLabel";

export interface RepoInfo {
  id: string;
  name: string;
  remoteUrl?: string;
  projectIds: string[];
  components: ComponentSummary[];
  environments: EnvironmentSummary[];
}

export interface EnvRef {
  env: EnvironmentSummary;
  repo: RepoInfo;
  componentName: string;
}

export interface DeployCatalog {
  projects: { id: string; name: string }[];
  repos: RepoInfo[];
  envs: EnvRef[];
}

// A repository in several projects shows up once per project in the
// overview; the catalog keeps one entry with every project it belongs to.
export function buildCatalog(overview: ProjectsOverview): DeployCatalog {
  const repos = new Map<string, RepoInfo>();
  const add = (r: RepositorySummary, projectId?: string) => {
    let info = repos.get(r.id);
    if (!info) {
      info = {
        id: r.id,
        name: r.name,
        remoteUrl: r.remote_url,
        projectIds: [],
        components: r.components ?? [],
        environments: r.environments ?? [],
      };
      repos.set(r.id, info);
    }
    if (projectId && !info.projectIds.includes(projectId)) info.projectIds.push(projectId);
  };
  for (const p of overview.projects ?? []) for (const r of p.repositories ?? []) add(r, p.id);
  for (const r of overview.unassigned ?? []) add(r);

  const sorted = [...repos.values()].sort((a, b) => a.name.localeCompare(b.name));
  const envs: EnvRef[] = sorted.flatMap((repo) =>
    repo.environments
      .filter((env) => env.status === "confirmed")
      .map((env) => ({ env, repo, componentName: componentName(repo, env.component_id) })),
  );
  return {
    projects: (overview.projects ?? []).map((p) => ({ id: p.id, name: p.name })),
    repos: sorted,
    envs,
  };
}

function componentName(repo: RepoInfo, componentId?: string): string {
  const c = repo.components.find((x) => x.id === componentId);
  return c?.name || repo.name;
}

export type FeedState = "pending" | "active" | "success" | "failed" | "rolled_back" | "canceled" | "unknown";

export function releaseState(status: ReleaseStatus): FeedState {
  switch (status) {
    case "draft":
    case "pending":
      return "pending";
    case "deploying":
    case "verifying":
    case "rolling_back":
    case "awaiting_verdict":
      return "active";
    case "released":
      return "success";
    case "failed":
      return "failed";
    case "rolled_back":
      return "rolled_back";
    default:
      return "canceled";
  }
}

export function deploymentState(status: CloudDeploymentStatus): FeedState {
  switch (status) {
    case "building":
      return "active";
    case "ready":
      return "success";
    case "error":
      return "failed";
    case "canceled":
      return "canceled";
    default:
      return "unknown";
  }
}

// The deploy watch records the environment in the release engineer's own
// shorthand ("prod", "stage", "preprod"); a release with no watch yet ships to
// production, and one whose label no environment answers to matches none.
export function releaseEnvironment(r: Release): DeployEnvironment | undefined {
  return environmentFromLabel(r.deploy?.env);
}

export interface DeployFeedItem {
  key: string;
  at: string;
  repo?: RepoInfo;
  repositoryId: string;
  componentName: string;
  environment?: DeployEnvironment;
  envId?: string;
  state: FeedState;
  commitSha?: string;
  commitMessage?: string;
  creator?: string;
  durationMs?: number;
  release?: Release;
  deployment?: CloudDeployment;
}

function sameCommit(a?: string, b?: string): boolean {
  if (!a || !b || a.length < 7 || b.length < 7) return false;
  return a.startsWith(b) || b.startsWith(a);
}

function span(from?: string, to?: string): number | undefined {
  if (!from || !to) return undefined;
  const ms = Date.parse(to) - Date.parse(from);
  return ms > 0 ? ms : undefined;
}

// One row per deploy. A TaskTrooper release and the provider deployment it
// produced are the same event, so the provider row is folded into the release
// (matched on the component's environment and commit) instead of listed twice.
export function buildFeed(
  catalog: DeployCatalog,
  releases: Release[],
  deploymentsByEnv: Record<string, CloudDeployment[]>,
): DeployFeedItem[] {
  const repoById = new Map(catalog.repos.map((r) => [r.id, r]));
  const used = new Set<string>();
  const items: DeployFeedItem[] = [];

  for (const r of releases) {
    if (r.status === "draft") continue;
    const repo = repoById.get(r.repository_id);
    const environment = releaseEnvironment(r);
    const envRef = catalog.envs.find(
      (e) => e.repo.id === r.repository_id && e.env.component_id === r.component_id && e.env.environment === environment,
    );
    const deployment = envRef
      ? (deploymentsByEnv[envRef.env.id] ?? []).find((d) => !used.has(d.id) && sameCommit(d.commit_sha, r.commit_sha))
      : undefined;
    if (deployment) used.add(deployment.id);
    const started = r.deploy_started_at ?? r.created_at;
    items.push({
      key: `release:${r.id}`,
      at: started,
      repo,
      repositoryId: r.repository_id,
      componentName: repo ? componentName(repo, r.component_id) : "",
      environment,
      envId: envRef?.env.id,
      state: releaseState(r.status),
      commitSha: r.commit_sha,
      commitMessage: deployment?.commit_message,
      creator: deployment?.creator,
      durationMs: span(started, r.finished_at ?? r.deployed_at),
      release: r,
      deployment,
    });
  }

  for (const ref of catalog.envs) {
    for (const d of deploymentsByEnv[ref.env.id] ?? []) {
      if (used.has(d.id)) continue;
      items.push({
        key: `deployment:${ref.env.id}:${d.id}`,
        at: d.created_at,
        repo: ref.repo,
        repositoryId: ref.repo.id,
        componentName: ref.componentName,
        environment: d.environment ?? ref.env.environment,
        envId: ref.env.id,
        state: deploymentState(d.status),
        commitSha: d.commit_sha,
        commitMessage: d.commit_message,
        creator: d.creator,
        durationMs: span(d.created_at, d.ready_at),
        deployment: d,
      });
    }
  }

  return items.sort((a, b) => Date.parse(b.at) - Date.parse(a.at));
}

export type StateFilter = "all" | "active" | "success" | "failed";

export interface FeedFilter {
  projectId: string;
  repositoryId: string;
  environment: DeployEnvironment | "all";
  state: StateFilter;
}

export const ALL = "all";
export const NO_PROJECT = "none";

export const DEFAULT_FILTER: FeedFilter = {
  projectId: ALL,
  repositoryId: ALL,
  environment: "production",
  state: ALL,
};

export function repoMatches(repo: RepoInfo | undefined, f: Pick<FeedFilter, "projectId" | "repositoryId">): boolean {
  if (!repo) return f.projectId === ALL && f.repositoryId === ALL;
  if (f.repositoryId !== ALL && repo.id !== f.repositoryId) return false;
  if (f.projectId === NO_PROJECT) return repo.projectIds.length === 0;
  if (f.projectId !== ALL) return repo.projectIds.includes(f.projectId);
  return true;
}

export function stateMatches(state: FeedState, f: StateFilter): boolean {
  switch (f) {
    case "active":
      return state === "active" || state === "pending";
    case "success":
      return state === "success";
    case "failed":
      return state === "failed" || state === "rolled_back";
    default:
      return true;
  }
}

export function itemMatches(item: DeployFeedItem, f: FeedFilter): boolean {
  if (!repoMatches(item.repo, f)) return false;
  if (f.environment !== ALL && item.environment !== f.environment) return false;
  return stateMatches(item.state, f.state);
}

// A release being verified or awaiting its verdict is already deployed: the
// environment serves it while the release engineer watches it.
function serving(i: DeployFeedItem): boolean {
  if (i.state === "success") return true;
  const status = i.release?.status;
  return !!i.release?.deployed_at && (status === "verifying" || status === "awaiting_verdict");
}

// What an environment serves right now: its newest deployed change. A newer
// deploy still in flight is reported beside it, not instead of it.
export function liveOf(envId: string, feed: DeployFeedItem[]): { live?: DeployFeedItem; inFlight?: DeployFeedItem } {
  const own = feed.filter((i) => i.envId === envId);
  const live = own.find(serving);
  const inFlight = own.find(
    (i) =>
      i !== live &&
      (i.state === "active" || i.state === "pending") &&
      (!live || Date.parse(i.at) >= Date.parse(live.at)),
  );
  return { live, inFlight };
}

export function historyMatches(state: FeedState, f: StateFilter): boolean {
  if (state === "active" || state === "pending") return false;
  return stateMatches(state, f);
}

// A refresh re-reads only the newest page; pages the viewer already loaded
// past it stay, so polling never collapses "load more".
export function mergeReleasePages(fresh: Release[], previous: Release[]): Release[] {
  const oldest = fresh.at(-1)?.created_at;
  if (!oldest) return fresh;
  const seen = new Set(fresh.map((r) => r.id));
  const older = previous.filter((r) => !seen.has(r.id) && Date.parse(r.created_at) < Date.parse(oldest));
  return [...fresh, ...older];
}
