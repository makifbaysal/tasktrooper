import { useCallback, useEffect, useRef, useState } from "react";
import { api, type RepositoryModel } from "@/api";
import { hasCache, readCache, writeCache } from "@/lib/uiCache";

function cacheKey(repositoryId: string): string {
  return `projectModel.repository.${repositoryId}`;
}

/**
 * One repository's full component/check/link/note model. Paints from the
 * last snapshot for this id (see lib/uiCache) while it refreshes, and keeps
 * that snapshot on a failed reload rather than blanking the page — the same
 * contract useCachedState/useFirstLoad give a fixed-key page, extended to a
 * dynamic per-id key so switching between sibling repositories stays instant.
 */
export function useRepositoryModel(repositoryId: string | undefined) {
  const [model, setModel] = useState<RepositoryModel | null>(() =>
    repositoryId ? (readCache<RepositoryModel>(cacheKey(repositoryId)) ?? null) : null,
  );
  const [loading, setLoading] = useState(() => (repositoryId ? !hasCache(cacheKey(repositoryId)) : false));
  const [error, setError] = useState<string | null>(null);
  const versionRef = useRef(0);

  const reload = useCallback(async () => {
    if (!repositoryId) return;
    const version = ++versionRef.current;
    try {
      const next = await api.getRepositoryModel(repositoryId);
      if (versionRef.current !== version) return;
      writeCache(cacheKey(repositoryId), next);
      setModel(next);
      setError(null);
    } catch (e) {
      if (versionRef.current !== version) return;
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      if (versionRef.current === version) setLoading(false);
    }
  }, [repositoryId]);

  useEffect(() => {
    versionRef.current++;
    if (!repositoryId) {
      setModel(null);
      setLoading(false);
      setError(null);
      return;
    }
    const cached = readCache<RepositoryModel>(cacheKey(repositoryId));
    setModel(cached ?? null);
    setLoading(cached === undefined);
    setError(null);
    void reload();
    // reload is stable per repositoryId (its own useCallback dep); re-running
    // it here on every identity change would refetch on each render instead
    // of once per id.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [repositoryId]);

  return { model, loading, error, reload };
}
