import { useCallback, useEffect, useRef, useState } from "react";
import { api, type CatalogSyncProgress } from "@/api";
import { usePolling } from "@/hooks/usePolling";

const BUSY_POLL_MS = 2000;
const IDLE_POLL_MS = 15000;

/** A sync that is doing visible work: adding an agent or indexing skills. A pass with nothing new finishes in a blink and is not worth a spinner. */
export function catalogSyncBusy(progress: CatalogSyncProgress | null | undefined): boolean {
  if (!progress?.running) return false;
  return progress.new_agent || progress.skills_total > 0 || (progress.agents_added?.length ?? 0) > 0;
}

/**
 * Follows the catalog sync. `onAgentsChanged` fires when the sync finishes an
 * agent it created and when it ends, so the caller can refresh its roster
 * while a long sync is still adding the rest.
 */
export function useCatalogSync(onAgentsChanged?: () => void) {
  const [progress, setProgress] = useState<CatalogSyncProgress | null>(null);
  const busy = catalogSyncBusy(progress);
  const callbackRef = useRef(onAgentsChanged);
  callbackRef.current = onAgentsChanged;
  const lastAdded = useRef(0);
  const wasBusy = useRef(false);

  const refresh = useCallback(async () => {
    try {
      const res = await api.getCatalogStatus();
      setProgress(res.configured ? (res.progress ?? null) : null);
    } catch {
      setProgress(null);
    }
  }, []);

  usePolling(refresh, busy ? BUSY_POLL_MS : IDLE_POLL_MS, true);

  const added = progress?.agents_added?.length ?? 0;
  useEffect(() => {
    if (added > lastAdded.current) callbackRef.current?.();
    lastAdded.current = added;
  }, [added]);

  useEffect(() => {
    if (wasBusy.current && !busy) callbackRef.current?.();
    wasBusy.current = busy;
  }, [busy]);

  return { progress, busy, refresh };
}
