// The starting teams the first run offers, and the pure rules that turn a
// picked template into a set of agent ids.
//
// The slugs are catalog slugs; the server seeds one agent per catalog entry
// and each carries `catalog_slug`. A server whose response predates that field
// omits it, so matching falls back to the agent's name (see `findAgentBySlug`).

import type { Agent, AgentInput } from "@/api";

export const PRODUCT_MANAGER_SLUG = "product-manager";
export const SECURITY_AGENT_SLUG = "security-agent";

export type TeamTemplateId = "web" | "mobile" | "game" | "data" | "custom";

export interface TeamTemplate {
  id: TeamTemplateId;
  /** i18n keys for the card, so the constant stays language-neutral. */
  nameKey: string;
  descriptionKey: string;
  /** Catalog slugs preselected when this template is picked. */
  agents: readonly string[];
}

export const TEAM_TEMPLATES: readonly TeamTemplate[] = [
  {
    id: "web",
    nameKey: "setup.team.templates.web.name",
    descriptionKey: "setup.team.templates.web.description",
    agents: [
      "product-manager",
      "system-architect",
      "backend-developer",
      "frontend-developer",
      "ui-designer",
      "qa-agent",
      "release-engineer",
    ],
  },
  {
    id: "mobile",
    nameKey: "setup.team.templates.mobile.name",
    descriptionKey: "setup.team.templates.mobile.description",
    agents: [
      "product-manager",
      "system-architect",
      "mobile-developer",
      "backend-developer",
      "ui-designer",
      "qa-agent",
      "release-engineer",
    ],
  },
  {
    id: "game",
    nameKey: "setup.team.templates.game.name",
    descriptionKey: "setup.team.templates.game.description",
    agents: ["product-manager", "game-developer", "ui-designer", "qa-agent"],
  },
  {
    id: "data",
    nameKey: "setup.team.templates.data.name",
    descriptionKey: "setup.team.templates.data.description",
    agents: ["product-manager", "data-scientist", "backend-developer", "qa-agent"],
  },
  {
    id: "custom",
    nameKey: "setup.team.templates.custom.name",
    descriptionKey: "setup.team.templates.custom.description",
    agents: ["product-manager"],
  },
];

export function templateById(id: TeamTemplateId): TeamTemplate | undefined {
  return TEAM_TEMPLATES.find((template) => template.id === id);
}

/**
 * Does this server's agent payload carry the catalog slug? Only then is a slug
 * a trustworthy key; a payload where every agent omits it predates the field.
 */
export function agentsUseCatalogSlug(agents: readonly Agent[]): boolean {
  return agents.some((agent) => Boolean(agent.catalog_slug));
}

function normalizeAgentKey(value: string): string {
  return value.toLowerCase().replace(/[^a-z0-9]/g, "");
}

/** The agent for a catalog slug, by slug where the payload has it and by name otherwise. */
export function findAgentBySlug(agents: readonly Agent[], slug: string): Agent | undefined {
  if (agentsUseCatalogSlug(agents)) {
    return agents.find((agent) => agent.catalog_slug === slug);
  }
  const wanted = normalizeAgentKey(slug);
  return agents.find((agent) => normalizeAgentKey(agent.name) === wanted);
}

/** The ids a template preselects, skipping any catalog entry this install lacks. */
export function templateAgentIds(agents: readonly Agent[], template: TeamTemplate): string[] {
  return template.agents
    .map((slug) => findAgentBySlug(agents, slug)?.id)
    .filter((id): id is string => Boolean(id));
}

/**
 * The PUT body for toggling one agent's `enabled`.
 *
 * `PUT /admin/agents/:id` replaces the whole record (a name is required), so
 * the current definition has to travel with the flag. The two catalog-sync
 * gates are deliberately left out: absent means the server keeps them as they
 * were.
 */
export function agentUpdate(agent: Agent, enabled: boolean): AgentInput {
  return {
    name: agent.name,
    description: agent.description,
    subagent_type: agent.subagent_type,
    system_prompt: agent.system_prompt,
    provider_type: agent.provider_type,
    model: agent.model,
    model_heavy: agent.model_heavy,
    tool_policy: agent.tool_policy,
    enabled,
    self_evolution_enabled: agent.self_evolution_enabled,
  };
}
