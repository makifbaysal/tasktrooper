import { describe, expect, it } from "vitest";
import type { Agent } from "@/api";
import { agentUpdate } from "@/lib/teamTemplates";

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
