import type { ComponentRole, DeployEnvironment, EnvironmentSummary, Incident, MobileStorePlatform, Release } from "@/api";
import { entityTypeOf, type OpsEntityType } from "@/components/operations/console/entityTypes";
import { entityPath } from "@/components/operations/console/roleViews";
import {
  compareOpsStatus,
  deriveOpsHealth,
  incidentEnvironment,
  isOpenIncident,
  OPS_STATUSES,
  type OpsHealth,
  type OpsStatus,
} from "@/components/operations/console/opsStatus";
import type { OpsEntity, OpsRepository } from "@/components/operations/shell/useOpsEntities";
import { ALL, NO_PROJECT, releaseEnvironment } from "@/lib/deployFeed";

export { ALL, NO_PROJECT, entityPath };

export function releaseTime(r: Release): string {
  return r.deployed_at ?? r.deploy_started_at ?? r.created_at;
}

export function releaseLabel(r: Release): string {
  return r.version || r.commit_sha?.slice(0, 7) || r.id.slice(0, 8);
}

export function openIncidentsIn(incidents: Incident[], environment: DeployEnvironment): Incident[] {
  return incidents.filter((i) => isOpenIncident(i) && incidentEnvironment(i.env) === environment);
}

/** Open incidents whose label no environment answers to ("canary-eu"): no
 * environment's rows count them, so the explorer says so instead. */
export function openIncidentsElsewhere(incidents: Incident[], repositoryIds?: ReadonlySet<string>): Incident[] {
  return incidents.filter(
    (i) => isOpenIncident(i) && incidentEnvironment(i.env) === undefined && (!repositoryIds || repositoryIds.has(i.repository_id)),
  );
}

// An incident names a repository and an environment, never a component: it
// goes to the components that run in that environment, and only when none
// does, to every listed component of the repository.
export function attributeIncidents(
  entity: OpsEntity,
  siblings: OpsEntity[],
  environment: DeployEnvironment,
  incidents: Incident[],
): Incident[] {
  const own = incidents.filter((i) => i.repository_id === entity.repositoryId && incidentEnvironment(i.env) === environment);
  if (own.length === 0) return own;
  const running = siblings.filter((s) => s.environments.some((e) => e.environment === environment));
  if (running.length === 0 || running.some((s) => s.id === entity.id)) return own;
  return [];
}

export function incidentsForEntity(
  entity: OpsEntity,
  siblings: OpsEntity[],
  environment: DeployEnvironment,
  incidents: Incident[],
): Incident[] {
  return attributeIncidents(entity, siblings, environment, incidents.filter(isOpenIncident));
}

// A release with no component belongs to a repository's only component.
export function releasesForEntity(
  entity: Pick<OpsEntity, "id" | "repositoryId">,
  soleComponent: boolean,
  environment: DeployEnvironment,
  releases: Release[],
): Release[] {
  return releases
    .filter(
      (r) =>
        r.repository_id === entity.repositoryId &&
        (r.component_id ? r.component_id === entity.id : soleComponent) &&
        releaseEnvironment(r) === environment,
    )
    .sort((a, b) => Date.parse(releaseTime(b)) - Date.parse(releaseTime(a)));
}

export type ExplorerRowType = OpsEntityType | "repository";

export interface ExplorerRow {
  key: string;
  /** `storeApps`: a repository's store apps that no mobile component carries. */
  kind: "entity" | "repository" | "storeApps";
  entityId?: string;
  /** Where the row opens when it is not an entity's own page. */
  href?: string;
  platforms?: MobileStorePlatform[];
  name: string;
  role?: ComponentRole;
  type: ExplorerRowType;
  repositoryId: string;
  repositoryName?: string;
  projectIds: string[];
  environment?: EnvironmentSummary;
  incidents: Incident[];
  lastRelease?: Release;
  health: OpsHealth;
}

export interface ExplorerInput {
  entities: OpsEntity[];
  repositories: OpsRepository[];
  incidents: Incident[];
  releases: Release[];
  environment: DeployEnvironment;
}

function compareRows(a: ExplorerRow, b: ExplorerRow): number {
  return (
    compareOpsStatus(a.health.status, b.health.status) ||
    a.name.localeCompare(b.name) ||
    (a.repositoryName ?? "").localeCompare(b.repositoryName ?? "")
  );
}

/** One row per listed component in the chosen environment, plus one per
 * repository whose open incidents no listed component can carry (nothing
 * scanned yet, or only libraries), so no incident drops out of the counts. */
export function buildExplorerRows({ entities, repositories, incidents, releases, environment }: ExplorerInput): ExplorerRow[] {
  const byRepo = new Map<string, OpsEntity[]>();
  for (const e of entities) byRepo.set(e.repositoryId, [...(byRepo.get(e.repositoryId) ?? []), e]);
  const repoById = new Map(repositories.map((r) => [r.id, r]));
  const rows: ExplorerRow[] = [];

  for (const entity of entities) {
    const siblings = byRepo.get(entity.repositoryId) ?? [entity];
    const env = entity.environments.find((e) => e.environment === environment);
    const open = incidentsForEntity(entity, siblings, environment, incidents);
    const soleComponent = (repoById.get(entity.repositoryId)?.componentCount ?? siblings.length) === 1;
    rows.push({
      key: `entity:${entity.id}`,
      kind: "entity",
      entityId: entity.id,
      name: entity.name,
      role: entity.role,
      type: entityTypeOf(entity.role),
      repositoryId: entity.repositoryId,
      repositoryName: entity.repositoryName,
      projectIds: entity.projectIds,
      environment: env,
      incidents: open,
      lastRelease: releasesForEntity(entity, soleComponent, environment, releases)[0],
      health: deriveOpsHealth({ connected: !!env, health: env?.health, incidents: open, errors24h: env?.error_count_24h }),
    });
  }

  const orphaned = new Map<string, Incident[]>();
  for (const incident of openIncidentsIn(incidents, environment)) {
    if (byRepo.has(incident.repository_id)) continue;
    orphaned.set(incident.repository_id, [...(orphaned.get(incident.repository_id) ?? []), incident]);
  }
  for (const [repositoryId, open] of orphaned) {
    const repo = repoById.get(repositoryId);
    rows.push({
      key: `repository:${repositoryId}`,
      kind: "repository",
      name: repo?.name ?? "",
      type: "repository",
      repositoryId,
      repositoryName: repo?.name,
      projectIds: repo?.projectIds ?? [],
      incidents: open,
      health: deriveOpsHealth({ connected: false, incidents: open }),
    });
  }

  return rows.sort(compareRows);
}

export interface ExplorerFilter {
  type: OpsEntityType | typeof ALL;
  health: OpsStatus | typeof ALL;
  projectId: string;
  query: string;
}

export const DEFAULT_EXPLORER_FILTER: ExplorerFilter = { type: ALL, health: ALL, projectId: ALL, query: "" };

export function rowMatches(row: ExplorerRow, filter: ExplorerFilter, ignore?: "type" | "health"): boolean {
  if (ignore !== "type" && filter.type !== ALL && row.type !== filter.type) return false;
  if (ignore !== "health" && filter.health !== ALL && row.health.status !== filter.health) return false;
  if (filter.projectId === NO_PROJECT && row.projectIds.length > 0) return false;
  if (filter.projectId !== ALL && filter.projectId !== NO_PROJECT && !row.projectIds.includes(filter.projectId)) return false;
  const q = filter.query.trim().toLowerCase();
  if (q && !row.name.toLowerCase().includes(q) && !(row.repositoryName ?? "").toLowerCase().includes(q)) return false;
  return true;
}

export function filterRows(rows: ExplorerRow[], filter: ExplorerFilter): ExplorerRow[] {
  return rows.filter((r) => rowMatches(r, filter));
}

export function healthCounts(rows: ExplorerRow[], filter: ExplorerFilter): Record<OpsStatus, number> {
  const counts = Object.fromEntries(OPS_STATUSES.map((s) => [s, 0])) as Record<OpsStatus, number>;
  for (const r of rows) if (rowMatches(r, filter, "health")) counts[r.health.status] += 1;
  return counts;
}

export function typeCounts(rows: ExplorerRow[], filter: ExplorerFilter): Map<ExplorerRowType, number> {
  const counts = new Map<ExplorerRowType, number>();
  for (const r of rows) if (rowMatches(r, filter, "type")) counts.set(r.type, (counts.get(r.type) ?? 0) + 1);
  return counts;
}

export function uniqueIncidents(rows: ExplorerRow[]): Incident[] {
  const seen = new Map<string, Incident>();
  for (const r of rows) for (const i of r.incidents) seen.set(i.id, i);
  return [...seen.values()];
}

export function issuesPath(target: { entityId?: string; repositoryId?: string } = {}): string {
  const params = new URLSearchParams();
  if (target.entityId) params.set("entity", target.entityId);
  else if (target.repositoryId) params.set("repository", target.repositoryId);
  const qs = params.toString();
  return `/operations/issues${qs ? `?${qs}` : ""}`;
}

export function repositoryDeployPath(repositoryId: string): string {
  return `/repositories/${encodeURIComponent(repositoryId)}?tab=deploy`;
}
