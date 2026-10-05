import { useCallback, useState } from "react";
import { api, type ActivityItem } from "@/api";
import { usePolling } from "@/hooks/usePolling";

/**
 * Shared `/v1/activity` poll: `ActivityFeed` and `NotificationCenter` both
 * need the same raw feed, filtered differently, so the fetch itself lives
 * here rather than being duplicated.
 */
export function useActivity(limit: number, intervalMs: number): { items: ActivityItem[]; loading: boolean } {
  const [items, setItems] = useState<ActivityItem[]>([]);
  const [loading, setLoading] = useState(true);

  const load = useCallback(async () => {
    try {
      const data = await api.listActivity(limit);
      const next = data.items ?? [];
      // Most 2s ticks return the same feed; keeping the old array skips a
      // re-render of every row.
      setItems((prev) => (JSON.stringify(prev) === JSON.stringify(next) ? prev : next));
    } catch {
      setItems((prev) => (prev.length === 0 ? prev : []));
    } finally {
      setLoading(false);
    }
  }, [limit]);

  usePolling(load, intervalMs, true);

  return { items, loading };
}
