import { useCallback, useEffect, useRef, useState } from "react";
import {
  api,
  opsTooManyEnvironments,
  type DeployEnvironment,
  type LogSeverity,
  type OpsLogEntry,
  type OpsTooManyEnvironments,
  type OpsUnavailable,
} from "@/api";
import {
  appendOlder,
  compareTimestamps,
  mergeNewer,
  newestTimestamp,
  oldestTimestamp,
  tailSince,
} from "@/components/operations/query/opsQuery";
import { useOptionalOpsContext } from "@/components/operations/shell/OpsContext";
import { usePolling } from "@/hooks/usePolling";
import { readCache, writeCache } from "@/lib/uiCache";

export const OPS_LOGS_PAGE = 200;
export const OPS_LOGS_TAIL_MS = 5_000;
/** Each tail read starts this far before the newest entry shown: providers
 * ingest late, and what is read twice is dropped. */
export const OPS_LOGS_TAIL_OVERLAP_MS = 10_000;
const TAIL_NOW_MARGIN_MS = 1_000;
/** Live tail keeps at most this many entries; the oldest give way. */
export const OPS_LOGS_MAX_SHOWN = 2_000;

export interface OpsLogsQueryState {
  env: DeployEnvironment;
  /** The window picked; a preset's is re-anchored on now when the query runs. */
  from: number;
  to: number;
  preset: boolean;
  window: string;
  componentIds: string[];
  minSeverity?: LogSeverity;
  text: string;
}

/** "more": the window holds older entries than are shown; "stuck": more
 * entries share the oldest instant than one page returns, so paging by time
 * cannot get past it. */
export type OlderState = "more" | "none" | "stuck";

export interface OpsLogsView {
  status: "loading" | "ready" | "error" | "tooMany";
  entries: OpsLogEntry[];
  unavailable: OpsUnavailable[];
  truncated: boolean;
  older: OlderState;
  window: { from: number; to: number };
  error?: string;
  tooMany?: OpsTooManyEnvironments;
  loadingOlder: boolean;
  olderError?: string;
  /** The tail found more new entries than one read returns: some in between are not shown. */
  tailGap: boolean;
  tailError?: string;
}

interface State extends OpsLogsView {
  key: string;
}

interface CachedPage {
  entries: OpsLogEntry[];
  unavailable: OpsUnavailable[];
  truncated: boolean;
  window: { from: number; to: number };
}

export function opsLogsCacheKey(q: OpsLogsQueryState): string {
  const ids = [...q.componentIds].sort().join(",") || "*";
  return `operations.logs.${q.env}.${q.window}.${ids}.${q.minSeverity ?? ""}.${q.text.trim()}`;
}

function initial(key: string, q: OpsLogsQueryState): State {
  const cached = readCache<CachedPage>(key);
  const base = {
    key,
    loadingOlder: false,
    tailGap: false,
  };
  if (cached) {
    return {
      ...base,
      status: "ready",
      entries: cached.entries,
      unavailable: cached.unavailable,
      truncated: cached.truncated,
      older: cached.truncated ? "more" : "none",
      window: cached.window,
    };
  }
  return { ...base, status: "loading", entries: [], unavailable: [], truncated: false, older: "none", window: { from: q.from, to: q.to } };
}

function message(e: unknown): string {
  return e instanceof Error ? e.message : String(e);
}

/** The Logs explorer's reads: one page for the window, "load older" by
 * `until` = the oldest shown (inclusive, so repeats are dropped), and a live
 * tail every 5 s from a little before the newest shown, paused on a hidden tab. */
export function useOpsLogs(query: OpsLogsQueryState, { enabled = true, live = false }: { enabled?: boolean; live?: boolean } = {}) {
  const key = opsLogsCacheKey(query);
  const [state, setState] = useState<State>(() => initial(key, query));
  if (state.key !== key) setState(initial(key, query));

  const latest = useRef(query);
  latest.current = query;
  const view = useRef(state);
  view.current = state;
  const generation = useRef(0);
  const ids = query.componentIds.join(",");

  const params = useCallback(() => {
    const q = latest.current;
    return {
      env: q.env,
      componentIds: q.componentIds.length > 0 ? q.componentIds : undefined,
      minSeverity: q.minSeverity,
      text: q.text,
      limit: OPS_LOGS_PAGE,
    };
  }, []);

  const run = useCallback(async () => {
    if (!enabled) return;
    const mine = ++generation.current;
    const q = latest.current;
    const now = Date.now();
    const window = q.preset ? { from: now - (q.to - q.from), to: now } : { from: q.from, to: q.to };
    setState((prev) => (prev.key === key ? { ...prev, status: prev.status === "ready" ? "ready" : "loading", error: undefined } : prev));
    try {
      const page = await api.listOpsLogs({
        ...params(),
        since: new Date(window.from).toISOString(),
        until: q.preset ? undefined : new Date(window.to).toISOString(),
      });
      if (mine !== generation.current) return;
      const cached: CachedPage = {
        entries: page.entries ?? [],
        unavailable: page.unavailable ?? [],
        truncated: !!page.truncated,
        window,
      };
      writeCache(key, cached);
      setState((prev) =>
        prev.key === key
          ? {
              key,
              status: "ready",
              ...cached,
              older: cached.truncated ? "more" : "none",
              loadingOlder: false,
              tailGap: false,
            }
          : prev,
      );
    } catch (e) {
      if (mine !== generation.current) return;
      const tooMany = opsTooManyEnvironments(e);
      setState((prev) => {
        if (prev.key !== key) return prev;
        if (tooMany) return { ...prev, status: "tooMany", tooMany, entries: [], unavailable: [], truncated: false, older: "none" };
        return prev.status === "ready" ? { ...prev, error: message(e) } : { ...prev, status: "error", error: message(e) };
      });
    }
  }, [enabled, key, params]);

  const loadOlder = useCallback(async () => {
    const cur = view.current;
    if (cur.status !== "ready" || cur.loadingOlder || cur.older !== "more") return;
    const until = oldestTimestamp(cur.entries);
    const since = new Date(cur.window.from).toISOString();
    if (!until || compareTimestamps(until, since) <= 0) {
      setState((prev) => (prev.key === key ? { ...prev, older: "none" } : prev));
      return;
    }
    const mine = generation.current;
    setState((prev) => (prev.key === key ? { ...prev, loadingOlder: true, olderError: undefined } : prev));
    try {
      const page = await api.listOpsLogs({ ...params(), since, until });
      if (mine !== generation.current) return;
      setState((prev) => {
        if (prev.key !== key) return prev;
        const merged = appendOlder(prev.entries, page.entries ?? []);
        const older: OlderState = page.truncated ? (merged.added === 0 ? "stuck" : "more") : "none";
        return { ...prev, entries: merged.entries, unavailable: page.unavailable ?? prev.unavailable, older, loadingOlder: false };
      });
    } catch (e) {
      if (mine !== generation.current) return;
      setState((prev) => (prev.key === key ? { ...prev, loadingOlder: false, olderError: message(e) } : prev));
    }
  }, [key, params]);

  const tail = useCallback(async () => {
    const cur = view.current;
    if (cur.status !== "ready") return;
    const mine = generation.current;
    const since = tailSince(newestTimestamp(cur.entries), cur.window.to, Date.now(), OPS_LOGS_TAIL_OVERLAP_MS, TAIL_NOW_MARGIN_MS);
    try {
      const page = await api.listOpsLogs({ ...params(), since });
      if (mine !== generation.current) return;
      const now = Date.now();
      setState((prev) => {
        if (prev.key !== key) return prev;
        const merged = mergeNewer(prev.entries, page.entries ?? [], since, OPS_LOGS_MAX_SHOWN);
        return {
          ...prev,
          entries: merged.entries,
          unavailable: page.unavailable ?? prev.unavailable,
          window: { from: prev.window.from, to: Math.max(prev.window.to, now) },
          older: merged.trimmed ? "more" : prev.older,
          tailGap: prev.tailGap || (!!page.truncated && merged.added > 0),
          tailError: undefined,
        };
      });
    } catch (e) {
      if (mine !== generation.current) return;
      setState((prev) => (prev.key === key ? { ...prev, tailError: message(e) } : prev));
    }
  }, [key, params]);

  const refreshKey = useOptionalOpsContext()?.refreshKey ?? 0;
  useEffect(() => {
    void run();
  }, [run, refreshKey, ids]);

  usePolling(tail, OPS_LOGS_TAIL_MS, enabled && live && state.status === "ready");

  const result: OpsLogsView & { reload(): Promise<void>; loadOlder(): Promise<void> } = {
    status: state.status,
    entries: state.entries,
    unavailable: state.unavailable,
    truncated: state.truncated,
    older: state.older,
    window: state.window,
    error: state.error,
    tooMany: state.tooMany,
    loadingOlder: state.loadingOlder,
    olderError: state.olderError,
    tailGap: state.tailGap,
    tailError: state.tailError,
    reload: run,
    loadOlder,
  };
  return result;
}
