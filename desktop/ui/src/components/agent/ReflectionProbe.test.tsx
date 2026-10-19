import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { AgentReflection } from "@/api";
import { ReflectionDetailDialog } from "@/components/agent/ReflectionDetailDialog";
import { GoldenTaskSection } from "@/components/agent/GoldenTaskSection";
import { I18nProvider } from "@/hooks/useI18n";

const { listGoldenTasks, createGoldenTask, updateGoldenTask, deleteGoldenTask } = vi.hoisted(() => ({
  listGoldenTasks: vi.fn(),
  createGoldenTask: vi.fn(),
  updateGoldenTask: vi.fn(),
  deleteGoldenTask: vi.fn(),
}));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return {
    ...actual,
    api: { ...actual.api, listGoldenTasks, createGoldenTask, updateGoldenTask, deleteGoldenTask },
  };
});

const probed: AgentReflection = {
  id: "r2",
  agent_id: "agent-1",
  trigger: "revision",
  status: "completed",
  window_start: "2026-10-10T00:00:00Z",
  window_end: "2026-10-17T00:00:00Z",
  summary: "",
  created_at: "2026-10-17T12:00:00Z",
  decision: {
    self_assessment: "Rollbacks keep being missed.",
    catalog_before: { skills: 3, rules: 2, memories: 0, skill_budget: 10, rule_budget: 10 },
    catalog_after: { skills: 4, rules: 2, memories: 0, skill_budget: 10, rule_budget: 10 },
    changes: [
      { kind: "skill", action: "create", name: "schema-changes", dimension: "procedure", verdict: "helped", outcome: "applied" },
      { kind: "rule", action: "create", name: "always-graphql", dimension: "domain", verdict: "hurt", outcome: "not_adopted" },
    ],
    gate: {
      keep: true,
      rolled_back: 0,
      probed: true,
      adopted: "procedure",
      not_adopted: 1,
      cases: [
        { kind: "task", title: "ORD-1 Paginate orders", measured: true },
        { kind: "chat", title: "Can you add an index", measured: false },
      ],
      variants: [
        { label: "procedure", changes: 1, wins: 2, losses: 0, ties: 0, golden_losses: 0, accepted: true },
        { label: "domain", changes: 1, wins: 0, losses: 2, ties: 0, golden_losses: 0, accepted: false },
        { label: "all", changes: 2, wins: 0, losses: 0, ties: 2, golden_losses: 0, accepted: false },
      ],
    },
    lesson: {
      cue: "a reviewer keeps asking for the same missing step",
      lesson: "Put that step in the skill the run loads for that work.",
      evidence: "procedure won 2 of 2",
      confidence: "medium",
    },
  },
};

describe("ReflectionDetailDialog probe results", () => {
  beforeEach(() => localStorage.clear());

  it("shows what was adopted, each change's verdict and the lesson", () => {
    render(
      <I18nProvider>
        <ReflectionDetailDialog reflection={probed} events={[]} kpis={[]} open onOpenChange={() => {}} />
      </I18nProvider>,
    );

    expect(screen.getByText("Rehearsal")).toBeInTheDocument();
    expect(screen.getByText("Adopted: Procedure changes")).toBeInTheDocument();
    expect(screen.getByText("helped")).toBeInTheDocument();
    expect(screen.getByText("hurt")).toBeInTheDocument();
    expect(screen.getByText("not adopted")).toBeInTheDocument();
    expect(screen.getByText("Rehearsal cases (2)")).toBeInTheDocument();
    expect(screen.getByText("Lesson learned")).toBeInTheDocument();
    expect(screen.getByText("Put that step in the skill the run loads for that work.")).toBeInTheDocument();
    expect(screen.queryByText("Golden gate")).not.toBeInTheDocument();
  });
});

describe("GoldenTaskSection", () => {
  beforeEach(() => {
    localStorage.clear();
    listGoldenTasks.mockReset().mockResolvedValue({ golden_tasks: [] });
    createGoldenTask.mockReset().mockResolvedValue({});
  });

  it("creates a golden task with one expected phrase per line", async () => {
    render(
      <I18nProvider>
        <GoldenTaskSection agentId="agent-1" />
      </I18nProvider>,
    );

    expect(await screen.findByText(/No golden tasks yet/)).toBeInTheDocument();
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "ship-schema" } });
    fireEvent.change(screen.getByLabelText("Prompt"), { target: { value: "How do you ship a schema change?" } });
    fireEvent.change(screen.getByLabelText("Expected phrases"), { target: { value: "rollback\n\n down migration " } });
    fireEvent.click(screen.getByRole("button", { name: "Add golden task" }));

    await waitFor(() =>
      expect(createGoldenTask).toHaveBeenCalledWith("agent-1", {
        name: "ship-schema",
        prompt: "How do you ship a schema change?",
        expected: ["rollback", "down migration"],
        enabled: true,
      }),
    );
  });
});
