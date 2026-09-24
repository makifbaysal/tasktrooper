import { useCallback, useEffect, useRef, useState } from "react";
import { api, type WorkspaceMap } from "@/api";
import { useCachedState, useFirstLoad } from "@/hooks/useCachedState";

export const CACHE_WORKSPACE_MAP = "projectModel.workspaceMap";

/** Every project with its repositories, cross-project edges and shared
 * resources (`GET /v1/projects/map`) — the projects hub's Map view. */
export function useWorkspaceMap() {
  const [map, setMap] = useCachedState<WorkspaceMap | null>(CACHE_WORKSPACE_MAP, null);
  const [loading, setLoading] = useFirstLoad(CACHE_WORKSPACE_MAP);
  const [error, setError] = useState<string | null>(null);
  const versionRef = useRef(0);

  const reload = useCallback(async () => {
    const version = ++versionRef.current;
    try {
      const next = await api.getWorkspaceMap();
      if (versionRef.current !== version) return;
      setMap(next);
      setError(null);
    } catch (e) {
      if (versionRef.current !== version) return;
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      if (versionRef.current === version) setLoading(false);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  useEffect(() => {
    void reload();
  }, [reload]);

  return { map, loading, error, reload };
}
