import "@testing-library/jest-dom/vitest";
import { render, screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { Agent } from "@/api";
import { I18nProvider } from "@/hooks/useI18n";
import { AgentSettingsPage } from "@/pages/AgentSettingsPage";

const getAgent = vi.fn<(id: string) => Promise<Agent>>();

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return {
    ...actual,
    api: {
      ...actual.api,
      getAgent: (id: string) => getAgent(id),
      listLLMProviders: async () => ({ providers: [], endpoints: [] }),
      listModels: async () => ({ data: [] }),
    },
  };
});

function agent(slug: string, enabled: boolean): Agent {
  return {
    id: "a1",
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

async function renderSettings() {
  render(
    <I18nProvider>
      <MemoryRouter initialEntries={["/agents/a1/settings"]}>
        <Routes>
          <Route path="/agents/:agentId/settings" element={<AgentSettingsPage />} />
        </Routes>
      </MemoryRouter>
    </I18nProvider>,
  );
  return screen.findByText("Enabled");
}

beforeEach(() => getAgent.mockReset());

describe("AgentSettingsPage enabled switch", () => {
  it("is locked on for a core agent and shows the hint", async () => {
    getAgent.mockResolvedValue(agent("security-agent", true));
    const label = await renderSettings();
    const sw = label.parentElement!.querySelector('[role="switch"]')!;

    expect(sw).toBeChecked();
    expect(sw).toBeDisabled();
    expect(await screen.findByText("Core agent; it cannot be turned off.")).toBeInTheDocument();
  });

  it("lets a core agent that is off be turned on", async () => {
    getAgent.mockResolvedValue(agent("qa-agent", false));
    const label = await renderSettings();
    const sw = label.parentElement!.querySelector('[role="switch"]')!;

    expect(sw).not.toBeChecked();
    expect(sw).not.toBeDisabled();
  });

  it("stays free for a non-core agent", async () => {
    getAgent.mockResolvedValue(agent("ui-designer", true));
    const label = await renderSettings();
    const sw = label.parentElement!.querySelector('[role="switch"]')!;

    expect(sw).not.toBeDisabled();
    expect(screen.queryByText("Core agent; it cannot be turned off.")).not.toBeInTheDocument();
  });
});
