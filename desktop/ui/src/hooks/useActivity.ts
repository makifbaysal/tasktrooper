import { useState } from "react";
import type { ActivityItem } from "@/api";
import { ACTIVITY_POLL, useSharedPoll } from "@/hooks/useSharedPoll";
import { keepEqual } from "@/lib/stableState";

/**
 * The newest `limit` (at most 100) items of `/v1/activity`, read through the
 * shared activity poll: the board's activity dialog, its "agent running"
 * badges and the header's notifications all want the same feed, and the
 * server returns it newest-first, so a shorter list is a prefix of the longer.
 */
export function useActivity(limit: number, intervalMs: number): { items: ActivityItem[]; loading: boolean } {
  const [items, setItems] = useState<ActivityItem[]>([]);
  const [loading, setLoading] = useState(true);

  useSharedPoll(ACTIVITY_POLL, intervalMs, true, {
    onValue: (data) => {
      const next = (data.items ?? []).slice(0, limit);
      setItems((prev) => keepEqual(prev, next));
      setLoading(false);
    },
    onError: () => {
      setItems((prev) => (prev.length === 0 ? prev : []));
      setLoading(false);
    },
  });

  return { items, loading };
}
