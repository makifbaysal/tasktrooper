import "@testing-library/jest-dom/vitest";
import { render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { describe, expect, it, vi, beforeEach } from "vitest";
import type { Agent } from "@/api";
import { AgentChatPage } from "@/pages/AgentChatPage";
import { I18nProvider } from "@/hooks/useI18n";

const { listAgentSessions, getAgent, listFiles, listAgents, listInitiativeProjects, listRepositories } =
  vi.hoisted(() => ({
    listAgentSessions: vi.fn(),
    getAgent: vi.fn(),
    listFiles: vi.fn(),
    listAgents: vi.fn(),
    listInitiativeProjects: vi.fn(),
    listRepositories: vi.fn(),
  }));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return {
    ...actual,
    api: {
      ...actual.api,
      listAgentSessions,
      getAgent,
      listFiles,
      listAgents,
      listInitiativeProjects,
      listRepositories,
    },
  };
});

const agent: Agent = {
  id: "agent-1",
  name: "Rex",
  description: "Reviews pull requests",
  subagent_type: "general",
  system_prompt: "",
  provider_type: "",
  model: "",
  model_heavy: "",
  tool_policy: {},
  skill_ids: [],
  enabled: true,
  self_evolution_enabled: false,
  created_at: "2026-09-17T10:00:00Z",
};

function renderChatPage() {
  return render(
    <I18nProvider>
      <MemoryRouter initialEntries={["/agents/agent-1/chat"]}>
        <Routes>
          <Route path="agents/:agentId/chat" element={<AgentChatPage />} />
        </Routes>
      </MemoryRouter>
    </I18nProvider>,
  );
}

describe("AgentChatPage header shortcuts", () => {
  beforeEach(() => {
    listAgentSessions.mockReset().mockResolvedValue({ sessions: [] });
    getAgent.mockReset().mockResolvedValue(agent);
    listFiles.mockReset().mockResolvedValue({ files: [] });
    listAgents.mockReset().mockResolvedValue({ agents: [] });
    listInitiativeProjects.mockReset().mockResolvedValue({ projects: [] });
    listRepositories.mockReset().mockResolvedValue({ repositories: [] });
  });

  it("renders a Memory shortcut next to the Performance shortcut, linking to that agent's memory view", async () => {
    renderChatPage();

    await waitFor(() => expect(getAgent).toHaveBeenCalledWith("agent-1"));

    const performanceLink = await screen.findByRole("link", { name: "Performance" });
    const memoryLink = screen.getByRole("link", { name: "Memory" });

    expect(memoryLink).toHaveAttribute("href", "/agents/agent-1/memory");
    expect(performanceLink).toHaveAttribute("href", "/agents/agent-1/performance");
  });
});
