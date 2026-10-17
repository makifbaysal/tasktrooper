import type { DeployEnvironment, Incident, IncidentSeverity, IncidentStatus } from "@/api";
import { attributeIncidents } from "@/components/operations/console/opsEntities";
import { incidentEnvironment, isOpenIncident, OPEN_INCIDENT_STATUSES } from "@/components/operations/console/opsStatus";
import type { OpsEntity } from "@/components/operations/shell/useOpsEntities";

export const ISSUE_SEVERITIES: IncidentSeverity[] = ["critical", "high", "medium", "low"];
export const CLOSED_ISSUE_STATUSES: IncidentStatus[] = ["resolved", "ignored"];
export const ISSUE_STATUSES: IncidentStatus[] = [...OPEN_INCIDENT_STATUSES, ...CLOSED_ISSUE_STATUSES];

const SEVERITY_RANK: Record<IncidentSeverity, number> = { critical: 4, high: 3, medium: 2, low: 1 };

export function isIssueSeverity(value: string | null): value is IncidentSeverity {
  return ISSUE_SEVERITIES.some((s) => s === value);
}

export function isIssueStatus(value: string | null): value is IncidentStatus {
  return ISSUE_STATUSES.some((s) => s === value);
}

/** The listed components of the entity's repository, the entity included. */
export function siblingsOf(entity: OpsEntity, entities: OpsEntity[]): OpsEntity[] {
  const listed = entities.filter((e) => e.repositoryId === entity.repositoryId);
  return listed.some((e) => e.id === entity.id) ? listed : [...listed, entity];
}

export interface IssueScope {
  env: DeployEnvironment;
  entity?: OpsEntity;
  siblings?: OpsEntity[];
  repositoryId?: string;
}

/** The incidents an Issues view shows in one environment: an entity's by the
 * console's own attribution (its repository's incidents in that environment,
 * when it runs there or nothing does), else a repository's, else all. */
export function scopeIssues(incidents: Incident[], { env, entity, siblings, repositoryId }: IssueScope): Incident[] {
  if (entity) return attributeIncidents(entity, siblings ?? [entity], env, incidents);
  return incidents.filter((i) => (!repositoryId || i.repository_id === repositoryId) && incidentEnvironment(i.env) === env);
}

export function inOtherEnvironment(i: Pick<Incident, "env">): boolean {
  return incidentEnvironment(i.env) === undefined;
}

/** The scope's incidents whose label no environment answers to ("canary-eu"):
 * no environment picked in the top bar shows them, so an Issues view lists
 * them on their own whatever the pick. An entity gets its repository's. */
export function otherEnvironmentIssues(
  incidents: Incident[],
  { entity, repositoryId }: Pick<IssueScope, "entity" | "repositoryId">,
): Incident[] {
  const repository = entity?.repositoryId ?? repositoryId;
  return incidents.filter((i) => (!repository || i.repository_id === repository) && inOtherEnvironment(i));
}

export function issueEnd(i: Incident, now: number): number {
  if (i.resolved_at) return Date.parse(i.resolved_at);
  return isOpenIncident(i) ? now : Date.parse(i.last_seen_at);
}

export function issueDurationMs(i: Incident, now: number): number {
  return Math.max(0, issueEnd(i, now) - Date.parse(i.first_seen_at));
}

// A live issue is shown whatever the window; a closed one when it was
// happening at some point inside it.
export function issueInWindow(i: Incident, range: { from: number; to: number }, now: number): boolean {
  if (isOpenIncident(i)) return true;
  return Date.parse(i.first_seen_at) <= range.to && issueEnd(i, now) >= range.from;
}

export interface IssueFilter {
  severity: IncidentSeverity | "all";
  status: IncidentStatus | "all";
  query: string;
}

export const DEFAULT_ISSUE_FILTER: IssueFilter = { severity: "all", status: "all", query: "" };

export function issueMatches(i: Incident, filter: IssueFilter, repositoryName?: string): boolean {
  if (filter.severity !== "all" && i.severity !== filter.severity) return false;
  if (filter.status !== "all" && i.status !== filter.status) return false;
  const q = filter.query.trim().toLowerCase();
  if (!q) return true;
  return [i.title, i.source, i.fingerprint, i.detail, i.env, repositoryName].some(
    (v) => typeof v === "string" && v.toLowerCase().includes(q),
  );
}

/** Live before closed, the worst first, then the most recently seen. */
export function sortIssues(incidents: Incident[]): Incident[] {
  return [...incidents].sort(
    (a, b) =>
      Number(isOpenIncident(b)) - Number(isOpenIncident(a)) ||
      SEVERITY_RANK[b.severity] - SEVERITY_RANK[a.severity] ||
      Date.parse(b.last_seen_at) - Date.parse(a.last_seen_at),
  );
}

/** Which listed components an incident is about: the repository's ones
 * running in its environment, else all of the repository's. */
export function entitiesForIncident(i: Incident, entitiesByRepository: Map<string, OpsEntity[]>): OpsEntity[] {
  const listed = entitiesByRepository.get(i.repository_id) ?? [];
  const env = incidentEnvironment(i.env);
  const running = env ? listed.filter((e) => e.environments.some((x) => x.environment === env)) : [];
  return running.length > 0 ? running : listed;
}

export function groupEntitiesByRepository(entities: OpsEntity[]): Map<string, OpsEntity[]> {
  const byRepo = new Map<string, OpsEntity[]>();
  for (const e of entities) byRepo.set(e.repositoryId, [...(byRepo.get(e.repositoryId) ?? []), e]);
  return byRepo;
}
