import { api, type DeployEnvironment, type ObservabilityReadiness } from "@/api";
import { useKeyedRead, type KeyedRead } from "@/hooks/useOpsSignals";

// Readiness moves only when a binding or the code changes.
export const OBSERVABILITY_READINESS_POLL_MS = 5 * 60_000;

/** What one component reports in one environment and what it lacks. */
export function useObservabilityReadiness(componentId: string, env: DeployEnvironment, enabled = true): KeyedRead<ObservabilityReadiness> {
  return useKeyedRead(
    `operations.observability.${componentId}.${env}`,
    () => api.getComponentObservability(componentId, env),
    enabled,
    OBSERVABILITY_READINESS_POLL_MS,
  );
}
