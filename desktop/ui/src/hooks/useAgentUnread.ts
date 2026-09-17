import { useEffect, useMemo, useState } from "react";
import { api, type Agent } from "@/api";
import { usePolling } from "@/hooks/usePolling";

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

/**
 * Which agents in `agents` have a chat message newer than the last time this
 * viewer had that agent's chat open, so the sidebar can put a dot next to
 * them.
 *
 * There is no server-side "unread" concept (a chat session carries no
 * viewer identity, this being a single-user app) — this polls the one cheap
 * signal that already carries every session's `updated_at`
 * (`GET /v1/sessions`, no filter) rather than one request per agent, and
 * compares it to the viewer's own last-viewed timestamps.
 *
 * `activeAgentId` (the agent whose chat is currently open, if any) is never
 * reported unread, and every poll while it is open advances that agent's
 * last-viewed time — so a reply that arrives while the user is already
 * looking at it never has to be dismissed by hand.
 */
export function useAgentUnread(agents: Agent[], activeAgentId: string | null): Set<string> {
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
      setLatestByAgent(latest);
    },
    POLL_INTERVAL_MS,
    hasAgents,
  );

  // The agent on screen catches up to "viewed" on every poll, not just on
  // mount — a message that streams in while the chat is already open must
  // not leave that agent looking unread the moment the user clicks away.
  useEffect(() => {
    if (!activeAgentId) return;
    const latest = latestByAgent[activeAgentId];
    if (!latest) return;
    if (lastViewed[activeAgentId] === latest) return;
    const next = { ...lastViewed, [activeAgentId]: latest };
    setLastViewed(next);
    writeLastViewed(next);
  }, [activeAgentId, latestByAgent, lastViewed]);

  return useMemo(() => {
    const unread = new Set<string>();
    for (const agent of agents) {
      if (agent.id === activeAgentId) continue;
      const latest = latestByAgent[agent.id];
      if (!latest) continue;
      const viewed = lastViewed[agent.id];
      if (!viewed || latest > viewed) unread.add(agent.id);
    }
    return unread;
  }, [agents, activeAgentId, latestByAgent, lastViewed]);
}
