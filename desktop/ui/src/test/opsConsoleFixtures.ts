import type {
  ComponentRole,
  ComponentSummary,
  EnvironmentSummary,
  Incident,
  ProjectsOverview,
  Release,
  RepositorySummary,
} from "@/api";

export function component(id: string, name: string, role: ComponentRole, path = "."): ComponentSummary {
  return { id, path, name, role, stack_summary: "", checks: 0, required_checks: 0 };
}

export function env(id: string, componentId: string, overrides: Partial<EnvironmentSummary> = {}): EnvironmentSummary {
  return {
    id,
    component_id: componentId,
    environment: "production",
    status: "confirmed",
    health: "healthy",
    error_count_24h: 0,
    ...overrides,
  };
}

export function repo(
  id: string,
  name: string,
  components: ComponentSummary[],
  environments: EnvironmentSummary[],
  projectIds: string[] = [],
): RepositorySummary {
  return {
    id,
    name,
    description: "",
    project_ids: projectIds,
    shape: "single",
    components,
    review_count: 0,
    environments,
    updated_at: "2026-10-01T00:00:00Z",
  } as RepositorySummary;
}

export function incident(id: string, repositoryId: string, severity: Incident["severity"], overrides: Partial<Incident> = {}): Incident {
  return {
    id,
    repository_id: repositoryId,
    env: "prod",
    source: "alertmanager",
    fingerprint: id,
    title: `Incident ${id}`,
    severity,
    status: "open",
    confidence: 0,
    occurrences: 1,
    first_seen_at: "2026-10-15T10:00:00Z",
    last_seen_at: "2026-10-15T10:00:00Z",
    ...overrides,
  };
}

export function release(id: string, repositoryId: string, overrides: Partial<Release> = {}): Release {
  return {
    id,
    repository_id: repositoryId,
    version: id,
    mode: "on_merge",
    status: "released",
    profile: {},
    checks: {},
    tasks: [],
    created_at: "2026-10-15T09:00:00Z",
    updated_at: "2026-10-15T09:10:00Z",
    deployed_at: "2026-10-15T09:05:00Z",
    ...overrides,
  } as Release;
}

export const shopRepo = repo(
  "r-shop",
  "shop",
  [component("c-api", "api", "backend"), component("c-web", "web", "frontend", "web"), component("c-lib", "ui-kit", "library", "packages/ui")],
  [
    env("e-api", "c-api", { provider: "gcp", resource_name: "api-svc", error_count_24h: 3 }),
    env("e-api-stg", "c-api", { environment: "staging", provider: "gcp", resource_name: "api-svc-stg", health: "degraded" }),
    env("e-web", "c-web", { provider: "vercel", resource_name: "shop-web" }),
    env("e-web-stg", "c-web", { environment: "staging", status: "suggested" }),
  ],
  ["p-shop"],
);

export const toolsRepo = repo(
  "r-tools",
  "tools",
  [component("c-cli", "tt-cli", "cli", "cli"), component("c-worker", "jobs", "worker", "worker")],
  [],
  ["p-tools"],
);

export const siteRepo = repo("r-site", "site", [component("c-site", "site", "frontend")], [
  env("e-site", "c-site", { provider: "vercel", resource_name: "site", health: "failed", error_count_24h: 12 }),
]);

// A repository whose only component is a library: nothing is listed for it,
// so its incident needs a repository row of its own.
export const docsRepo = repo("r-docs", "docs", [component("c-docs", "docs-kit", "library")], []);

export const opsOverview: ProjectsOverview = {
  projects: [
    { id: "p-shop", name: "Shop", repositories: [shopRepo] },
    { id: "p-tools", name: "Tools", repositories: [toolsRepo] },
  ],
  unassigned: [siteRepo, docsRepo],
} as unknown as ProjectsOverview;

export const opsIncidents: Incident[] = [
  incident("i-shop", "r-shop", "critical"),
  incident("i-tools", "r-tools", "low"),
  incident("i-site-old", "r-site", "critical", { status: "resolved" }),
  incident("i-docs", "r-docs", "high"),
  incident("i-pre", "r-shop", "critical", { env: "preprod" }),
];

export const opsReleases: Release[] = [
  release("1.5.0-rc", "r-shop", { component_id: "c-api", deploy: { env: "stage" } as Release["deploy"], deployed_at: "2026-10-15T11:00:00Z" }),
  release("pre-1", "r-shop", { component_id: "c-api", deploy: { env: "preprod" } as Release["deploy"], deployed_at: "2026-10-15T10:30:00Z" }),
  release("1.4.0", "r-shop", { component_id: "c-api", deployed_at: "2026-10-15T10:00:00Z" }),
  release("1.3.0", "r-shop", { component_id: "c-api", deployed_at: "2026-10-14T10:00:00Z" }),
  release("site-9", "r-site", { commit_sha: "abcdef1234567" }),
];
