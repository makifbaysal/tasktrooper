import "@testing-library/jest-dom/vitest";
import { render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { Agent } from "@/api";
import { TeamTemplateStep } from "@/components/setup/TeamTemplateStep";
import { I18nProvider } from "@/hooks/useI18n";

const listAgents = vi.fn<() => Promise<{ agents: Agent[] }>>();
const updateAgent = vi.fn<(id: string, input: unknown) => Promise<Agent>>();

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return {
    ...actual,
    api: { ...actual.api, listAgents: () => listAgents(), updateAgent: (id: string, input: unknown) => updateAgent(id, input) },
  };
});

function agent(id: string, slug: string, enabled = false): Agent {
  return {
    id,
    name: slug,
    description: "",
    subagent_type: "generalPurpose",
    system_prompt: "",
    provider_type: "",
    model: "",
    model_heavy: "",
    tool_policy: {},
    skill_ids: [],
    enabled,
    self_evolution_enabled: false,
    catalog_slug: slug,
    created_at: "",
  };
}

const AGENTS: Agent[] = [
  agent("pm", "product-manager", true),
  agent("arch", "system-architect"),
  agent("backend", "backend-developer"),
  agent("frontend", "frontend-developer"),
  agent("mobile", "mobile-developer"),
  agent("design", "ui-designer"),
  agent("qa", "qa-agent"),
  agent("release", "release-engineer"),
  agent("security", "security-agent"),
];

async function renderWithTemplates() {
  listAgents.mockResolvedValue({ agents: AGENTS });
  render(
    <I18nProvider>
      <TeamTemplateStep onDone={vi.fn()} />
    </I18nProvider>,
  );
  return screen.findByRole("button", { name: /Web application/ });
}

beforeEach(() => {
  listAgents.mockReset();
  updateAgent.mockReset();
  updateAgent.mockImplementation(async () => AGENTS[0]);
});

describe("TeamTemplateStep", () => {
  it("preselects the template's agents and leaves the rest off", async () => {
    (await renderWithTemplates()).click();

    expect(await screen.findByRole("switch", { name: "product-manager" })).toBeChecked();
    expect(screen.getByRole("switch", { name: "frontend-developer" })).toBeChecked();
    expect(screen.getByRole("switch", { name: "backend-developer" })).toBeChecked();
    expect(screen.getByRole("switch", { name: "security-agent" })).not.toBeChecked();
    expect(screen.getByRole("switch", { name: "mobile-developer" })).not.toBeChecked();
  });

  it("cannot turn the product manager off", async () => {
    (await renderWithTemplates()).click();

    const pm = await screen.findByRole("switch", { name: "product-manager" });
    expect(pm).toBeDisabled();
    expect(pm).toBeChecked();
  });

  it("writes enabled for every agent the template changed", async () => {
    const onDone = vi.fn();
    listAgents.mockResolvedValue({ agents: AGENTS });
    render(
      <I18nProvider>
        <TeamTemplateStep onDone={onDone} />
      </I18nProvider>,
    );
    (await screen.findByRole("button", { name: /Web application/ })).click();
    (await screen.findByRole("button", { name: "Create this team" })).click();

    await waitFor(() => expect(onDone).toHaveBeenCalled());

    const changed = updateAgent.mock.calls.map(([id, input]) => [id, (input as { enabled: boolean }).enabled]);
    expect(changed).toEqual(
      expect.arrayContaining([
        ["frontend", true],
        ["backend", true],
        ["arch", true],
        ["design", true],
        ["qa", true],
        ["release", true],
      ]),
    );
    expect(changed.map(([id]) => id)).not.toContain("pm");
    expect(changed.map(([id]) => id)).not.toContain("security");
    expect(updateAgent).toHaveBeenCalledTimes(6);
  });
});
