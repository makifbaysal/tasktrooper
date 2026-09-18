import "@testing-library/jest-dom/vitest";
import { render, screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Agent, AgentPerformance } from "@/api";
import { AgentPerformancePage } from "@/pages/AgentPerformancePage";
import { I18nProvider } from "@/hooks/useI18n";

const { getAgent, getAgentPerformance, listAgentEvolutionEvents, listAgentReflections, listAgentMemories } =
  vi.hoisted(() => ({
    getAgent: vi.fn(),
    getAgentPerformance: vi.fn(),
    listAgentEvolutionEvents: vi.fn(),
    listAgentReflections: vi.fn(),
    listAgentMemories: vi.fn(),
  }));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return {
    ...actual,
    api: {
      ...actual.api,
      getAgent,
      getAgentPerformance,
      listAgentEvolutionEvents,
      listAgentReflections,
      listAgentMemories,
    },
  };
});

const agent: Agent = {
  id: "agent-1",
  name: "Quinn",
  description: "QA agent",
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

const perf: AgentPerformance = {
  score: { id: "s1", agent_id: "agent-1", score: 10, runs_total: 2, runs_passed: 2, runs_revised: 0, updated_at: "2026-09-18T10:00:00Z" },
  events: [
    { id: "e1", agent_id: "agent-1", event_type: "qa_task_tested", delta: 5, score_after: 10, created_at: "2026-09-18T10:00:00Z" },
    { id: "e2", agent_id: "agent-1", event_type: "pm_uat_completed", delta: 5, score_after: 5, created_at: "2026-09-17T10:00:00Z" },
  ],
  kpis: [],
  kpi_results: [],
};

function renderPerformancePage() {
  return render(
    <I18nProvider>
      <MemoryRouter initialEntries={["/agents/agent-1/performance"]}>
        <Routes>
          <Route path="agents/:agentId/performance" element={<AgentPerformancePage />} />
        </Routes>
      </MemoryRouter>
    </I18nProvider>,
  );
}

describe("AgentPerformancePage score event labels", () => {
  beforeEach(() => {
    localStorage.clear();
    getAgent.mockReset().mockResolvedValue(agent);
    getAgentPerformance.mockReset().mockResolvedValue(perf);
    listAgentEvolutionEvents.mockReset().mockResolvedValue({ events: [] });
    listAgentReflections.mockReset().mockResolvedValue({ reflections: [] });
    listAgentMemories.mockReset().mockResolvedValue({ memories: [] });
  });

  afterEach(() => {
    localStorage.clear();
  });

  it("labels qa_task_tested and pm_uat_completed events in English", async () => {
    renderPerformancePage();

    expect(await screen.findByText("QA finished testing")).toBeInTheDocument();
    expect(screen.getByText("PM UAT completed")).toBeInTheDocument();
    expect(screen.queryByText("qa_task_tested")).not.toBeInTheDocument();
    expect(screen.queryByText("pm_uat_completed")).not.toBeInTheDocument();
  });

  it("labels qa_task_tested and pm_uat_completed events in Turkish", async () => {
    localStorage.setItem("bridge_locale", "tr");

    renderPerformancePage();

    expect(await screen.findByText("QA testi tamamladı")).toBeInTheDocument();
    expect(screen.getByText("PM UAT'ı tamamladı")).toBeInTheDocument();
    expect(screen.queryByText("qa_task_tested")).not.toBeInTheDocument();
    expect(screen.queryByText("pm_uat_completed")).not.toBeInTheDocument();
  });
});
