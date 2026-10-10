import "@testing-library/jest-dom/vitest";
import { render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { Agent } from "@/api";
import { TeamAgentsEditor } from "@/components/setup/TeamAgentsEditor";
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
  agent("arch", "system-architect", true),
  agent("backend", "backend-developer", true),
  agent("frontend", "frontend-developer"),
  agent("design", "ui-designer"),
  agent("qa", "qa-agent", true),
  agent("release", "release-engineer", true),
  agent("security", "security-agent", true),
];

beforeEach(() => {
  listAgents.mockReset();
  updateAgent.mockReset();
  updateAgent.mockImplementation(async () => AGENTS[0]);
  listAgents.mockResolvedValue({ agents: AGENTS });
});

function renderEditor(onSaved = vi.fn()) {
  render(
    <I18nProvider>
      <TeamAgentsEditor startFrom="current" confirmLabel="Save" onSaved={onSaved} />
    </I18nProvider>,
  );
  return onSaved;
}

describe("TeamAgentsEditor from the current team", () => {
  it("preselects the enabled agents without picking a template and locks the core", async () => {
    renderEditor();

    expect(await screen.findByRole("switch", { name: "backend-developer" })).toBeChecked();
    expect(screen.getByRole("switch", { name: "frontend-developer" })).not.toBeChecked();
    expect(screen.getByRole("switch", { name: "product-manager" })).toBeDisabled();
  });

  it("writes only the flags that changed", async () => {
    const onSaved = renderEditor();

    (await screen.findByRole("switch", { name: "frontend-developer" })).click();
    await waitFor(() => expect(screen.getByRole("switch", { name: "frontend-developer" })).toBeChecked());
    screen.getByRole("switch", { name: "backend-developer" }).click();
    await waitFor(() => expect(screen.getByRole("switch", { name: "backend-developer" })).not.toBeChecked());
    screen.getByRole("button", { name: "Save" }).click();

    await waitFor(() => expect(onSaved).toHaveBeenCalled());
    const calls = updateAgent.mock.calls.map(([id, input]) => [id, (input as { enabled: boolean }).enabled]);
    expect(calls).toEqual(
      expect.arrayContaining([
        ["frontend", true],
        ["backend", false],
      ]),
    );
    expect(updateAgent).toHaveBeenCalledTimes(2);
  });
});

describe("TeamAgentsEditor dialog layout", () => {
  it("scrolls its body and keeps the switches and save button rendered", async () => {
    render(
      <I18nProvider>
        <TeamAgentsEditor startFrom="current" layout="dialog" confirmLabel="Save" onSaved={vi.fn()} />
      </I18nProvider>,
    );

    expect(await screen.findAllByRole("switch")).toHaveLength(AGENTS.length);
    expect(screen.getByTestId("team-editor-body").className).toContain("overflow-y-auto");
    expect(screen.getByRole("button", { name: "Save" })).toBeInTheDocument();
  });
});
