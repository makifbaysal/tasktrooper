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
      setItems(data.items ?? []);
    } catch {
      setItems([]);
    } finally {
      setLoading(false);
    }
  }, [limit]);

  usePolling(load, intervalMs, true);

  return { items, loading };
}
