import { useMemo } from "react";
import { api, type DeployEnvironment, type EnvironmentSummary, type Incident, type Release } from "@/api";
import {
  attributeIncidents,
  incidentsForEntity,
  releasesForEntity,
} from "@/components/operations/console/opsEntities";
import { deriveOpsHealth, type OpsHealth } from "@/components/operations/console/opsStatus";
import { otherEnvironmentIssues } from "@/components/operations/issues/issueList";
import { createOpsResource, useOpsResource, type OpsResource } from "@/components/operations/shell/useOpsResource";
import { useOpsDirectory, useOpsEntities, type OpsEntity } from "@/components/operations/shell/useOpsEntities";
import { OPS_CONSOLE_POLL_MS, OPS_RELEASE_LIMIT, SHIPPED_RELEASE_STATUSES, useOpsConsole } from "@/hooks/useOpsConsole";
import { incidentsTruncated, readIncidents } from "@/hooks/useOpsIssues";

const NO_RELEASES: Release[] = [];
const NO_INCIDENTS: Incident[] = [];

const releaseResources = new Map<string, OpsResource<Release[]>>();
const incidentResources = new Map<string, OpsResource<Incident[]>>();

// A repository with one component may have releases opened before
// component_id existed; those are read by repository and kept for it.
function releasesResource(scope: { componentId: string } | { repositoryId: string }): OpsResource<Release[]> {
  const key = "componentId" in scope ? `c:${scope.componentId}` : `r:${scope.repositoryId}`;
  let resource = releaseResources.get(key);
  if (!resource) {
    resource = createOpsResource(() =>
      api
        .listAllReleases({ ...scope, statuses: SHIPPED_RELEASE_STATUSES, limit: OPS_RELEASE_LIMIT })
        .then((r) => r.releases ?? NO_RELEASES),
    );
    releaseResources.set(key, resource);
  }
  return resource;
}

function incidentsKey(repositoryId: string, includeClosed: boolean): string {
  return includeClosed ? repositoryId : `${repositoryId}|live`;
}

function incidentsResource(repositoryId: string, includeClosed: boolean): OpsResource<Incident[]> {
  const key = incidentsKey(repositoryId, includeClosed);
  let resource = incidentResources.get(key);
  if (!resource) {
    resource = createOpsResource(() => readIncidents(repositoryId, includeClosed));
    incidentResources.set(key, resource);
  }
  return resource;
}

function useSiblings(entity: OpsEntity): OpsEntity[] {
  const { entities } = useOpsEntities();
  return useMemo(() => {
    const listed = entities.filter((e) => e.repositoryId === entity.repositoryId);
    return listed.some((e) => e.id === entity.id) ? listed : [...listed, entity];
  }, [entities, entity]);
}

/** The entity's shipped releases in one environment, newest first. Mount
 * under a key of the entity's id: the cache key follows the entity. */
export function useEntityReleases(entity: OpsEntity, env: DeployEnvironment) {
  const { repositories } = useOpsDirectory();
  const sole = (repositories.find((r) => r.id === entity.repositoryId)?.componentCount ?? 0) === 1;
  const resource = releasesResource(sole ? { repositoryId: entity.repositoryId } : { componentId: entity.id });
  const { value, loading, error } = useOpsResource(
    resource,
    `operations.entity.releases.${sole ? `r:${entity.repositoryId}` : `c:${entity.id}`}`,
    NO_RELEASES,
    OPS_CONSOLE_POLL_MS,
  );
  const releases = useMemo(() => releasesForEntity(entity, sole, env, value), [entity, sole, env, value]);
  return { releases, loading, error };
}

/** The entity's incidents in one environment — every status, or the live
 * ones only with `includeClosed: false` — and, as `others`, its repository's
 * incidents whose label no environment answers to. Mount under a key of the
 * entity's id: the cache key follows the repository. */
export function useEntityIncidents(entity: OpsEntity, env: DeployEnvironment, { includeClosed = true }: { includeClosed?: boolean } = {}) {
  const siblings = useSiblings(entity);
  const key = incidentsKey(entity.repositoryId, includeClosed);
  const { value, loading, error, reload } = useOpsResource(
    incidentsResource(entity.repositoryId, includeClosed),
    `operations.entity.incidents.${key}`,
    NO_INCIDENTS,
    OPS_CONSOLE_POLL_MS,
  );
  const incidents = useMemo(() => attributeIncidents(entity, siblings, env, value), [entity, siblings, env, value]);
  const others = useMemo(() => otherEnvironmentIssues(value, { entity }), [entity, value]);
  return { incidents, others, truncated: incidentsTruncated(value), loading, error, reload };
}

/** The entity's health in one environment, from the same open incidents
 * and stored health the explorer reads, so both always agree. */
export function useEntityHealth(
  entity: OpsEntity,
  env: DeployEnvironment,
  environment: EnvironmentSummary | undefined,
): { health: OpsHealth; openIncidents: Incident[] } {
  const siblings = useSiblings(entity);
  const { incidents } = useOpsConsole();
  return useMemo(() => {
    const open = incidentsForEntity(entity, siblings, env, incidents);
    return {
      openIncidents: open,
      health: deriveOpsHealth({
        connected: !!environment,
        health: environment?.health,
        incidents: open,
        errors24h: environment?.error_count_24h,
      }),
    };
  }, [entity, siblings, env, incidents, environment]);
}
