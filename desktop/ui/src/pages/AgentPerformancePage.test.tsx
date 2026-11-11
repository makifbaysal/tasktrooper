import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, within } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Agent, AgentPerformance, AgentReflection } from "@/api";
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
    { id: "e2", agent_id: "agent-1", event_type: "human_uat_failed", delta: -5, score_after: 5, created_at: "2026-09-17T10:00:00Z" },
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

  it("labels qa_task_tested and human_uat_failed events in English", async () => {
    renderPerformancePage();

    expect(await screen.findByText("QA finished testing")).toBeInTheDocument();
    expect(screen.getByText("Human UAT failed")).toBeInTheDocument();
    expect(screen.queryByText("qa_task_tested")).not.toBeInTheDocument();
    expect(screen.queryByText("human_uat_failed")).not.toBeInTheDocument();
  });

  it("labels qa_task_tested and human_uat_failed events in Turkish", async () => {
    localStorage.setItem("bridge_locale", "tr");

    renderPerformancePage();

    expect(await screen.findByText("QA testi tamamladı")).toBeInTheDocument();
    expect(screen.getByText("İnsan UAT başarısız")).toBeInTheDocument();
    expect(screen.queryByText("qa_task_tested")).not.toBeInTheDocument();
    expect(screen.queryByText("human_uat_failed")).not.toBeInTheDocument();
  });
});

const reflectionWithDecision: AgentReflection = {
  id: "r1",
  agent_id: "agent-1",
  trigger: "periodic",
  status: "completed",
  window_start: "2026-09-10T00:00:00Z",
  window_end: "2026-09-17T00:00:00Z",
  summary: "Analyzed recent runs.",
  created_at: "2026-09-17T12:00:00Z",
  completed_at: "2026-09-17T12:05:00Z",
  raw_output: "raw model output",
  performance_snapshot: { score: 15, kpi_composite: 80, kpis: { qa_speed: 0.9 }, golden_pass_rate: 0.85, captured_at: "2026-09-17T12:00:00Z" },
  decision: {
    analysis: "The agent improved on QA speed this window.",
    self_assessment: "Tightened the QA checklist skill after two revisions.",
    baseline: { score: 10, kpi_composite: 70, kpis: { qa_speed: 0.7 }, golden_pass_rate: 0.8, captured_at: "2026-09-10T12:00:00Z" },
    catalog_before: { skills: 3, rules: 2, memories: 5, skill_budget: 10, rule_budget: 10 },
    catalog_after: { skills: 4, rules: 2, memories: 5, skill_budget: 10, rule_budget: 10 },
    changes: [
      { kind: "skill", action: "create", name: "QA checklist", outcome: "applied", reason: "Repeated QA misses.", event_id: "ev1" },
      { kind: "rule", action: "update", name: "Revision policy", outcome: "not_applied", reason: "Golden gate regressed." },
    ],
    gate: { before_rate: 0.8, after_rate: 0.85, keep: true, rolled_back: 0 },
  },
};

describe("AgentPerformancePage reflection history", () => {
  beforeEach(() => {
    localStorage.clear();
    getAgent.mockReset().mockResolvedValue(agent);
    getAgentPerformance.mockReset().mockResolvedValue(perf);
    listAgentEvolutionEvents.mockReset().mockResolvedValue({ events: [] });
    listAgentReflections.mockReset().mockResolvedValue({ reflections: [reflectionWithDecision] });
    listAgentMemories.mockReset().mockResolvedValue({ memories: [] });
  });

  afterEach(() => {
    localStorage.clear();
  });

  it("renders the history list above the KPI targets section", async () => {
    renderPerformancePage();

    const heading = await screen.findByText("Analysis History");
    const kpiHeading = screen.getByText("KPI Targets");
    expect(heading.compareDocumentPosition(kpiHeading) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  });

  it("opens the detail dialog and shows applied/not-applied changes and the before/after table", async () => {
    renderPerformancePage();

    fireEvent.click(await screen.findByText("Tightened the QA checklist skill after two revisions."));

    const dialog = (await screen.findByRole("dialog")) as HTMLElement;
    const inDialog = within(dialog);

    expect(inDialog.getByText("QA checklist")).toBeInTheDocument();
    expect(inDialog.getByText("Revision policy")).toBeInTheDocument();
    expect(inDialog.getByText("applied")).toBeInTheDocument();
    expect(inDialog.getByText("not applied")).toBeInTheDocument();

    expect(inDialog.getByText("Performance score")).toBeInTheDocument();
    expect(inDialog.getByText("10.0")).toBeInTheDocument();
    expect(inDialog.getByText("15.0")).toBeInTheDocument();
  });
});

// A decision that changed nothing is stored with a nil slice on the server, so
// `changes` arrives as null rather than []. Reading a length off it took the
// whole page down with "Cannot read properties of null".
describe("AgentPerformancePage reflection with no changes", () => {
  const reflectionWithoutChanges: AgentReflection = {
    ...reflectionWithDecision,
    id: "r2",
    decision: {
      ...reflectionWithDecision.decision!,
      changes: null,
    },
  };

  beforeEach(() => {
    localStorage.clear();
    getAgent.mockReset().mockResolvedValue(agent);
    getAgentPerformance.mockReset().mockResolvedValue(perf);
    listAgentEvolutionEvents.mockReset().mockResolvedValue({ events: [] });
    listAgentReflections.mockReset().mockResolvedValue({ reflections: [reflectionWithoutChanges] });
    listAgentMemories.mockReset().mockResolvedValue({ memories: [] });
  });

  it("renders the history and the detail dialog instead of crashing", async () => {
    renderPerformancePage();

    fireEvent.click(await screen.findByText("Tightened the QA checklist skill after two revisions."));

    const dialog = (await screen.findByRole("dialog")) as HTMLElement;
    expect(within(dialog).getByText("Performance score")).toBeInTheDocument();
  });
});

