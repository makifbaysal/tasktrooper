import { useCallback, useEffect, useRef } from "react";
import { toast } from "sonner";
import { api, type DataSource, type DataSourceKindInfo, type ObservabilityRecipe, type ObservabilitySignal } from "@/api";
import { useCachedState, useFirstLoad } from "@/hooks/useCachedState";
import { useI18n } from "@/hooks/useI18n";
import { usePolling } from "@/hooks/usePolling";
import { keepEqual } from "@/lib/stableState";

export const DATA_SOURCES_POLL_MS = 60_000;

const CACHE_KINDS = "operations.sources.kinds";
const CACHE_SOURCES = "operations.sources.list";
const CACHE_RECIPES = "operations.sources.recipes";
const CACHE_SIGNALS = "operations.sources.signals";

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
