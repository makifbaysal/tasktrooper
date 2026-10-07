import { useCallback, useEffect, useRef, useState } from "react";
import { api, ApiError, type MobileStorePlatform, type StoreTestBuild } from "@/api";
import { isTestBuildActive, mergeTestBuilds } from "@/components/operations/storeTestBuilds";
import { usePolling } from "@/hooks/usePolling";
import { tStatic } from "@/hooks/useI18n";
import { keepEqual } from "@/lib/stableState";

const POLL_MS = 5_000;

interface TestBuildFilter {
  platform?: MobileStorePlatform;
  taskId?: string;
  limit?: number;
}

/**
 * A repository's test builds, narrowed to one task and/or platform, newest
 * first. `builds` is null until the first answer. `unavailable` is the
 * server's 503 — test builds are not configured on this install — which a
 * caller renders as nothing rather than as an error. Polls only while a build
 * is still moving on its own.
 */
export function useStoreTestBuilds(repositoryId: string, filter: TestBuildFilter, enabled = true) {
  const { platform, taskId, limit } = filter;
  const [builds, setBuilds] = useState<StoreTestBuild[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [unavailable, setUnavailable] = useState(false);
  // Only the newest read may write: the task drawer swaps tasks in place, and
  // a mutation's answer must not be overwritten by a poll that left before it.
  const seq = useRef(0);

  const load = useCallback(async () => {
    const ticket = ++seq.current;
    const next = await api.listStoreTestBuilds(repositoryId, { platform, taskId, limit });
    if (ticket !== seq.current) return;
    setBuilds((prev) => keepEqual(prev, next));
    setError(null);
  }, [limit, platform, repositoryId, taskId]);

  useEffect(() => {
    seq.current += 1;
    setBuilds(null);
    setError(null);
    setUnavailable(false);
    if (!enabled) return;
    load().catch((e: unknown) => {
      setUnavailable(e instanceof ApiError && e.status === 503);
      setError(e instanceof Error ? e.message : tStatic("operations.storeTest.buildsFailed"));
      setBuilds((prev) => prev ?? []);
    });
  }, [enabled, load]);

  const active = enabled && !!builds?.some((b) => isTestBuildActive(b.status));
  const poll = useCallback(() => load().catch(() => undefined), [load]);
  usePolling(poll, POLL_MS, active, { leading: false });

  const merge = useCallback((incoming: StoreTestBuild[]) => {
    seq.current += 1;
    setBuilds((prev) => mergeTestBuilds(prev ?? [], incoming));
  }, []);

  return { builds, error, unavailable, reload: load, merge };
}
