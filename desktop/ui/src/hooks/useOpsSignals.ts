import { useCallback, useEffect, useRef, useState } from "react";
import { api, type ComponentQueues, type ComponentSeries, type ComponentSignals, type DeployEnvironment, type OpsGolden } from "@/api";
import { autoStepSeconds } from "@/components/operations/charts/signalChart";
import { goldenWindow } from "@/components/operations/console/goldenMetrics";
import { useOptionalOpsContext, type OpsTimeRange } from "@/components/operations/shell/OpsContext";
import { windowKey } from "@/components/operations/query/opsQuery";
import { usePolling } from "@/hooks/usePolling";
import { keepEqual } from "@/lib/stableState";
import { readCache, writeCache } from "@/lib/uiCache";

export const OPS_SIGNALS_POLL_MS = 60_000;

interface KeyedState<T> {
  key: string;
  data: T | null;
  error?: string;
  fetching: boolean;
}

export interface KeyedRead<T> {
  data: T | null;
  error?: string;
  loading: boolean;
  fetching: boolean;
  reload(): Promise<void>;
  /** Puts a write's answer in place; a read already in flight is dropped. */
  update(updater: (prev: T | null) => T | null): void;
}

// One read per key: cache-first paint, a poll while the tab is visible, a
// forced read on the top bar's Refresh. `fetcher` is read at fetch time, so a
// preset window that slides with the clock keeps one cache entry and still
// asks for the window as it is now.
export function useKeyedRead<T>(key: string, fetcher: () => Promise<T>, enabled: boolean, pollMs = OPS_SIGNALS_POLL_MS): KeyedRead<T> {
  const [state, setState] = useState<KeyedState<T>>(() => ({ key, data: readCache<T>(key) ?? null, fetching: false }));
  if (state.key !== key) setState({ key, data: readCache<T>(key) ?? null, fetching: false });
  const latest = useRef(fetcher);
  latest.current = fetcher;
  const seq = useRef(0);

  const load = useCallback(async () => {
    if (!enabled) return;
    const mine = ++seq.current;
    setState((prev) => (prev.key === key ? { ...prev, fetching: true } : prev));
    try {
      const next = await latest.current();
      if (mine !== seq.current) return;
      writeCache(key, next);
      setState((prev) => (prev.key === key ? { key, data: keepEqual(prev.data, next), fetching: false } : prev));
    } catch (e) {
      if (mine !== seq.current) return;
      const message = e instanceof Error ? e.message : String(e);
      setState((prev) => (prev.key === key ? { ...prev, error: message, fetching: false } : prev));
    }
  }, [enabled, key]);

  const update = useCallback(
    (updater: (prev: T | null) => T | null) => {
      seq.current += 1;
      setState((prev) => {
        if (prev.key !== key) return prev;
        const data = updater(prev.data);
        writeCache(key, data ?? undefined);
        return { key, data, fetching: false };
      });
    },
    [key],
  );

  const refreshKey = useOptionalOpsContext()?.refreshKey ?? 0;
  useEffect(() => {
    void load();
  }, [load, refreshKey]);
  usePolling(load, pollMs, enabled, { leading: false });

  const mine = state.key === key;
  return {
    data: mine ? state.data : null,
    error: mine ? state.error : undefined,
    loading: enabled && (!mine || (state.data === null && state.error === undefined)),
    fetching: mine && state.fetching,
    reload: load,
    update,
  };
}

/** One request for every chart on a page: the window's series of each signal
 * asked, with the previous period when compare is on. */
export function useComponentSeries(
  componentId: string,
  env: DeployEnvironment,
  signals: string[],
  range: OpsTimeRange,
  { compare = false, enabled = true }: { compare?: boolean; enabled?: boolean } = {},
): KeyedRead<ComponentSeries> {
  const ids = signals.join(",");
  const key = `operations.series.${componentId}.${env}.${windowKey(range)}.${compare ? "compare" : "plain"}.${ids}`;
  const fetcher = () =>
    api.getComponentSeries(componentId, {
      env,
      signals: ids ? ids.split(",") : [],
      from: range.from,
      to: range.to,
      stepSeconds: autoStepSeconds(range.to - range.from),
      compare,
    });
  return useKeyedRead(key, fetcher, enabled && ids !== "");
}

// The explorer's golden columns change slowly and cost one query per bound
// signal of every component, so they are read half as often as a page's own.
export const OPS_EXPLORER_GOLDEN_POLL_MS = 120_000;

/** The golden metrics of many components at once (the explorer's columns,
 * an entity's tiles): latest value, previous period and a sparkline each.
 * `null` asks for every component with a binding (the explorer: a list of ids
 * would outgrow the URL), and the caller picks its rows from the answer. The
 * window is clamped to the last 30 days of the range (`goldenWindow`). */
export function useOpsGolden(
  env: DeployEnvironment,
  range: OpsTimeRange,
  componentIds: string[] | null,
  enabled = true,
  pollMs = OPS_SIGNALS_POLL_MS,
): KeyedRead<OpsGolden> {
  const ids = componentIds === null ? null : [...new Set(componentIds)].sort().join(",");
  const key = `operations.golden.${env}.${windowKey(range)}.${ids ?? "*"}`;
  const fetcher = () => {
    const { from, to } = goldenWindow(range);
    return api.getOpsGolden({ env, from, to, componentIds: ids ? ids.split(",") : undefined });
  };
  return useKeyedRead(key, fetcher, enabled && ids !== "", pollMs);
}

/** A component's signals in one environment: what feeds each, and the
 * suggestions waiting for a yes or a no. */
export function useComponentSignals(componentId: string, env: DeployEnvironment, enabled = true): KeyedRead<ComponentSignals> {
  const key = `operations.signals.${componentId}.${env}`;
  return useKeyedRead(key, () => api.getComponentSignals(componentId, env), enabled);
}

// The server keeps a queues answer 30 s, so a faster poll reads the same one.
export const OPS_QUEUES_POLL_MS = 30_000;

/** A component's queue tables in one environment, as they are now: one per
 * thing its confirmed `queue.*` bindings read. */
export function useComponentQueues(componentId: string, env: DeployEnvironment, enabled = true): KeyedRead<ComponentQueues> {
  const key = `operations.queues.${componentId}.${env}`;
  return useKeyedRead(key, () => api.getComponentQueues(componentId, env), enabled, OPS_QUEUES_POLL_MS);
}

/** Whether a component has a confirmed binding for one signal in an
 * environment: what a page reads through when the component runs nowhere it
 * can read itself (a Sentry project, a Loki stream). Undefined while unknown;
 * a failed read counts as unbound. */
export function useSignalBound(componentId: string, env: DeployEnvironment, signal: string, enabled = true): boolean | undefined {
  const read = useComponentSignals(componentId, env, enabled);
  if (!enabled) return undefined;
  if (read.data) return Boolean(read.data.signals?.find((s) => s.signal === signal)?.binding);
  return read.error ? false : undefined;
}
