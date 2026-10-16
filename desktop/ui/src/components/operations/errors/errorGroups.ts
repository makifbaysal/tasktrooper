import type { EnvironmentSummary, OpsErrorGroup, RuntimeErrorGroup } from "@/api";
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

// "." survives URL encoding as is, and an environment id (a UUID) never holds one.
export function groupKey(g: Pick<OpsErrorGroup, "environment_id" | "fingerprint">): string {
  return `${g.environment_id}.${g.fingerprint}`;
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
