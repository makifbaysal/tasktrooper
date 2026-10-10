import { describe, expect, it } from "vitest";
import type { Agent } from "@/api";
import { CORE_AGENT_SLUGS, TEAM_TEMPLATES, agentUpdate, coreAgentIds, isCoreAgent } from "@/lib/teamTemplates";

const agent: Agent = {
  id: "a1",
  name: "Backend",
  description: "",
  subagent_type: "generalPurpose",
  system_prompt: "",
  provider_type: "claude_code",
  model: "",
  model_heavy: "",
  tool_policy: {},
  skill_ids: [],
  enabled: false,
  self_evolution_enabled: false,
  max_turns: 40,
  effort: "high",
  catalog_slug: "backend-developer",
  created_at: "2026-10-09T00:00:00Z",
};

describe("agentUpdate", () => {
  it("changes only enabled and carries the CLI turn cap and effort back", () => {
    const input = agentUpdate(agent, true);
    expect(input.enabled).toBe(true);
    expect(input.max_turns).toBe(40);
    expect(input.effort).toBe("high");
    expect(input.name).toBe("Backend");
  });
});

describe("core agents", () => {
  it("every template includes all five core agents, once each", () => {
    for (const template of TEAM_TEMPLATES) {
      for (const slug of CORE_AGENT_SLUGS) expect(template.agents).toContain(slug);
      expect(new Set(template.agents).size).toBe(template.agents.length);
    }
  });

  it("custom is exactly the core agents", () => {
    const custom = TEAM_TEMPLATES.find((template) => template.id === "custom");
    expect([...(custom?.agents ?? [])].sort()).toEqual([...CORE_AGENT_SLUGS].sort());
  });

  it("coreAgentIds finds core agents by slug and skips the rest", () => {
    const list = [...CORE_AGENT_SLUGS, "ui-designer"].map((slug) => ({ ...agent, id: slug, name: slug, catalog_slug: slug }));
    expect(coreAgentIds(list).sort()).toEqual([...CORE_AGENT_SLUGS].sort());
    expect(isCoreAgent(list[0])).toBe(true);
    expect(isCoreAgent(list[5])).toBe(false);
  });

  it("isCoreAgent falls back to the name when the payload has no slug", () => {
    expect(isCoreAgent({ ...agent, name: "Release Engineer", catalog_slug: undefined })).toBe(true);
    expect(isCoreAgent({ ...agent, name: "Backend", catalog_slug: undefined })).toBe(false);
  });
});
