import { useCallback, useEffect, useRef, useState } from "react";
import { api, type Incident } from "@/api";
import { OPEN_INCIDENT_STATUSES } from "@/components/operations/console/opsStatus";
import { createOpsResource, useOpsResource, type OpsResource } from "@/components/operations/shell/useOpsResource";
import { usePolling } from "@/hooks/usePolling";
import { keepEqual } from "@/lib/stableState";

export const ISSUES_POLL_MS = 30_000;
/** The most the server returns in one read: it takes anything above as its default, 100. */
export const INCIDENT_LIST_LIMIT = 500;
export const LIVE_INCIDENT_STATUSES = OPEN_INCIDENT_STATUSES.join(",");

const NO_INCIDENTS: Incident[] = [];
const resources = new Map<string, OpsResource<Incident[]>>();

/** A full page: the server holds more, the least recently seen are left out. */
export function incidentsTruncated(incidents: Incident[]): boolean {
  return incidents.length >= INCIDENT_LIST_LIMIT;
}

/** One read of the incident list: the live statuses unless closed ones are
 * asked for, and as many as the server returns at once. */
export function readIncidents(repositoryId: string | undefined, includeClosed: boolean): Promise<Incident[]> {
  return api
    .listIncidents({ repositoryId, status: includeClosed ? undefined : LIVE_INCIDENT_STATUSES, limit: INCIDENT_LIST_LIMIT })
    .then((r) => r.incidents ?? NO_INCIDENTS);
}

export function issueFeedKey(repositoryId: string | undefined, includeClosed: boolean): string {
  return `${repositoryId ?? "*"}|${includeClosed ? "all" : "live"}`;
}

function issuesResource(repositoryId: string | undefined, includeClosed: boolean): OpsResource<Incident[]> {
  const key = issueFeedKey(repositoryId, includeClosed);
  let resource = resources.get(key);
  if (!resource) {
    resource = createOpsResource(() => readIncidents(repositoryId, includeClosed));
    resources.set(key, resource);
  }
  return resource;
}

/** Incidents for the Issues page: the live ones by default, every status
 * with `includeClosed`, across repositories or for one. Mount under a key
 * of `issueFeedKey`: the cache key follows it. */
export function useIssueFeed(repositoryId: string | undefined, includeClosed: boolean) {
  const { value, loading, error, reload } = useOpsResource(
    issuesResource(repositoryId, includeClosed),
    `operations.issues.${issueFeedKey(repositoryId, includeClosed)}`,
    NO_INCIDENTS,
    ISSUES_POLL_MS,
  );
  return { incidents: value, truncated: incidentsTruncated(value), loading, error, reload };
}

/** One incident with its timeline, read on its own so a notification's link
 * opens whatever the list's filters; `listed` paints first. */
export function useIncidentDetail(id: string | null, listed: Incident | undefined) {
  const [fetched, setFetched] = useState<Incident | undefined>(undefined);
  const [error, setError] = useState<string | undefined>(undefined);
  const [loading, setLoading] = useState(id !== null);
  const current = useRef(id);
  current.current = id;

  const load = useCallback(async () => {
    if (!id) return;
    try {
      const next = await api.getIncident(id);
      if (current.current !== id) return;
      setFetched((prev) => keepEqual(prev, next));
      setError(undefined);
    } catch (err) {
      if (current.current !== id) return;
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      if (current.current === id) setLoading(false);
    }
  }, [id]);

  useEffect(() => {
    setFetched(undefined);
    setError(undefined);
    setLoading(id !== null);
    void load();
  }, [id, load]);

  usePolling(load, ISSUES_POLL_MS, id !== null, { leading: false });

  const own = fetched?.id === id ? fetched : undefined;
  const incident = own ?? (listed?.id === id ? listed : undefined);
  // An action answers without the timeline; keep the one already read.
  const apply = useCallback(
    (updated: Incident) => setFetched((prev) => ({ ...updated, events: updated.events ?? prev?.events })),
    [],
  );

  return { incident, loading: loading && !incident, error, reload: load, apply };
}
