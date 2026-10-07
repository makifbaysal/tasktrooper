import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { SessionStep } from "@/api";
import { ActivityFeed } from "@/components/activity/ActivityFeed";
import { AgentRunHeader } from "@/components/activity/AgentRunHeader";
import { I18nProvider } from "@/hooks/useI18n";
import { buildActivityFeed } from "@/lib/activityFeed";

let n = 0;
function step(step_type: string, payload: Record<string, unknown> = {}): SessionStep {
  n += 1;
  return { id: `s${n}`, run_id: "r", step_type, payload, created_at: `2026-01-01T10:00:${String(n).padStart(2, "0")}Z` };
}
const tool = (name: string, id: string, args: unknown, extra: Record<string, unknown> = {}) => [
  step("tool_call_start", { tool: name, call_id: id, arguments: JSON.stringify(args), ...extra }),
  step("tool_call_result", { tool: name, call_id: id, content: `result of ${id}`, is_error: false, ...extra }),
];

function renderFeed(steps: SessionStep[], live = false) {
  const feed = buildActivityFeed([{ runId: "r", live, steps }]);
  return render(
    <I18nProvider>
      <ActivityFeed feed={feed} live={live} rawSteps={steps} />
    </I18nProvider>,
  );
}

describe("ActivityFeed", () => {
  it("renders the user turn, grouped tools, say text and expands tool detail", () => {
    renderFeed([
      step("user_message", { content: "Analyse login" }),
      ...tool("read_file", "a", { path: "/x/run.go" }),
      ...tool("read_file", "b", { path: "/x/auth.go" }),
      step("assistant_message", { content: "Found the flow" }),
    ]);
    expect(screen.getByText("Analyse login")).toBeInTheDocument();
    expect(screen.getByText("Found the flow")).toBeInTheDocument();

    const group = screen.getByRole("button", { name: "2 files read" });
    expect(group).toHaveAttribute("aria-expanded", "false");
    fireEvent.click(group);
    expect(group).toHaveAttribute("aria-expanded", "true");

    fireEvent.click(screen.getByRole("button", { name: "run.go read" }));
    expect(screen.getByText("result of a")).toBeInTheDocument();
  });

  it("keeps a completed sub-agent collapsed and opens it from its chip", () => {
    renderFeed([
      step("user_message", { content: "go" }),
      step("tool_call_start", { tool: "subagent", call_id: "sa", arguments: JSON.stringify({ description: "Find auth", subagent_type: "Explore" }) }),
      ...tool("grep_code", "g", { pattern: "Token" }, { parent_call_id: "sa" }),
      step("tool_call_result", { tool: "subagent", call_id: "sa", content: "done", is_error: false }),
    ]);
    const header = screen.getByRole("button", { name: "Find auth" });
    expect(header).toHaveAttribute("aria-expanded", "false");
    expect(screen.queryByRole("button", { name: '"Token" searched' })).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: "Jump to Find auth" }));
    expect(header).toHaveAttribute("aria-expanded", "true");
    expect(screen.getByRole("button", { name: 'Searched "Token"' })).toBeInTheDocument();
  });

  it("opens a running sub-agent by default and shows the live tail", () => {
    renderFeed(
      [
        step("tool_call_start", { tool: "subagent", call_id: "sa", arguments: JSON.stringify({ description: "Working" }) }),
        step("tool_call_start", { tool: "read_file", call_id: "r", arguments: JSON.stringify({ path: "a.go" }), parent_call_id: "sa" }),
      ],
      true,
    );
    expect(screen.getByRole("button", { name: "Working" })).toHaveAttribute("aria-expanded", "true");
    expect(screen.getAllByText("Reading a.go…").length).toBeGreaterThan(0);
  });

  it("shows raw steps behind a toggle", () => {
    renderFeed([step("user_message", { content: "hi" })]);
    fireEvent.click(screen.getByRole("button", { name: "Raw steps (1)" }));
    expect(screen.getByRole("button", { name: "user_message" })).toBeInTheDocument();
  });
});

describe("AgentRunHeader", () => {
  it("shows the live line, lane chip and compact facts", () => {
    render(
      <I18nProvider>
        <AgentRunHeader
          agentName="system-architect"
          runStatus="running"
          live
          startedAt={new Date(Date.now() - 134_000).toISOString()}
          current={{ kind: "thinking", laneTitle: "x", laneAgent: "Explore", parallel: 2, since: "" }}
          stats={{ toolCalls: 23, failures: 1, subagents: 2, costUsd: 0.12, model: "opus" }}
          tokens={45_000}
        />
      </I18nProvider>,
    );
    expect(screen.getByText("system-architect")).toBeInTheDocument();
    expect(screen.getByText("Running")).toBeInTheDocument();
    expect(screen.getByText("Explore ›")).toBeInTheDocument();
    expect(screen.getByText("+2 parallel")).toBeInTheDocument();
    expect(screen.getByText("23 tools · 1 failed · 2 sub-agents · 45K tokens · $0.12 · opus")).toBeInTheDocument();
    expect(screen.getByText("2m")).toBeInTheDocument();
  });

  it("shows a finished run's exact duration", () => {
    render(
      <I18nProvider>
        <AgentRunHeader
          agentName="system-architect"
          runStatus="completed"
          live={false}
          startedAt="2026-01-01T10:00:00Z"
          endedAt="2026-01-01T10:02:14Z"
          current={null}
          stats={{ toolCalls: 0, failures: 0, subagents: 0 }}
        />
      </I18nProvider>,
    );
    expect(screen.getByText("2:14")).toBeInTheDocument();
  });
});

describe("ActivityFeed on a long run", () => {
  it("renders only the newest rows until earlier ones are asked for", () => {
    const notes = Array.from({ length: 130 }, (_, i) => `note ${i}`);
    renderFeed(notes.map((content) => step("assistant_message", { content })));
    const first = notes[0];
    const last = notes[notes.length - 1];
    expect(screen.queryByText(first)).toBeNull();
    expect(screen.getByText(last)).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: /^Show \d+ earlier$/ }));
    expect(screen.getByText(first)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /^Show \d+ earlier$/ })).toBeNull();
  });
});
