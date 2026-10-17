import { useCallback, useEffect, useRef, useState } from "react";
import { api, type DeployEnvironment, type OpsErrors } from "@/api";
import { useOptionalOpsContext } from "@/components/operations/shell/OpsContext";
import { usePolling } from "@/hooks/usePolling";
import { readCache, writeCache } from "@/lib/uiCache";
import { keepEqual } from "@/lib/stableState";

export const OPS_ERRORS_POLL_MS = 60_000;

export interface OpsErrorsQueryState {
  env: DeployEnvironment;
  since: number;
  componentIds: string[];
  /** The window's identity (`windowKey`): a sliding preset keeps one cache entry. */
  window: string;
  /** Also the groups an `ignore` error definition hides. */
  includeIgnored?: boolean;
}

interface State {
  key: string;
  data: OpsErrors | null;
  error?: string;
  fetching: boolean;
}

export function opsErrorsCacheKey(q: Pick<OpsErrorsQueryState, "env" | "componentIds" | "window" | "includeIgnored">): string {
  return `operations.errors.${q.env}.${q.window}.${[...q.componentIds].sort().join(",") || "*"}${q.includeIgnored ? ".ignored" : ""}`;
}

/** The Errors inbox's one read: cached per query so a revisit paints at once,
 * polled while the tab is visible, read again on the top bar's Refresh. A
 * failed read keeps the last answer and says why. */
export function useOpsErrors(query: OpsErrorsQueryState, enabled = true) {
  const key = opsErrorsCacheKey(query);
  const [state, setState] = useState<State>(() => ({ key, data: readCache<OpsErrors>(key) ?? null, fetching: false }));
  if (state.key !== key) setState({ key, data: readCache<OpsErrors>(key) ?? null, fetching: false });

  const since = useRef(query.since);
  since.current = query.since;
  const seq = useRef(0);
  const ids = query.componentIds.join(",");
  const env = query.env;
  const includeIgnored = query.includeIgnored === true;

  const load = useCallback(async () => {
    if (!enabled) return;
    const mine = ++seq.current;
    setState((prev) => (prev.key === key ? { ...prev, fetching: true } : prev));
    try {
      const next = await api.listOpsErrors({
        env,
        since: new Date(since.current).toISOString(),
        componentIds: ids ? ids.split(",") : undefined,
        ...(includeIgnored ? { includeIgnored } : {}),
      });
      if (mine !== seq.current) return;
      const answer: OpsErrors = { groups: next.groups ?? [], unavailable: next.unavailable ?? [], truncated: !!next.truncated };
      if (next.ignored_count) answer.ignored_count = next.ignored_count;
      writeCache(key, answer);
      setState((prev) => (prev.key === key ? { key, data: keepEqual(prev.data, answer), fetching: false } : prev));
    } catch (e) {
      if (mine !== seq.current) return;
      const message = e instanceof Error ? e.message : String(e);
      setState((prev) => (prev.key === key ? { ...prev, error: message, fetching: false } : prev));
    }
  }, [enabled, env, ids, includeIgnored, key]);

  const refreshKey = useOptionalOpsContext()?.refreshKey ?? 0;
  useEffect(() => {
    void load();
  }, [load, refreshKey]);
  usePolling(load, OPS_ERRORS_POLL_MS, enabled, { leading: false });

  return {
    data: state.key === key ? state.data : null,
    error: state.key === key ? state.error : undefined,
    loading: enabled && state.data === null && state.error === undefined,
    fetching: state.fetching,
    reload: load,
  };
}
