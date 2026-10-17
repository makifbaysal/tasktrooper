import { api, type ErrorDefinition } from "@/api";
import { useKeyedRead, type KeyedRead } from "@/hooks/useOpsSignals";

export const ERROR_DEFINITIONS_POLL_MS = 60_000;

/** Every status of the definitions in scope, in one read: the page's tabs
 * count and filter them locally. A component's scope also answers the
 * repository-wide ones. */
export function useErrorDefinitions(scope: { repositoryId?: string; componentId?: string }): KeyedRead<ErrorDefinition[]> {
  const key = `operations.errorDefinitions.${scope.repositoryId ?? "all"}.${scope.componentId ?? "all"}`;
  return useKeyedRead(
    key,
    async () => (await api.listErrorDefinitions({ repositoryId: scope.repositoryId, componentId: scope.componentId })).definitions ?? [],
    true,
    ERROR_DEFINITIONS_POLL_MS,
  );
}
