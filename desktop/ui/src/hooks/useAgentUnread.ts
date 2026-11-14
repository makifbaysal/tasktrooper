import { useCallback, useEffect, useMemo, useState } from "react";
import { type Agent, type ActivityItem } from "@/api";
import { ACTIVITY_POLL, useSharedPoll } from "@/hooks/useSharedPoll";
import { keepEqual } from "@/lib/stableState";

const POLL_INTERVAL_MS = 15000;
const LAST_VIEWED_KEY = "tt.agentChat.lastViewedAt";

/**
 * Per-agent "last viewed" timestamps, in localStorage rather than
 * sessionStorage (see lib/uiCache.ts) — a badge that reappeared for every
 * agent on every app launch would train the user to ignore it. Never read or
 * written outside this hook.
 */
function readLastViewed(): Record<string, string> {
  try {
    const raw = window.localStorage.getItem(LAST_VIEWED_KEY);
    return raw ? (JSON.parse(raw) as Record<string, string>) : {};
  } catch {
    return {};
  }
}

function writeLastViewed(next: Record<string, string>): void {
  try {
    window.localStorage.setItem(LAST_VIEWED_KEY, JSON.stringify(next));
  } catch {
    /* private-mode storage or a quota error — the badge just outlives this tab */
  }
}

export interface AgentUnread {
  counts: Record<string, number>;
  /** Advances an agent's last-viewed time from outside its own chat screen — the notification center uses this so reading a chat message there also clears the sidebar badge. */
  markAgentSeen: (agentId: string, iso: string) => void;
}

/**
 * How many `chat_message` activity items each agent in `agents` has newer
 * than the last time this viewer looked, so the sidebar can badge them with
 * a count.
 *
 * Reads the same `ACTIVITY_POLL` the notification center already subscribes
 * to (`/v1/activity`, which now folds `session_messages` in as
 * `chat_message` items) rather than a second signal — one shared poll feeds
 * both surfaces.
 *
 * `activeAgentId` (the agent whose chat is currently open, if any) is never
 * counted, and every poll while it is open advances that agent's last-viewed
 * time — so a reply that arrives while the user is already looking at it
 * never has to be dismissed by hand.
 */
export function useAgentUnread(agents: Agent[], activeAgentId: string | null): AgentUnread {
  const [activity, setActivity] = useState<ActivityItem[]>([]);
  const [lastViewed, setLastViewed] = useState<Record<string, string>>(() => readLastViewed());

  const hasAgents = agents.length > 0;
  useSharedPoll(ACTIVITY_POLL, POLL_INTERVAL_MS, hasAgents, {
    onValue: (data) => setActivity((prev) => keepEqual(prev, data.items ?? [])),
  });

  const markAgentSeen = useCallback((agentId: string, iso: string) => {
    setLastViewed((prev) => {
      if (prev[agentId] && prev[agentId] >= iso) return prev;
      const next = { ...prev, [agentId]: iso };
      writeLastViewed(next);
      return next;
    });
  }, []);

  const latestChatByAgent = useMemo(() => {
    const latest: Record<string, string> = {};
    for (const item of activity) {
      if (item.kind !== "chat_message" || !item.agent_id) continue;
      const prev = latest[item.agent_id];
      if (!prev || item.created_at > prev) latest[item.agent_id] = item.created_at;
    }
    return latest;
  }, [activity]);

  // The agent on screen catches up to "viewed" on every poll, not just on
  // mount — a message that streams in while the chat is already open must
  // not leave that agent looking unread the moment the user clicks away.
  useEffect(() => {
    if (!activeAgentId) return;
    const latest = latestChatByAgent[activeAgentId];
    if (!latest) return;
    markAgentSeen(activeAgentId, latest);
  }, [activeAgentId, latestChatByAgent, markAgentSeen]);

  const counts = useMemo(() => {
    const result: Record<string, number> = {};
    for (const item of activity) {
      if (item.kind !== "chat_message" || !item.agent_id) continue;
      if (item.agent_id === activeAgentId) continue;
      const viewed = lastViewed[item.agent_id];
      if (viewed && item.created_at <= viewed) continue;
      result[item.agent_id] = (result[item.agent_id] ?? 0) + 1;
    }
    return result;
  }, [activity, activeAgentId, lastViewed]);

  return { counts, markAgentSeen };
}
