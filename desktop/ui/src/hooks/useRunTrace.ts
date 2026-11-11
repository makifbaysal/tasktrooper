import { useCallback, useEffect, useRef, useState } from "react";
import { api, type RunTraceEvent, type RunTraceSummary } from "@/api";
import { usePolling } from "@/hooks/usePolling";

const LIVE_POLL_MS = 4000;

interface RunTraceState {
  recorded: boolean | null;
  summary: RunTraceSummary | null;
  events: RunTraceEvent[];
  loading: boolean;
  error: string | null;
}

const EMPTY: RunTraceState = { recorded: null, summary: null, events: [], loading: false, error: null };

/**
 * A run's trace: its summary and every recorded call, read page by page. Only
 * the calls after the last one already held are fetched on a refresh; a live
 * run is refreshed while the section is open.
 */
export function useRunTrace(repositoryId: string, taskId: string, runId: string | null, enabled: boolean, live: boolean) {
  const [state, setState] = useState<RunTraceState>(EMPTY);
  const lastSeq = useRef(0);
  const current = useRef<string | null>(null);

  useEffect(() => {
    current.current = runId;
    lastSeq.current = 0;
    setState(runId ? { ...EMPTY, loading: true } : EMPTY);
  }, [runId]);

  const refresh = useCallback(async () => {
    if (!runId) return;
    try {
      const head = await api.getRunTraceSummary(repositoryId, taskId, runId);
      if (current.current !== runId) return;
      if (!head.recorded || !head.summary) {
        setState({ ...EMPTY, recorded: false });
        return;
      }
      const fresh: RunTraceEvent[] = [];
      let after = lastSeq.current;
      for (let page = 0; page < 20; page++) {
        const res = await api.listRunTrace(repositoryId, taskId, runId, after);
        if (current.current !== runId) return;
        fresh.push(...res.events);
        if (!res.next_after) break;
        after = res.next_after;
      }
      if (fresh.length > 0) lastSeq.current = fresh[fresh.length - 1].seq;
      setState((prev) => ({
        recorded: true,
        summary: head.summary ?? null,
        events: fresh.length > 0 ? [...prev.events, ...fresh] : prev.events,
        loading: false,
        error: null,
      }));
    } catch (e) {
      if (current.current === runId) {
        setState((prev) => ({ ...prev, loading: false, error: e instanceof Error ? e.message : String(e) }));
      }
    }
  }, [repositoryId, taskId, runId]);

  usePolling(refresh, LIVE_POLL_MS, enabled && !!runId && live);
  useEffect(() => {
    if (enabled && runId && !live) void refresh();
  }, [enabled, runId, live, refresh]);

  return { ...state, refresh };
}
