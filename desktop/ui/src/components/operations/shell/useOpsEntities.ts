import { useCallback, useMemo } from "react";
import { api, type ComponentRole, type DeployEnvironment, type EnvironmentSummary, type ProjectsOverview } from "@/api";
import { OPS_ENVIRONMENTS } from "@/components/operations/shell/OpsContext";
import { createOpsResource, useOpsResource } from "@/components/operations/shell/useOpsResource";
import { buildCatalog } from "@/lib/deployFeed";

export interface OpsEntity {
  id: string;
  name: string;
  role: ComponentRole;
  repositoryId: string;
  repositoryName: string;
  projectIds: string[];
  environments: EnvironmentSummary[];
}

export interface OpsRepository {
  id: string;
  name: string;
  remoteUrl?: string;
  projectIds: string[];
  componentCount: number;
}

export interface OpsProjectRef {
  id: string;
  name: string;
}

export const OPS_RUNTIME_ROLES: ComponentRole[] = ["backend", "worker", "frontend", "mobile", "desktop"];

export const OPS_ENTITIES_POLL_MS = 30_000;

export const CACHE_OPS_OVERVIEW = "operations.entities.overview";

const OVERVIEW = createOpsResource(() => api.getProjectsOverview());

interface OpsDirectory {
  listed: OpsEntity[];
  all: OpsEntity[];
  repositories: OpsRepository[];
  projects: OpsProjectRef[];
}

const EMPTY_DIRECTORY: OpsDirectory = { listed: [], all: [], repositories: [], projects: [] };

function byEnvironment(a: EnvironmentSummary, b: EnvironmentSummary): number {
  return OPS_ENVIRONMENTS.indexOf(a.environment) - OPS_ENVIRONMENTS.indexOf(b.environment);
}

// Listed: a component that runs somewhere confirmed, or whose role always
// runs (a service, site, worker or app not connected yet is "not reporting",
// not absent). A library or CLI with no environment is still findable by id.
export function buildOpsDirectory(overview: ProjectsOverview | null): OpsDirectory {
  if (!overview) return EMPTY_DIRECTORY;
  const catalog = buildCatalog(overview);
  const all: OpsEntity[] = [];
  const repositories: OpsRepository[] = [];
  for (const repo of catalog.repos) {
    repositories.push({
      id: repo.id,
      name: repo.name,
      remoteUrl: repo.remoteUrl,
      projectIds: repo.projectIds,
      componentCount: repo.components.length,
    });
    for (const c of repo.components) {
      const environments = repo.environments
        .filter((e) => e.component_id === c.id && e.status === "confirmed")
        .sort(byEnvironment);
      all.push({
        id: c.id,
        name: c.name || repo.name,
        role: c.role,
        repositoryId: repo.id,
        repositoryName: repo.name,
        projectIds: repo.projectIds,
        environments,
      });
    }
  }
  all.sort((a, b) => a.name.localeCompare(b.name) || a.repositoryName.localeCompare(b.repositoryName));
  const listed = all.filter((e) => e.environments.length > 0 || OPS_RUNTIME_ROLES.includes(e.role));
  return { listed, all, repositories, projects: catalog.projects };
}

export function toOpsEntities(overview: ProjectsOverview | null): OpsEntity[] {
  return buildOpsDirectory(overview).listed;
}

function useOpsDirectoryState(enabled = true) {
  const { value, loading, error, reload } = useOpsResource<ProjectsOverview | null>(
    OVERVIEW,
    CACHE_OPS_OVERVIEW,
    null,
    OPS_ENTITIES_POLL_MS,
    enabled,
  );
  const directory = useMemo(() => buildOpsDirectory(value), [value]);
  return { directory, loaded: value !== null, loading, error, reload };
}

export function useOpsEntities(): { entities: OpsEntity[]; loading: boolean; error?: string; reload(): void } {
  const { directory, loading, error, reload } = useOpsDirectoryState();
  return { entities: directory.listed, loading, error, reload };
}

export function useOpsEntity(id: string | undefined): {
  entity?: OpsEntity;
  envFor(env: DeployEnvironment): EnvironmentSummary | undefined;
  loading: boolean;
  /** The overview has been read at least once (or painted from the cache). */
  loaded: boolean;
  error?: string;
  reload(): void;
} {
  const { directory, loaded, loading, error, reload } = useOpsDirectoryState(id !== undefined);
  const entity = useMemo(() => (id ? directory.all.find((e) => e.id === id) : undefined), [directory, id]);
  const envFor = useCallback(
    (env: DeployEnvironment) => entity?.environments.find((e) => e.environment === env),
    [entity],
  );
  return { entity, envFor, loading, loaded, error, reload };
}

/** Every repository and project the overview knows, including repositories
 * with no listed component, so an incident filed against one is never lost. */
export function useOpsDirectory(): {
  repositories: OpsRepository[];
  projects: OpsProjectRef[];
  loaded: boolean;
  loading: boolean;
  error?: string;
} {
  const { directory, loaded, loading, error } = useOpsDirectoryState();
  return { repositories: directory.repositories, projects: directory.projects, loaded, loading, error };
}
