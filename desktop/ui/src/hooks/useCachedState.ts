import { useEffect, useRef, useState, type Dispatch, type SetStateAction } from "react";
import { hasCache, readCache, writeCache } from "@/lib/uiCache";

/**
 * useState whose value survives navigation: it starts from the tab's last
 * snapshot for `key` (see lib/uiCache) and writes every update back.
 *
 * Pages use it for the payloads they fetch on mount, so revisiting a page
 * paints the previous data immediately instead of a skeleton while the
 * refresh runs behind it.
 */
export function useCachedState<T>(key: string, fallback: T): [T, Dispatch<SetStateAction<T>>] {
  const [value, setValue] = useState<T>(() => readCache<T>(key) ?? fallback);
  const keyRef = useRef(key);
  keyRef.current = key;
  // Written after commit, not inside the updater (React may run an updater
  // twice), and only when the value actually changed: a poll whose updater
  // hands back `prev` must not serialize the whole payload again.
  const written = useRef(value);

  useEffect(() => {
    if (Object.is(written.current, value)) return;
    written.current = value;
    writeCache(keyRef.current, value);
  }, [value]);

  return [value, setValue];
}

/**
 * The initial value for a page's `loading` flag: true only when nothing has
 * been cached for these keys yet. A revisit renders its snapshot straight away
 * and never shows the full-page skeleton again.
 */
export function useFirstLoad(...keys: string[]): [boolean, Dispatch<SetStateAction<boolean>>] {
  return useState(() => !keys.every((key) => hasCache(key)));
}
