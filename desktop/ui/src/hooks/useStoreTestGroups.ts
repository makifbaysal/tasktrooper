import { useCallback, useEffect, useRef, useState } from "react";
import { api, type MobileStorePlatform, type StoreTestGroup } from "@/api";
import { tStatic } from "@/hooks/useI18n";

/**
 * One app's TestFlight groups or Play testing tracks. Reading them reaches the
 * store console (on Play an edit is inserted and deleted), so the list loads
 * the first time `enabled` turns true and is kept after that — refreshing is
 * `reload`'s job. A key change starts over.
 */
export function useStoreTestGroups(
  repositoryId: string | undefined,
  platform: MobileStorePlatform | undefined,
  enabled: boolean,
) {
  const [groups, setGroups] = useState<StoreTestGroup[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);
  const seq = useRef(0);

  const reload = useCallback(async () => {
    if (!repositoryId || !platform) return;
    const ticket = ++seq.current;
    setLoading(true);
    try {
      const next = await api.listStoreTestGroups(repositoryId, platform);
      if (ticket !== seq.current) return;
      setGroups(next);
      setError(null);
    } catch (e) {
      if (ticket !== seq.current) return;
      setError(e instanceof Error ? e.message : tStatic("operations.storeTest.groupsFailed"));
    } finally {
      if (ticket === seq.current) setLoading(false);
    }
  }, [platform, repositoryId]);

  useEffect(() => {
    seq.current += 1;
    setGroups(null);
    setError(null);
    setLoading(false);
  }, [platform, repositoryId]);

  const wanted = enabled && groups === null && error === null;
  useEffect(() => {
    if (wanted) void reload();
  }, [reload, wanted]);

  /** A local edit (auto toggle, new group, tester count); bumps the sequence so an older read cannot undo it. */
  const update = useCallback((fn: (prev: StoreTestGroup[]) => StoreTestGroup[]) => {
    seq.current += 1;
    setLoading(false);
    setGroups((prev) => fn(prev ?? []));
  }, []);

  return { groups, error, loading, reload, update };
}

export type StoreTestGroupsState = ReturnType<typeof useStoreTestGroups>;
