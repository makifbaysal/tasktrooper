import { useCallback, useEffect, useRef, useState } from "react";
import {
  api,
  type DesignSystemFile,
  type DesignSystemGenerateResult,
  type ProjectDesignSystemView,
  type RepositoryDesignSystemView,
} from "@/api";
import { usePolling } from "@/hooks/usePolling";
import { hasCache, readCache, writeCache } from "@/lib/uiCache";

// While the design task it opened is still open, the tab re-reads the view so
// a proposal shows up (and an approval lands) without a manual refresh.
const OPEN_REQUEST_POLL_MS = 10_000;

function normalizeProjectView(view: ProjectDesignSystemView): ProjectDesignSystemView {
  return {
    ...view,
    pending: view.pending ?? [],
    versions: view.versions ?? [],
    repositories: view.repositories ?? [],
  };
}

function normalizeRepositoryView(view: RepositoryDesignSystemView): RepositoryDesignSystemView {
  return {
    ...view,
    effective: { ...view.effective, tokens: view.effective?.tokens ?? {} },
    project_choices: view.project_choices ?? [],
    pending_layers: view.pending_layers ?? [],
    layer_versions: view.layer_versions ?? [],
    overrides: view.overrides ?? [],
    lint: view.lint ?? [],
  };
}

/** Cache-then-refresh for one id-keyed view, the same contract as useProjectMap. */
function useCachedView<T>(id: string | undefined, cacheKey: (id: string) => string, load: (id: string) => Promise<T>) {
  const [view, setView] = useState<T | null>(() => (id ? (readCache<T>(cacheKey(id)) ?? null) : null));
  const [loading, setLoading] = useState(() => (id ? !hasCache(cacheKey(id)) : false));
  const [error, setError] = useState<string | null>(null);
  const versionRef = useRef(0);

  const accept = useCallback(
    (next: T) => {
      if (!id) return;
      versionRef.current++;
      writeCache(cacheKey(id), next);
      setView(next);
      setError(null);
      setLoading(false);
    },
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [id],
  );

  const reload = useCallback(async () => {
    if (!id) return;
    const version = ++versionRef.current;
    try {
      const next = await load(id);
      if (versionRef.current !== version) return;
      writeCache(cacheKey(id), next);
      setView(next);
      setError(null);
    } catch (e) {
      if (versionRef.current !== version) return;
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      if (versionRef.current === version) setLoading(false);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [id]);

  useEffect(() => {
    versionRef.current++;
    if (!id) {
      setView(null);
      setLoading(false);
      setError(null);
      return;
    }
    const cached = readCache<T>(cacheKey(id));
    setView(cached ?? null);
    setLoading(cached === undefined);
    setError(null);
    void reload();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [id]);

  return { view, loading, error, reload, accept };
}

const projectCacheKey = (id: string) => `designSystem.project.${id}`;
const repositoryCacheKey = (id: string) => `designSystem.repository.${id}`;

async function loadProjectView(id: string) {
  return normalizeProjectView(await api.getProjectDesignSystem(id));
}

async function loadRepositoryView(id: string) {
  return normalizeRepositoryView(await api.getRepositoryDesignSystem(id));
}

/** `GET /v1/projects/:projectId/design-system` plus its generate action. */
export function useProjectDesignSystem(projectId: string | undefined) {
  const { view, loading, error, reload } = useCachedView(projectId, projectCacheKey, loadProjectView);

  usePolling(reload, OPEN_REQUEST_POLL_MS, !!view?.request?.open, { leading: false });

  const generate = useCallback(
    async (notes: string): Promise<DesignSystemGenerateResult> => {
      if (!projectId) throw new Error("no project");
      const result = await api.generateProjectDesignSystem(projectId, notes.trim());
      void reload();
      return result;
    },
    [projectId, reload],
  );

  return { view, loading, error, reload, generate };
}

/** `GET /v1/repositories/:id/design-system` plus generate and the base-project choice. */
export function useRepositoryDesignSystem(repositoryId: string | undefined) {
  const { view, loading, error, reload, accept } = useCachedView(repositoryId, repositoryCacheKey, loadRepositoryView);

  usePolling(reload, OPEN_REQUEST_POLL_MS, !!view?.request?.open, { leading: false });

  const generate = useCallback(
    async (notes: string): Promise<DesignSystemGenerateResult> => {
      if (!repositoryId) throw new Error("no repository");
      const result = await api.generateRepositoryDesignSystem(repositoryId, notes.trim());
      void reload();
      return result;
    },
    [repositoryId, reload],
  );

  const setBaseProject = useCallback(
    async (projectId: string | null) => {
      if (!repositoryId) return;
      accept(normalizeRepositoryView(await api.setRepositoryDesignBaseProject(repositoryId, projectId)));
    },
    [repositoryId, accept],
  );

  return { view, loading, error, reload, generate, setBaseProject };
}

/**
 * `GET /v1/repositories/:id/design-system/files`, read only once `enabled`
 * (the Files section is opened) and again on `reload`.
 */
export function useDesignSystemFiles(repositoryId: string | undefined, enabled: boolean) {
  const [files, setFiles] = useState<DesignSystemFile[] | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const versionRef = useRef(0);

  const reload = useCallback(async () => {
    if (!repositoryId) return;
    const version = ++versionRef.current;
    setLoading(true);
    setError(null);
    try {
      const answer = await api.getRepositoryDesignSystemFiles(repositoryId);
      if (versionRef.current === version) setFiles(answer.files ?? []);
    } catch (e) {
      if (versionRef.current === version) setError(e instanceof Error ? e.message : String(e));
    } finally {
      if (versionRef.current === version) setLoading(false);
    }
  }, [repositoryId]);

  useEffect(() => {
    versionRef.current++;
    setFiles(null);
    setError(null);
    setLoading(false);
  }, [repositoryId]);

  useEffect(() => {
    if (enabled && files === null && !loading && !error) void reload();
  }, [enabled, files, loading, error, reload]);

  return { files, loading, error, reload };
}
