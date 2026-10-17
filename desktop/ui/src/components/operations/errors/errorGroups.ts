import {
  CLOUD_PROVIDERS,
  ZERO_UUID,
  type BoundErrorGroup,
  type CloudProviderKind,
  type DeployEnvironment,
  type EnvironmentSummary,
  type OpsErrorGroup,
  type RuntimeErrorGroup,
} from "@/api";
import { entityTypeOf, type OpsEntityType } from "@/components/operations/console/entityTypes";
import type { OpsEntity } from "@/components/operations/shell/useOpsEntities";

/** Mirrors the inbox's server side (opsquery + `cloud.ErrorsSupported`): an
 * environment bound to a cloud resource is read — GCP Error Reporting, or the
 * error-level logs grouped by message for Vercel and AWS — except a Vercel
 * preview, one deployment per branch; a custom URL has no account to read. */
export function hasErrorFeed(env: Pick<EnvironmentSummary, "environment" | "provider" | "resource_name">): boolean {
  if (!env.provider || !env.resource_name) return false;
  return !(env.provider === "vercel" && env.environment === "preview");
}

/** A row read through a component's data source binding (a Sentry project)
 * rather than a cloud environment: it carries the source and the zero
 * environment id. */
export function isBindingRow(row: { environment_id: string; data_source_id?: string | null }): boolean {
  return Boolean(row.data_source_id) || row.environment_id === ZERO_UUID;
}

// "." survives URL encoding as is, and an environment or data source id (a
// UUID) never holds one. Every binding row shares the zero environment id, so
// those are told apart by their source.
export function groupKey(g: Pick<OpsErrorGroup, "environment_id" | "fingerprint" | "data_source_id">): string {
  return `${isBindingRow(g) && g.data_source_id ? g.data_source_id : g.environment_id}.${g.fingerprint}`;
}

/** A cloud environment's group opens its task through the environment; a
 * binding group (Sentry) through its component, naming the source. A row
 * with neither has nowhere to open one. */
export function canCreateTask(g: Pick<OpsErrorGroup, "environment_id" | "data_source_id" | "component_id">): boolean {
  return isBindingRow(g) ? Boolean(g.data_source_id && g.component_id) : true;
}

export function isCloudProvider(provider: string): provider is CloudProviderKind {
  return (CLOUD_PROVIDERS as string[]).includes(provider);
}

/** A group's provider by name: a cloud's, a data source kind's (Sentry), else as sent. */
export function providerLabel(t: (key: string) => string, provider: string): string {
  for (const key of [`cloud.providers.${provider}`, `operations.telemetry.errors.providers.${provider}`]) {
    const text = t(key);
    if (text !== key) return text;
  }
  return provider;
}

export function dataSourcePath(id: string): string {
  return `/operations/sources?source=${encodeURIComponent(id)}`;
}

/** The per-environment shape `POST /v1/environments/{id}/errors/task` takes. */
export function runtimeGroup(g: OpsErrorGroup): RuntimeErrorGroup {
  return {
    fingerprint: g.fingerprint,
    message: g.message,
    count: g.count,
    first_seen: g.first_seen,
    last_seen: g.last_seen,
    sample: g.sample,
    source: g.source,
    new: g.new,
    external_url: g.external_url,
  };
}

/** The shape `POST /v1/components/{id}/errors/task` takes for a binding group. */
export function boundGroup(g: OpsErrorGroup, environment: DeployEnvironment): BoundErrorGroup {
  return { ...runtimeGroup(g), data_source_id: g.data_source_id ?? "", environment };
}

export function groupType(g: OpsErrorGroup, entities: Map<string, OpsEntity>): OpsEntityType {
  const entity = entities.get(g.component_id);
  return entity ? entityTypeOf(entity.role) : "other";
}

export interface ErrorFilter {
  type?: OpsEntityType;
  newOnly: boolean;
  query: string;
}

export function groupMatches(
  g: OpsErrorGroup,
  filter: ErrorFilter,
  entities: Map<string, OpsEntity>,
  skip?: "type",
): boolean {
  if (filter.newOnly && !g.new) return false;
  if (skip !== "type" && filter.type && groupType(g, entities) !== filter.type) return false;
  const q = filter.query.trim().toLowerCase();
  if (!q) return true;
  const name = entities.get(g.component_id)?.name ?? "";
  return [g.message, g.source, g.fingerprint, g.sample, name].some((v) => (v ?? "").toLowerCase().includes(q));
}

export function typeCountsOf(groups: OpsErrorGroup[], filter: ErrorFilter, entities: Map<string, OpsEntity>): Map<OpsEntityType, number> {
  const counts = new Map<OpsEntityType, number>();
  for (const g of groups) {
    if (!groupMatches(g, filter, entities, "type")) continue;
    const type = groupType(g, entities);
    counts.set(type, (counts.get(type) ?? 0) + 1);
  }
  return counts;
}
