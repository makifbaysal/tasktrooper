import { useCallback, useEffect, useRef, useState } from "react";
import { api, type AlertToken } from "@/api";
import { useCachedState, useFirstLoad } from "@/hooks/useCachedState";
import { usePolling } from "@/hooks/usePolling";
import { keepEqual } from "@/lib/stableState";

export const ALERT_TOKENS_POLL_MS = 60_000;

export const CACHE_ALERT_TOKENS = "operations.alertConnections.tokens";

/** Every repository's alert connections, one list read per repository,
 * re-read every minute (for "last used") and whenever `refreshKey` moves.
 * Lists never carry a token, so they are safe to cache. */
export function useAlertTokens(repositoryIds: string[], refreshKey = 0) {
  const [byRepository, setByRepository] = useCachedState<Record<string, AlertToken[]>>(CACHE_ALERT_TOKENS, {});
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [loading, setLoading] = useFirstLoad(CACHE_ALERT_TOKENS);
  // A change made here bumps it, so a read that started before the change
  // cannot land after it and bring a deleted row back.
  const version = useRef(0);
  const idsRef = useRef(repositoryIds);
  idsRef.current = repositoryIds;
  const idsKey = repositoryIds.join(",");

  const load = useCallback(async () => {
    const ids = idsRef.current;
    if (ids.length === 0) return;
    const started = version.current;
    const results = await Promise.allSettled(ids.map((id) => api.listAlertTokens(id)));
    if (started === version.current) {
      setByRepository((prev) => {
        const next: Record<string, AlertToken[]> = {};
        ids.forEach((id, i) => {
          const r = results[i];
          next[id] = r.status === "fulfilled" ? (r.value?.tokens ?? []) : (prev[id] ?? []);
        });
        return keepEqual(prev, next);
      });
      const failed: Record<string, string> = {};
      ids.forEach((id, i) => {
        const r = results[i];
        if (r.status === "rejected") failed[id] = r.reason instanceof Error ? r.reason.message : String(r.reason);
      });
      setErrors((prev) => keepEqual(prev, failed));
    }
    setLoading(false);
  }, [setByRepository, setLoading]);

  useEffect(() => {
    void load();
  }, [load, idsKey, refreshKey]);

  usePolling(load, ALERT_TOKENS_POLL_MS, repositoryIds.length > 0, { leading: false });

  const add = useCallback(
    (repositoryId: string, token: AlertToken) => {
      version.current += 1;
      setByRepository((prev) => ({
        ...prev,
        [repositoryId]: [token, ...(prev[repositoryId] ?? []).filter((t) => t.id !== token.id)],
      }));
    },
    [setByRepository],
  );

  const remove = useCallback(
    (repositoryId: string, tokenId: string) => {
      version.current += 1;
      setByRepository((prev) => ({
        ...prev,
        [repositoryId]: (prev[repositoryId] ?? []).filter((t) => t.id !== tokenId),
      }));
    },
    [setByRepository],
  );

  return { byRepository, errors, loading, reload: load, add, remove };
}
