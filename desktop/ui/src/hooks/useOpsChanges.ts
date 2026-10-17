import { useCallback, useEffect, useRef, useState } from "react";
import { ApiError, api, type Release } from "@/api";
import {
  CHANGE_STATUSES,
  CHANGES_IDLE_POLL_MS,
  CHANGES_LIVE_POLL_MS,
  IN_FLIGHT_RELEASE_STATUSES,
  changesPollMs,
  isInFlightRelease,
  isMovingRelease,
} from "@/components/operations/changes/changeFeed";
import { useOptionalOpsContext } from "@/components/operations/shell/OpsContext";
import { useOpsDirectory, type OpsEntity } from "@/components/operations/shell/useOpsEntities";
import { useCachedState, useFirstLoad } from "@/hooks/useCachedState";
import { usePolling } from "@/hooks/usePolling";
import { keepEqual } from "@/lib/stableState";

export const CHANGE_PAGE_SIZE = 100;
export const CHANGE_MAX_PAGES = 5;

export type ChangeScope = { repositoryId: string } | { componentId: string } | { all: true };

export function changeScopeKey(scope: ChangeScope): string {
  if ("componentId" in scope) return `c:${scope.componentId}`;
  if ("repositoryId" in scope) return `r:${scope.repositoryId}`;
  return "all";
}

const ALL_CHANGES: ChangeScope = { all: true };

/** What to read for an entity's changes, the way `useEntityReleases` does:
 * by repository when it is the repository's only component (its releases
 * may predate component_id), else by component. Not ready until the
 * directory says which. */
export function useChangeScope(entity: OpsEntity | undefined): { scope: ChangeScope; soleComponent: boolean; ready: boolean } {
  const { repositories, loaded } = useOpsDirectory();
  if (!entity) return { scope: ALL_CHANGES, soleComponent: false, ready: true };
  const repository = repositories.find((r) => r.id === entity.repositoryId);
  const soleComponent = repository?.componentCount === 1;
  return {
    scope: soleComponent ? { repositoryId: entity.repositoryId } : { componentId: entity.id },
    soleComponent,
    ready: loaded,
  };
}

export interface ChangeFeedSnapshot {
  releases: Release[];
  /** created_at (ms) of the oldest release read; null once the server has nothing older. */
  coveredFrom: number | null;
  /** The window start this read was asked to reach. */
  fetchedFrom: number;
  /** The custom range's end it paged back from; null for a window ending now. */
  fetchedUntil: number | null;
}

const EMPTY: ChangeFeedSnapshot = { releases: [], coveredFrom: null, fetchedFrom: Number.MAX_SAFE_INTEGER, fetchedUntil: null };

/** At most this many settled releases are read back one by one per poll. */
const MAX_REREAD = 20;

function scopeQuery(scope: ChangeScope): { repositoryId?: string; componentId?: string } {
  if ("componentId" in scope) return { componentId: scope.componentId };
  if ("repositoryId" in scope) return { repositoryId: scope.repositoryId };
  return {};
}

function isoOf(ms: number | undefined): string | undefined {
  return ms === undefined ? undefined : new Date(ms).toISOString();
}

/** One list per id, the first list's copy winning, in the order first seen. */
function mergeReleases(...lists: Release[][]): Release[] {
  const seen = new Set<string>();
  const out: Release[] = [];
  for (const list of lists) {
    for (const r of list) {
      if (seen.has(r.id)) continue;
      seen.add(r.id);
      out.push(r);
    }
  }
  return out;
}

async function readInFlight(scope: ChangeScope): Promise<Release[]> {
  const { releases } = await api.listAllReleases({ ...scopeQuery(scope), statuses: IN_FLIGHT_RELEASE_STATUSES, limit: CHANGE_PAGE_SIZE });
  return releases ?? [];
}

/** Pages back (newest first) from the window's end — now, or a custom
 * range's `until` — until its start is covered, the server runs out, or
 * CHANGE_MAX_PAGES pages were read. Whatever is pending or moving is read
 * on its own beside it, so an old one is never paged out of sight. */
export async function fetchChangeFeed(scope: ChangeScope, from: number, until?: number): Promise<ChangeFeedSnapshot> {
  const inFlight = readInFlight(scope);
  const paged: Release[] = [];
  let before = isoOf(until);
  let coveredFrom: number | null = null;
  for (let page = 0; page < CHANGE_MAX_PAGES; page++) {
    const { releases: batch } = await api.listAllReleases({
      ...scopeQuery(scope),
      statuses: CHANGE_STATUSES,
      limit: CHANGE_PAGE_SIZE,
      before,
    });
    const rows = batch ?? [];
    paged.push(...rows);
    const oldest = rows.at(-1)?.created_at;
    if (rows.length < CHANGE_PAGE_SIZE || !oldest) {
      coveredFrom = null;
      break;
    }
    coveredFrom = Date.parse(oldest);
    if (!Number.isFinite(coveredFrom) || coveredFrom <= from) break;
    before = oldest;
  }
  return { releases: mergeReleases(paged, await inFlight), coveredFrom, fetchedFrom: from, fetchedUntil: until ?? null };
}

export interface ChangeHead {
  releases: Release[];
  /** Listed releases the server no longer has. */
  gone: string[];
}

/** What a poll reads: the newest page of the window and whatever is in
 * flight, then, by id, each release `known` held in flight that neither
 * answer carries any more — it has settled since. */
export async function fetchChangeHead(scope: ChangeScope, until: number | undefined, known: Release[]): Promise<ChangeHead> {
  const [first, inFlight] = await Promise.all([
    api.listAllReleases({ ...scopeQuery(scope), statuses: CHANGE_STATUSES, limit: CHANGE_PAGE_SIZE, before: isoOf(until) }),
    readInFlight(scope),
  ]);
  const fresh = mergeReleases(inFlight, first.releases ?? []);
  const got = new Set(fresh.map((r) => r.id));
  const settled = known.filter((r) => isInFlightRelease(r) && !got.has(r.id)).slice(0, MAX_REREAD);
  const gone: string[] = [];
  const reread = await Promise.all(
    settled.map((r) =>
      api.getRelease(r.id).catch((err: unknown) => {
        if (err instanceof ApiError && err.status === 404) gone.push(r.id);
        return null;
      }),
    ),
  );
  return { releases: mergeReleases(fresh, reread.filter((r): r is Release => r !== null)), gone };
}

/** A poll's answer folded into what is shown: rows it carries are replaced,
 * new ones go on top, everything older the full read paged in stays. */
export function applyChangeHead(prev: ChangeFeedSnapshot, head: ChangeHead): ChangeFeedSnapshot {
  const byId = new Map(head.releases.map((r) => [r.id, r]));
  const gone = new Set(head.gone);
  const known = new Set(prev.releases.map((r) => r.id));
  const added = head.releases.filter((r) => !known.has(r.id));
  const kept = prev.releases.filter((r) => !gone.has(r.id)).map((r) => byId.get(r.id) ?? r);
  return { ...prev, releases: [...added, ...kept] };
}

export interface ChangeFeedWindow {
  from: number;
  /** A custom range's end; absent for a window that ends now. */
  until?: number;
}

/** The change feed for one scope: every page the window needs, read in full
 * on mount, on Refresh and when the window moves further back or to another
 * end; between those only the head is polled — every 15 s while a listed
 * release is moving, every minute otherwise. Mount under a key of the scope:
 * the cache key follows it. */
export function useReleaseFeed(scope: ChangeScope, span: ChangeFeedWindow) {
  const cacheKey = `operations.changes.${changeScopeKey(scope)}`;
  const [snapshot, setSnapshot] = useCachedState<ChangeFeedSnapshot>(cacheKey, EMPTY);
  const [loading, setLoading] = useFirstLoad(cacheKey);
  const [error, setError] = useState<string | undefined>(undefined);
  const version = useRef(0);
  const scopeRef = useRef(scope);
  scopeRef.current = scope;
  const { from, until } = span;
  const fromRef = useRef(from);
  fromRef.current = from;
  const untilRef = useRef(until);
  untilRef.current = until;
  const snapshotRef = useRef(snapshot);
  snapshotRef.current = snapshot;
  const untilKey = until ?? null;

  const load = useCallback(async () => {
    const mine = ++version.current;
    try {
      const next = await fetchChangeFeed(scopeRef.current, fromRef.current, untilRef.current);
      if (mine !== version.current) return;
      setSnapshot((prev) => keepEqual(prev, next));
      setError(undefined);
    } catch (err) {
      if (mine !== version.current) return;
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      if (mine === version.current) setLoading(false);
    }
  }, [setSnapshot, setLoading]);

  // A head read never cancels a full one; a full read (or an action's
  // answer) started after it does cancel it.
  const loadHead = useCallback(async () => {
    const mine = version.current;
    try {
      const head = await fetchChangeHead(scopeRef.current, untilRef.current, snapshotRef.current.releases);
      if (mine !== version.current) return;
      setSnapshot((prev) => keepEqual(prev, applyChangeHead(prev, head)));
      setError(undefined);
    } catch (err) {
      if (mine !== version.current) return;
      setError(err instanceof Error ? err.message : String(err));
    }
  }, [setSnapshot]);

  const refreshKey = useOptionalOpsContext()?.refreshKey ?? 0;
  useEffect(() => {
    void load();
  }, [load, refreshKey, untilKey]);

  const reachesFurther =
    snapshot.fetchedUntil === untilKey &&
    from < snapshot.fetchedFrom &&
    snapshot.coveredFrom !== null &&
    from < snapshot.coveredFrom;
  useEffect(() => {
    if (reachesFurther) void load();
  }, [reachesFurther, load]);

  usePolling(loadHead, changesPollMs(snapshot.releases), true, { leading: false });

  const apply = useCallback(
    (updated: Release) => {
      version.current += 1;
      setSnapshot((prev) => ({ ...prev, releases: prev.releases.map((r) => (r.id === updated.id ? updated : r)) }));
      void loadHead();
    },
    [setSnapshot, loadHead],
  );

  return {
    releases: snapshot.releases,
    loading,
    error,
    truncated: snapshot.coveredFrom !== null && snapshot.coveredFrom > from,
    reload: load,
    apply,
  };
}

function newer(a: Release | undefined, b: Release | undefined): Release | undefined {
  if (!a) return b;
  if (!b) return a;
  return Date.parse(b.updated_at) > Date.parse(a.updated_at) ? b : a;
}

/** One release for the detail panel, read on its own so a shared link opens
 * even when the list does not hold it; `listed` paints first. */
export function useReleaseDetail(id: string | null, listed: Release | undefined) {
  const [fetched, setFetched] = useState<Release | undefined>(undefined);
  const [error, setError] = useState<string | undefined>(undefined);
  const [loading, setLoading] = useState(id !== null);
  const current = useRef(id);
  current.current = id;

  const load = useCallback(async () => {
    if (!id) return;
    try {
      const next = await api.getRelease(id);
      if (current.current !== id) return;
      setFetched((prev) => keepEqual(prev, next));
      setError(undefined);
    } catch (err) {
      if (current.current !== id) return;
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      if (current.current === id) setLoading(false);
    }
  }, [id]);

  useEffect(() => {
    setFetched(undefined);
    setError(undefined);
    setLoading(id !== null);
    void load();
  }, [id, load]);

  const release = newer(fetched?.id === id ? fetched : undefined, listed?.id === id ? listed : undefined);
  const moving = release ? isMovingRelease(release) : false;
  usePolling(load, moving ? CHANGES_LIVE_POLL_MS : CHANGES_IDLE_POLL_MS, id !== null, { leading: false });

  const apply = useCallback((updated: Release) => setFetched(updated), []);

  return { release, loading: loading && !release, error, reload: load, apply };
}
