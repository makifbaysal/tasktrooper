import { api, type ComponentTables, type DeployEnvironment, type WebTableName } from "@/api";
import { useKeyedRead, type KeyedRead } from "@/hooks/useOpsSignals";

// Like the queues answer, a table is read at the instant asked and kept a
// short while server side, so a faster poll would read the same rows.
export const OPS_TABLES_POLL_MS = 60_000;

/** A backend's endpoints table(s) as they are now: throughput, error rate
 * and p95 per endpoint, from every confirmed binding whose recipe has one. */
export function useComponentEndpoints(componentId: string, env: DeployEnvironment, enabled = true): KeyedRead<ComponentTables> {
  return useKeyedRead(
    `operations.endpoints.${componentId}.${env}`,
    () => api.getComponentEndpoints(componentId, env),
    enabled,
    OPS_TABLES_POLL_MS,
  );
}

/** One of a frontend's web tables: pages, referrers, countries or devices. */
export function useComponentWeb(
  componentId: string,
  env: DeployEnvironment,
  table: WebTableName,
  enabled = true,
): KeyedRead<ComponentTables> {
  return useKeyedRead(
    `operations.web.${componentId}.${env}.${table}`,
    () => api.getComponentWeb(componentId, env, table),
    enabled,
    OPS_TABLES_POLL_MS,
  );
}

/** A mobile app's crash and ANR issues, and its builds' diagnostic signatures. */
export function useComponentCrashes(componentId: string, env: DeployEnvironment, enabled = true): KeyedRead<ComponentTables> {
  return useKeyedRead(
    `operations.crashes.${componentId}.${env}`,
    () => api.getComponentCrashes(componentId, env),
    enabled,
    OPS_TABLES_POLL_MS,
  );
}
