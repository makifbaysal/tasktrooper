import { useCallback, useEffect, useRef, useState } from "react";
import { api, type RepositoryReadiness } from "@/api";
import { usePolling } from "@/hooks/usePolling";

const POLL_MS = 4000;

/**
 * One repository's setup checklist. It polls only while a step is under way
 * (a scan, an index pass, an open task), so a finished checklist costs
 * nothing; a failed refresh keeps the last answer.
 */
export function useRepositoryReadiness(repositoryId: string | undefined) {
  const [readiness, setReadiness] = useState<RepositoryReadiness | null>(null);
  const [loading, setLoading] = useState(Boolean(repositoryId));
  const [error, setError] = useState<string | null>(null);
  const seq = useRef(0);

  const reload = useCallback(async () => {
    if (!repositoryId) return;
    const mine = ++seq.current;
    try {
      const next = await api.getRepositoryReadiness(repositoryId);
      if (seq.current !== mine) return;
      setReadiness(next);
      setError(null);
    } catch (e) {
      if (seq.current === mine) setError(e instanceof Error ? e.message : String(e));
    } finally {
      if (seq.current === mine) setLoading(false);
    }
  }, [repositoryId]);

  useEffect(() => {
    seq.current++;
    setReadiness(null);
    setError(null);
    setLoading(Boolean(repositoryId));
    void reload();
  }, [repositoryId, reload]);

  const active = readiness?.steps.some((s) => s.status === "running") ?? false;
  usePolling(reload, POLL_MS, active, { leading: false });

  const replace = useCallback((next: RepositoryReadiness) => {
    seq.current++;
    setReadiness(next);
  }, []);

  return { readiness, loading, error, reload, replace };
}
