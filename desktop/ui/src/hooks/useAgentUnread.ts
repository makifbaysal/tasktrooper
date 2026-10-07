import { useCallback, useEffect, useMemo, useState } from "react";
import { api, type Agent } from "@/api";
import { usePolling } from "@/hooks/usePolling";
import { keepEqual } from "@/lib/stableState";

const POLL_INTERVAL_MS = 20_000;
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
  unread: Set<string>;
  /** Advances an agent's last-viewed time from outside its own chat screen — the notification center uses this so reading a comment there also clears the sidebar dot. */
  markAgentSeen: (agentId: string, iso: string) => void;
}

/**
 * Which agents in `agents` have a chat message newer than the last time this
 * viewer looked, so the sidebar can put a dot next to them.
 *
 * Deliberately DM-only: a task comment or a column move is about a task, not
 * a message from the agent, and lighting up the sidebar for those trained the
 * user to check chats that had nothing new in them. `GET /v1/sessions` is the
 * one signal that means "this agent said something to me."
 *
 * There is no server-side "unread" concept (a chat session carries no
 * viewer identity, this being a single-user app) — this polls that one cheap
 * signal, which already carries every session's timestamp, rather than one
 * request per agent, and compares it to the viewer's own last-viewed
 * timestamps.
 *
 * `activeAgentId` (the agent whose chat is currently open, if any) is never
 * reported unread, and every poll while it is open advances that agent's
 * last-viewed time — so a reply that arrives while the user is already
 * looking at it never has to be dismissed by hand.
 */
export function useAgentUnread(agents: Agent[], activeAgentId: string | null): AgentUnread {
  const [latestByAgent, setLatestByAgent] = useState<Record<string, string>>({});
  const [lastViewed, setLastViewed] = useState<Record<string, string>>(() => readLastViewed());

  const hasAgents = agents.length > 0;
  usePolling(
    async () => {
      const { sessions } = await api.listSessions();
      const latest: Record<string, string> = {};
      for (const sess of sessions ?? []) {
        if (!sess.agent_id) continue;
        const prev = latest[sess.agent_id];
        if (!prev || sess.updated_at > prev) latest[sess.agent_id] = sess.updated_at;
      }
      // The whole shell (header, sidebar) reads this; an unchanged answer must not re-render it.
      setLatestByAgent((prev) => keepEqual(prev, latest));
    },
    POLL_INTERVAL_MS,
    hasAgents,
  );

  const markAgentSeen = useCallback((agentId: string, iso: string) => {
    setLastViewed((prev) => {
      if (prev[agentId] && prev[agentId] >= iso) return prev;
      const next = { ...prev, [agentId]: iso };
      writeLastViewed(next);
      return next;
    });
  }, []);

  // The agent on screen catches up to "viewed" on every poll, not just on
  // mount — a message that streams in while the chat is already open must
  // not leave that agent looking unread the moment the user clicks away.
  useEffect(() => {
    if (!activeAgentId) return;
    const latest = latestByAgent[activeAgentId];
    if (!latest) return;
    markAgentSeen(activeAgentId, latest);
  }, [activeAgentId, latestByAgent, markAgentSeen]);

  const unread = useMemo(() => {
    const set = new Set<string>();
    for (const agent of agents) {
      if (agent.id === activeAgentId) continue;
      const latest = latestByAgent[agent.id];
      if (!latest) continue;
      const viewed = lastViewed[agent.id];
      if (!viewed || latest > viewed) set.add(agent.id);
    }
    return set;
  }, [agents, activeAgentId, latestByAgent, lastViewed]);

  return { unread, markAgentSeen };
}
