import { useCallback, useEffect, useMemo, useRef } from "react";
import { toast } from "sonner";
import {
  api,
  type CloudAccount,
  type DataSource,
  type DataSourceKindInfo,
  type ObservabilityRecipe,
  type ObservabilitySignal,
} from "@/api";
import { useCachedState, useFirstLoad } from "@/hooks/useCachedState";
import { useI18n } from "@/hooks/useI18n";
import { usePolling } from "@/hooks/usePolling";
import { keepEqual } from "@/lib/stableState";

export const DATA_SOURCES_POLL_MS = 60_000;

const CACHE_KINDS = "operations.sources.kinds";
const CACHE_SOURCES = "operations.sources.list";
const CACHE_RECIPES = "operations.sources.recipes";
const CACHE_SIGNALS = "operations.sources.signals";
const CACHE_CLOUD_ACCOUNTS = "operations.sources.cloudAccounts";

/** The configured data sources and the kinds they can be, cached and
 * re-read every minute so a check run from elsewhere (another window, an
 * agent) shows here too. */
export function useDataSources() {
  const { t } = useI18n();
  const [kinds, setKinds] = useCachedState<DataSourceKindInfo[]>(CACHE_KINDS, []);
  const [sources, setSources] = useCachedState<DataSource[]>(CACHE_SOURCES, []);
  const [loading, setLoading] = useFirstLoad(CACHE_KINDS, CACHE_SOURCES);
  const failedOnce = useRef(false);
  // A change made here bumps it, so a list read that started before the
  // change cannot land after it and bring the old row back.
  const version = useRef(0);

  const load = useCallback(async () => {
    const started = version.current;
    const [k, s] = await Promise.allSettled([api.listDataSourceKinds(), api.listDataSources()]);
    if (k.status === "fulfilled") setKinds((prev) => keepEqual(prev, k.value.kinds ?? []));
    if (s.status === "fulfilled" && started === version.current) {
      setSources((prev) => keepEqual(prev, s.value.data_sources ?? []));
    }
    const failure = [k, s].find((r) => r.status === "rejected") as PromiseRejectedResult | undefined;
    if (failure && !failedOnce.current) {
      failedOnce.current = true;
      toast.error(failure.reason instanceof Error ? failure.reason.message : t("operations.sources.loadFailed"));
    }
    if (!failure) failedOnce.current = false;
    setLoading(false);
  }, [t, setKinds, setSources, setLoading]);

  usePolling(load, DATA_SOURCES_POLL_MS, true);

  const upsert = useCallback(
    (source: DataSource) => {
      version.current += 1;
      setSources((prev) =>
        prev.some((s) => s.id === source.id) ? prev.map((s) => (s.id === source.id ? source : s)) : [...prev, source],
      );
    },
    [setSources],
  );

  const remove = useCallback(
    (id: string) => {
      version.current += 1;
      setSources((prev) => prev.filter((s) => s.id !== id));
    },
    [setSources],
  );

  return { kinds, sources, loading, upsert, remove };
}

/** The signal and recipe catalog: fixed for a running server, read once per visit. */
export function useRecipeCatalog() {
  const { t } = useI18n();
  const [recipes, setRecipes] = useCachedState<ObservabilityRecipe[]>(CACHE_RECIPES, []);
  const [signals, setSignals] = useCachedState<ObservabilitySignal[]>(CACHE_SIGNALS, []);
  const [loading, setLoading] = useFirstLoad(CACHE_RECIPES, CACHE_SIGNALS);

  useEffect(() => {
    let cancelled = false;
    void (async () => {
      const [r, s] = await Promise.allSettled([api.listRecipes(), api.listSignals()]);
      if (cancelled) return;
      if (r.status === "fulfilled") setRecipes((prev) => keepEqual(prev, r.value.recipes ?? []));
      if (s.status === "fulfilled") setSignals((prev) => keepEqual(prev, s.value.signals ?? []));
      const failure = [r, s].find((x) => x.status === "rejected") as PromiseRejectedResult | undefined;
      if (failure) {
        toast.error(failure.reason instanceof Error ? failure.reason.message : t("operations.sources.recipes.loadFailed"));
      }
      setLoading(false);
    })();
    return () => {
      cancelled = true;
    };
  }, [t, setRecipes, setSignals, setLoading]);

  return { recipes, signals, loading };
}

/** The signal catalog alone, by id, for the units and directions a list row
 * needs: the cached one at once, read once per visit when there is none, and
 * a failed read leaves the rows to their own units. */
export function useSignalCatalog(): ReadonlyMap<string, ObservabilitySignal> {
  const [signals, setSignals] = useCachedState<ObservabilitySignal[]>(CACHE_SIGNALS, []);
  const known = signals.length > 0;

  useEffect(() => {
    if (known) return;
    let cancelled = false;
    Promise.resolve()
      .then(() => api.listSignals())
      .then((res) => {
        if (!cancelled) setSignals((prev) => keepEqual(prev, res?.signals ?? []));
      })
      .catch(() => {});
    return () => {
      cancelled = true;
    };
  }, [known, setSignals]);

  return useMemo(() => new Map(signals.map((s) => [s.id, s])), [signals]);
}

/** The connected cloud accounts a `cloud_account` field picks from, re-read
 * with the data sources so one connected in Settings meanwhile shows up. A
 * failed read keeps the last list: it only fills a picker and an offer. */
export function useCloudAccounts() {
  const [accounts, setAccounts] = useCachedState<CloudAccount[]>(CACHE_CLOUD_ACCOUNTS, []);
  const [loading, setLoading] = useFirstLoad(CACHE_CLOUD_ACCOUNTS);

  const load = useCallback(async () => {
    try {
      const res = await api.listCloudAccounts();
      const next = res?.accounts ?? [];
      setAccounts((prev) => keepEqual(prev, next));
    } catch {
      // The picker says how to connect one; nothing else depends on this read.
    } finally {
      setLoading(false);
    }
  }, [setAccounts, setLoading]);

  usePolling(load, DATA_SOURCES_POLL_MS, true);

  return { accounts, loading, reload: load };
}
