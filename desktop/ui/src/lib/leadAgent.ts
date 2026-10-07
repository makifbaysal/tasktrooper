import type { NavigateFunction } from "react-router-dom";
import { api, type Agent, type MessageMention, type Role } from "@/api";

/**
 * The lead agent is the one the stakeholder talks to: the product manager,
 * which turns requests into board tasks and hands them to the rest of the team.
 * Resolved by role first (the same `product_manager` key the server routes
 * refinements through), so a renamed or custom PM still leads; the catalog
 * slug is only the fallback for a server that cannot list roles.
 */
export const LEAD_ROLE_KEY = "product_manager";
export const LEAD_CATALOG_SLUG = "product-manager";

export function resolveLeadAgent(agents: Agent[], roles: Role[] | null): Agent | null {
  const enabled = agents.filter((a) => a.enabled);
  const byId = new Map(enabled.map((a) => [a.id, a]));

  const role = roles?.find((r) => r.key === LEAD_ROLE_KEY);
  if (role) {
    // Assignments arrive ordered by priority, then age. An any-area assignment
    // wins over an area-scoped one, mirroring the server's AgentForRole.
    const assigned = role.assignments.filter((a) => byId.has(a.agent_id));
    const pick = assigned.find((a) => a.areas === null || a.areas.length === 0) ?? assigned[0];
    if (pick) return byId.get(pick.agent_id) ?? null;
  }

  return enabled.find((a) => a.catalog_slug === LEAD_CATALOG_SLUG) ?? null;
}

/**
 * Every agent holding the lead's role. The lead works its own column (pm_uat)
 * and the stakeholder's chats, never a board card as its assignee — the server
 * refuses that, so the pickers leave these agents out.
 */
export function productManagerIds(agents: Agent[], roles: Role[] | null): string[] {
  const role = roles?.find((r) => r.key === LEAD_ROLE_KEY);
  if (role) return [...new Set(role.assignments.map((a) => a.agent_id))];
  if (roles) return [];
  return agents.filter((a) => a.catalog_slug === LEAD_CATALOG_SLUG).map((a) => a.id);
}

export function agentInitials(name: string): string {
  const parts = name
    .trim()
    .split(/[\s\-_]+/)
    .filter(Boolean);
  if (parts.length === 0) return "?";
  if (parts.length === 1) return parts[0].slice(0, 2).toLocaleUpperCase("tr");
  return (parts[0][0] + parts[1][0]).toLocaleUpperCase("tr");
}

/**
 * Opens a new conversation with the lead and hands the chat page the first
 * message to send, as router state — the chat page owns sending (streaming,
 * recovery, error handling), so this never posts the message itself.
 */
export async function startLeadConversation(
  navigate: NavigateFunction,
  agentId: string,
  message: string,
  title: string,
  mentions: MessageMention[] = [],
): Promise<void> {
  const session = await api.createSession({ title, agent_id: agentId });
  navigate(`/agents/${agentId}/chat/${session.id}`, { state: { autoSend: message, autoSendMentions: mentions } });
}
