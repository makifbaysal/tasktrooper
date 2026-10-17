import { useCallback } from "react";
import { api, type Incident, type Release, type ReleaseStatus } from "@/api";
import { createOpsResource, useOpsResource } from "@/components/operations/shell/useOpsResource";
import { INCIDENT_LIST_LIMIT, LIVE_INCIDENT_STATUSES } from "@/hooks/useOpsIssues";

export const OPS_CONSOLE_POLL_MS = 30_000;
export const OPS_RELEASE_LIMIT = 100;

// Releases that reached an environment. Asking the server for only these
// keeps drafts and pending cuts from using up the page and hiding real deploys.
export const SHIPPED_RELEASE_STATUSES: ReleaseStatus[] = [
  "deploying",
  "verifying",
  "awaiting_verdict",
  "rolling_back",
  "released",
  "rolled_back",
  "failed",
];

const CACHE_INCIDENTS = "operations.console.openIncidents";
const CACHE_RELEASES = "operations.console.shippedReleases";

const NO_INCIDENTS: Incident[] = [];
const NO_RELEASES: Release[] = [];

// The server's largest page (its default is 100), so a busy workspace's
// health and counts are not read from only the newest hundred.
const OPEN_INCIDENTS = createOpsResource(() =>
  api.listIncidents({ status: LIVE_INCIDENT_STATUSES, limit: INCIDENT_LIST_LIMIT }).then((r) => r.incidents ?? NO_INCIDENTS),
);

const SHIPPED_RELEASES = createOpsResource(() =>
  api.listAllReleases({ statuses: SHIPPED_RELEASE_STATUSES, limit: OPS_RELEASE_LIMIT }).then((r) => r.releases ?? NO_RELEASES),
);

/** Every open incident and the newest shipped releases: what the console's
 * health, open-issue counts and "last deploy" are read from until data
 * sources feed it. Shared by every Operations page through one poll each. */
export function useOpsConsole() {
  const incidents = useOpsResource(OPEN_INCIDENTS, CACHE_INCIDENTS, NO_INCIDENTS, OPS_CONSOLE_POLL_MS);
  const releases = useOpsResource(SHIPPED_RELEASES, CACHE_RELEASES, NO_RELEASES, OPS_CONSOLE_POLL_MS);
  const reloadIncidents = incidents.reload;
  const reloadReleases = releases.reload;
  const reload = useCallback(() => {
    reloadIncidents();
    reloadReleases();
  }, [reloadIncidents, reloadReleases]);
  return {
    incidents: incidents.value,
    releases: releases.value,
    loading: incidents.loading || releases.loading,
    error: incidents.error ?? releases.error,
    reload,
  };
}
