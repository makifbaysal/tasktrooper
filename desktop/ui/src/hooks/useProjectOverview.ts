import { useCallback, useEffect, useRef, useState } from "react";
import { api, type ProjectDetail } from "@/api";
import { hasCache, readCache, writeCache } from "@/lib/uiCache";

function cacheKey(projectId: string): string {
  return `projectModel.project.${projectId}`;
}

/**
 * One project's overview plus its cross-repository review queue
 * (`/v1/projects/:projectId/overview`). Same per-id cache-then-refresh
 * contract as useRepositoryModel.
 */
export function useProjectOverview(projectId: string | undefined) {
  const [project, setProject] = useState<ProjectDetail | null>(() =>
    projectId ? (readCache<ProjectDetail>(cacheKey(projectId)) ?? null) : null,
  );
  const [loading, setLoading] = useState(() => (projectId ? !hasCache(cacheKey(projectId)) : false));
  const [error, setError] = useState<string | null>(null);
  const versionRef = useRef(0);

  const reload = useCallback(async () => {
    if (!projectId) return;
    const version = ++versionRef.current;
    try {
      const next = await api.getProjectOverview(projectId);
      if (versionRef.current !== version) return;
      writeCache(cacheKey(projectId), next);
      setProject(next);
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
      setProject(null);
      setLoading(false);
      setError(null);
      return;
    }
    const cached = readCache<ProjectDetail>(cacheKey(projectId));
    setProject(cached ?? null);
    setLoading(cached === undefined);
    setError(null);
    void reload();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [projectId]);

  return { project, loading, error, reload };
}
