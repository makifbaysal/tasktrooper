import { useCallback, useEffect, useRef, useState } from "react";
import { api, type ProjectsOverview } from "@/api";
import { useCachedState, useFirstLoad } from "@/hooks/useCachedState";

export const CACHE_PROJECTS_OVERVIEW = "projectModel.projectsOverview";

/**
 * Every project's card data in one call (`/v1/projects/overview`) — the
 * projects hub's list. Paints from the last snapshot, keeps it on a failed
 * refresh.
 */
export function useProjectsOverview() {
  const [overview, setOverview] = useCachedState<ProjectsOverview | null>(CACHE_PROJECTS_OVERVIEW, null);
  const [loading, setLoading] = useFirstLoad(CACHE_PROJECTS_OVERVIEW);
  const [error, setError] = useState<string | null>(null);
  const versionRef = useRef(0);

  const reload = useCallback(async () => {
    const version = ++versionRef.current;
    try {
      const next = await api.getProjectsOverview();
      if (versionRef.current !== version) return;
      setOverview(next);
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

  return { overview, loading, error, reload };
}
