import { useCallback, useEffect, useRef, useState } from "react";
import { api, type ProjectMap } from "@/api";
import { hasCache, readCache, writeCache } from "@/lib/uiCache";

function cacheKey(projectId: string): string {
  return `projectModel.projectMap.${projectId}`;
}

/** One project's architecture map (`GET /v1/projects/:projectId/map`); same
 * per-id cache-then-refresh contract as useProjectOverview. */
export function useProjectMap(projectId: string | undefined) {
  const [map, setMap] = useState<ProjectMap | null>(() =>
    projectId ? (readCache<ProjectMap>(cacheKey(projectId)) ?? null) : null,
  );
  const [loading, setLoading] = useState(() => (projectId ? !hasCache(cacheKey(projectId)) : false));
  const [error, setError] = useState<string | null>(null);
  const versionRef = useRef(0);

  const reload = useCallback(async () => {
    if (!projectId) return;
    const version = ++versionRef.current;
    try {
      const next = await api.getProjectMap(projectId);
      if (versionRef.current !== version) return;
      writeCache(cacheKey(projectId), next);
      setMap(next);
      setError(null);
    } catch (e) {
      if (versionRef.current !== version) return;
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      if (versionRef.current === version) setLoading(false);
    }
  }, [projectId]);

  useEffect(() => {
    versionRef.current++;
    if (!projectId) {
      setMap(null);
      setLoading(false);
      setError(null);
      return;
    }
    const cached = readCache<ProjectMap>(cacheKey(projectId));
    setMap(cached ?? null);
    setLoading(cached === undefined);
    setError(null);
    void reload();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [projectId]);

  return { map, loading, error, reload };
}
