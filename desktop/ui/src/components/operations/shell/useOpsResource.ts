import { useCallback, useEffect, useRef, useState } from "react";
import { useOptionalOpsContext } from "@/components/operations/shell/OpsContext";
import { useCachedState, useFirstLoad } from "@/hooks/useCachedState";
import { useSharedPoll } from "@/hooks/useSharedPoll";
import { createSharedPoll, type SharedPoll } from "@/lib/sharedPoll";
import { keepEqual } from "@/lib/stableState";

/** One endpoint the console reads from several mounted places at once (the
 * shell's breadcrumb, the page, its sub-page): one timer, one request in flight,
 * and one forced read when the top bar's Refresh is pressed. */
export interface OpsResource<T> {
  poll: SharedPoll<T>;
  fetchNow(): Promise<T>;
}

export function createOpsResource<T>(fetcher: () => Promise<T>): OpsResource<T> {
  let forced: Promise<T> | null = null;
  return {
    poll: createSharedPoll(fetcher),
    fetchNow() {
      if (!forced) {
        forced = fetcher().finally(() => {
          forced = null;
        });
      }
      return forced;
    },
  };
}

export interface OpsResourceState<T> {
  value: T;
  loading: boolean;
  error?: string;
  reload(): void;
}

export function useOpsResource<T>(
  resource: OpsResource<T>,
  cacheKey: string,
  fallback: T,
  intervalMs: number,
  enabled = true,
): OpsResourceState<T> {
  const [value, setValue] = useCachedState<T>(cacheKey, fallback);
  const [loading, setLoading] = useFirstLoad(cacheKey);
  const [error, setError] = useState<string | undefined>(undefined);
  const version = useRef(0);

  const accept = useCallback(
    (next: T) => {
      setValue((prev) => keepEqual(prev, next));
      setError(undefined);
      setLoading(false);
    },
    [setValue, setLoading],
  );
  const reject = useCallback(
    (err: unknown) => {
      setError(err instanceof Error ? err.message : String(err));
      setLoading(false);
    },
    [setLoading],
  );

  useSharedPoll(resource.poll, intervalMs, enabled, {
    begin: () => version.current,
    onValue: (next, ticket) => {
      if (ticket === version.current) accept(next);
    },
    onError: reject,
  });

  const reload = useCallback(() => {
    const mine = ++version.current;
    resource.poll.invalidate();
    resource.fetchNow().then(
      (next) => {
        if (mine === version.current) accept(next);
      },
      (err: unknown) => {
        if (mine === version.current) reject(err);
      },
    );
  }, [resource, accept, reject]);

  const refreshKey = useOptionalOpsContext()?.refreshKey ?? 0;
  const seenKey = useRef(refreshKey);
  useEffect(() => {
    if (!enabled || refreshKey === seenKey.current) return;
    seenKey.current = refreshKey;
    reload();
  }, [enabled, refreshKey, reload]);

  return { value, loading, error, reload };
}
