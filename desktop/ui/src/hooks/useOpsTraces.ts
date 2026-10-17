import { useCallback, useEffect, useRef, useState } from "react";
import { api, ApiError, type ComponentTraces, type DeployEnvironment, type TraceView } from "@/api";
import { traceWindow } from "@/components/operations/traces/waterfallLayout";
import { windowKey } from "@/components/operations/query/opsQuery";
import { useOptionalOpsContext, type OpsTimeRange } from "@/components/operations/shell/OpsContext";
import { useKeyedRead, type KeyedRead } from "@/hooks/useOpsSignals";
import { readCache, writeCache } from "@/lib/uiCache";

export const OPS_TRACES_POLL_MS = 60_000;

// Enough to see what is slow; past it, a minimum duration narrows the list.
export const OPS_TRACES_LIMIT = 50;

/** A component's trace search over the console's range (cut to its last 7
 * days), through its confirmed traces.spans binding. */
export function useComponentTraces(
  componentId: string,
  env: DeployEnvironment,
  range: OpsTimeRange,
  { minDurationMs, limit = OPS_TRACES_LIMIT, enabled = true }: { minDurationMs?: number; limit?: number; enabled?: boolean } = {},
): KeyedRead<ComponentTraces> {
  const key = `operations.traces.${componentId}.${env}.${windowKey(range)}.${minDurationMs ?? 0}.${limit}`;
  const fetcher = () => {
    const { since, until } = traceWindow(range);
    return api.getComponentTraces(componentId, { env, since, until, minDurationMs, limit });
  };
  return useKeyedRead(key, fetcher, enabled, OPS_TRACES_POLL_MS);
}

export type TraceReadStatus = "loading" | "ready" | "notFound" | "unbound" | "timeout" | "failed";

export interface TraceRead {
  status: TraceReadStatus;
  view?: TraceView;
  error?: string;
  /** The trace was found through another component's binding than the one asked. */
  foundElsewhere: boolean;
  reload: () => void;
}

function statusOf(err: unknown): TraceReadStatus {
  if (err instanceof ApiError) {
    if (err.status === 404) return "notFound";
    if (err.status === 422 && err.type === "unbound") return "unbound";
    if (err.status === 504 || err.status === 408 || err.type === "timeout") return "timeout";
  }
  return "failed";
}

/** One trace, read through the component's binding — or, when the component
 * has none (a log line of a service whose traces another component's source
 * holds), looked up in every traces source of the environment. A trace does
 * not change once ended, so it is read once per id (and on Refresh). */
export function useTraceView(componentId: string | undefined, traceId: string | undefined, env: DeployEnvironment): TraceRead {
  const key = traceId ? `operations.trace.${env}.${componentId ?? "*"}.${traceId}` : "";
  const cached = key ? readCache<TraceView>(key) : undefined;
  const [state, setState] = useState<{ key: string; status: TraceReadStatus; view?: TraceView; error?: string }>(() => ({
    key,
    status: cached ? "ready" : "loading",
    view: cached,
  }));
  if (state.key !== key) setState({ key, status: cached ? "ready" : "loading", view: cached });
  const seq = useRef(0);
  const refreshKey = useOptionalOpsContext()?.refreshKey ?? 0;

  const load = useCallback(async () => {
    if (!traceId) return;
    const mine = ++seq.current;
    const done = (next: { status: TraceReadStatus; view?: TraceView; error?: string }) => {
      if (mine !== seq.current) return;
      if (next.view) writeCache(key, next.view);
      setState((prev) => (prev.key === key ? { key, ...next, view: next.view ?? prev.view } : prev));
    };
    try {
      done({ status: "ready", view: await api.getOperationsTrace(traceId, { env, componentId }) });
    } catch (err) {
      if (componentId && statusOf(err) === "unbound") {
        try {
          done({ status: "ready", view: await api.getOperationsTrace(traceId, { env }) });
          return;
        } catch (inner) {
          done({ status: statusOf(inner), error: inner instanceof Error ? inner.message : String(inner) });
          return;
        }
      }
      done({ status: statusOf(err), error: err instanceof Error ? err.message : String(err) });
    }
  }, [componentId, traceId, env, key]);

  useEffect(() => {
    void load();
  }, [load, refreshKey]);

  const mine = state.key === key;
  const view = mine ? state.view : undefined;
  return {
    status: mine ? state.status : "loading",
    view,
    error: mine ? state.error : undefined,
    foundElsewhere: Boolean(view && componentId && view.component_id !== componentId),
    reload: () => void load(),
  };
}
