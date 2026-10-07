import { useEffect, useRef } from "react";
import { api } from "@/api";
import { createSharedPoll, type SharedPoll, type SharedPollListener } from "@/lib/sharedPoll";

/** `GET /v1/tasks` — the header's notifications, the board and the backlog. */
export const ALL_TASKS_POLL = createSharedPoll(() => api.listAllTasks());

/** `GET /v1/activity?limit=100` — the header's notifications and the board's "agent running" badges. */
export const ACTIVITY_POLL = createSharedPoll(() => api.listActivity(100));

type Handlers<T> = Omit<SharedPollListener<T>, "intervalMs">;

/** Subscribes while `enabled`; the handlers may change every render without resubscribing. */
export function useSharedPoll<T>(poll: SharedPoll<T>, intervalMs: number, enabled: boolean, handlers: Handlers<T>) {
  const latest = useRef(handlers);
  latest.current = handlers;

  useEffect(() => {
    if (!enabled) return;
    return poll.subscribe({
      intervalMs,
      begin: () => latest.current.begin?.() ?? 0,
      onValue: (value, ticket) => latest.current.onValue(value, ticket),
      onError: (error) => latest.current.onError?.(error),
    });
  }, [poll, intervalMs, enabled]);
}
