import { useCallback, useEffect, useRef, useState } from "react";
import { api, ApiError, TELEMETRY_NOT_CONFIGURED } from "@/api";
import { useOptionalOpsContext } from "@/components/operations/shell/OpsContext";
import { usePolling } from "@/hooks/usePolling";

type Query = Record<string, string | number | boolean | undefined>;

export interface TelemetryQueryState<T> {
  data: T | undefined;
  loading: boolean;
  error?: string;
  /** The server has no telemetry address or key yet. */
  notConfigured: boolean;
  reload(): void;
}

interface Options {
  enabled?: boolean;
  /** 0 reads once; the console's Refresh and a changed query always re-read. */
  intervalMs?: number;
}

/** One read of the telemetry product through the app server's proxy. The last
 * answer stays on screen while the next one loads, and an answer that lands
 * after the query changed is dropped. */
export function useTelemetryQuery<T>(path: string, query?: Query, { enabled = true, intervalMs = 30_000 }: Options = {}): TelemetryQueryState<T> {
  const [data, setData] = useState<T | undefined>(undefined);
  const [loading, setLoading] = useState(enabled);
  const [error, setError] = useState<string | undefined>(undefined);
  const [notConfigured, setNotConfigured] = useState(false);
  const ticket = useRef(0);
  const queryKey = JSON.stringify(query ?? {});

  const load = useCallback(async () => {
    const mine = ++ticket.current;
    try {
      const next = await api.telemetryGet<T>(path, JSON.parse(queryKey) as Query);
      if (mine !== ticket.current) return;
      setData(next);
      setError(undefined);
      setNotConfigured(false);
    } catch (e) {
      if (mine !== ticket.current) return;
      setNotConfigured(e instanceof ApiError && e.type === TELEMETRY_NOT_CONFIGURED);
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      if (mine === ticket.current) setLoading(false);
    }
  }, [path, queryKey]);

  useEffect(() => {
    if (!enabled) {
      ticket.current++;
      setLoading(false);
      return;
    }
    setLoading(true);
    void load();
  }, [enabled, load]);

  usePolling(load, intervalMs, enabled && intervalMs > 0, { leading: false });

  const refreshKey = useOptionalOpsContext()?.refreshKey ?? 0;
  const seen = useRef(refreshKey);
  useEffect(() => {
    if (!enabled || refreshKey === seen.current) return;
    seen.current = refreshKey;
    void load();
  }, [enabled, refreshKey, load]);

  const reload = useCallback(() => void load(), [load]);
  return { data, loading, error, notConfigured, reload };
}
