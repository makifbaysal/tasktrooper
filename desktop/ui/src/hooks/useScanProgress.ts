import { useCallback, useEffect, useRef, useState } from "react";
import { api, type ProjectScan } from "@/api";
import { isScanFinished } from "@/lib/project-model";

interface UseScanProgressOptions {
  /** Whether to poll at all — off while the caller has no reason to watch a scan yet. */
  enabled?: boolean;
  pollMs?: number;
}

/**
 * Polls a repository's latest scan every `pollMs` (default 1000) until it
 * reaches a finished status, then stops — the add-repository flow and the
 * repository page's scan banner both watch a scan this way.
 */
export function useScanProgress(repositoryId: string | undefined, options: UseScanProgressOptions = {}) {
  const { enabled = true, pollMs = 1000 } = options;
  const [scan, setScan] = useState<ProjectScan | null>(null);
  const [error, setError] = useState<string | null>(null);
  const versionRef = useRef(0);

  const refresh = useCallback(async () => {
    if (!repositoryId) return;
    const version = ++versionRef.current;
    try {
      const res = await api.getLatestRepositoryScan(repositoryId);
      if (versionRef.current !== version) return;
      setScan(res.scan);
      setError(null);
    } catch (e) {
      if (versionRef.current !== version) return;
      setError(e instanceof Error ? e.message : String(e));
    }
  }, [repositoryId]);

  useEffect(() => {
    versionRef.current++;
    setScan(null);
    setError(null);
  }, [repositoryId]);

  const finished = isScanFinished(scan);

  useEffect(() => {
    if (!repositoryId || !enabled || finished) return;
    void refresh();
    const timer = window.setInterval(() => {
      void refresh();
    }, pollMs);
    return () => window.clearInterval(timer);
  }, [repositoryId, enabled, finished, pollMs, refresh]);

  return { scan, finished, error, refresh };
}
