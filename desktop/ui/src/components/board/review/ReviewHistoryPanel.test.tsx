import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import type { ReviewHistory } from "@/api";
import { ReviewHistoryPanel, ReviewHistoryToggle, summaryLine } from "@/components/board/review/ReviewHistoryPanel";
import { I18nProvider } from "@/hooks/useI18n";

const history: ReviewHistory = {
  task_id: "t1",
  summary: {
    rounds: 3, findings_raised: 3, fixed: 1, open: 2, withdrawn: 0, reopened: 0, bounces: 2,
    loop_guard_stops: 0, qa_failures: 0, open_flags: 0,
  },
  rounds: [
    {
      seq: 1, number: 1, gate: "code_review", entered_at: "2026-10-07T09:00:00Z", left_at: "2026-10-07T10:00:00Z",
      outcome: "changes_requested", from_sha: "", head_sha: "aaaaaaaa1111",
      reviewers: [{ agent_id: "a1", agent_name: "system-architect", verdict: "reject", run_id: "run-r1" }],
      raised: ["f1", "f2"], fixed: [], still_open: [], reopened: [],
      moves: [
        { at: "2026-10-07T10:00:00Z", from: "code_review", to: "need_revision", actor: "agent", agent_name: "system-architect", bounce: true },
        { at: "2026-10-07T10:01:00Z", from: "need_revision", to: "in_progress", actor: "system" },
      ],
      fix_runs: [{ run_id: "run-d1", agent_id: "d1", agent_name: "backend-developer", status: "completed", started_at: "2026-10-07T10:02:00Z", base_sha: "aaaaaaaa1111", head_sha: "bbbbbbbb2222", files_changed: 3, has_trace: true }],
    },
    {
      seq: 2, number: 2, gate: "code_review", entered_at: "2026-10-08T09:00:00Z", outcome: "open",
      from_sha: "aaaaaaaa1111", head_sha: "bbbbbbbb2222",
      reviewers: [{ agent_id: "a1", agent_name: "system-architect", verdict: "approve" }],
      raised: ["f3"], fixed: ["f1"], still_open: ["f2"], reopened: [], moves: [], fix_runs: [],
    },
  ],
  findings: [
    { id: "f1", gate: "code_review", file: "api/a.go", line: 4, file_url: "https://github.com/o/r/pull/1/files#diff-abcR4", severity: "high", category: "correctness", message: "Missing nil check", status: "fixed", raised_round: 1, fixed_round: 2, trail: [{ round: 1, state: "raised", at: "x" }, { round: 2, state: "fixed", at: "y" }] },
    { id: "f2", gate: "code_review", file: "api/b.go", severity: "medium", message: "Error swallowed", status: "open", raised_round: 1, trail: [{ round: 1, state: "raised", at: "x" }, { round: 2, state: "still_open", at: "y" }] },
    { id: "f3", gate: "code_review", message: "Unbounded loop", status: "open", raised_round: 2, confidence: "low", trail: [{ round: 2, state: "raised", at: "y" }] },
  ],
  flags: [],
};

function renderPanel(onOpenRun?: (id: string) => void) {
  return render(
    <I18nProvider>
      <ReviewHistoryPanel history={history} onOpenRun={onOpenRun} />
    </I18nProvider>,
  );
}

describe("ReviewHistoryPanel", () => {
  it("opens with the one-line story of the whole loop", () => {
    renderPanel();
    expect(screen.getByTestId("review-history-summary")).toHaveTextContent(
      "3 review rounds · 3 findings raised · 1 fixed · 2 open · 2 bounces",
    );
  });

  it("lists rounds newest first with outcome, verdicts and the commit range", () => {
    renderPanel();
    const rounds = screen.getAllByTestId("review-round");
    expect(rounds).toHaveLength(2);
    expect(within(rounds[0]).getByText("Code review round 2")).toBeInTheDocument();
    expect(within(rounds[0]).getByText(/aaaaaaa → bbbbbbb/)).toBeInTheDocument();
    expect(within(rounds[1]).getAllByText("Changes requested").length).toBe(2);
  });

  it("shows what became of each finding across rounds", () => {
    renderPanel();
    const [round2, round1] = screen.getAllByTestId("review-round");
    const fixed = within(round2).getByText("Missing nil check").closest("li") as HTMLElement;
    expect(within(fixed).getByText("Fixed in round 2")).toBeInTheDocument();
    const carried = within(round2).getByText("Error swallowed").closest("li") as HTMLElement;
    expect(within(carried).getAllByText("Still open").length).toBeGreaterThan(0);
    expect(within(round1).getAllByTestId("review-finding")).toHaveLength(2);
  });

  it("filters to open findings only", () => {
    renderPanel();
    fireEvent.click(screen.getByLabelText("Open findings only"));
    expect(screen.queryByText("Missing nil check")).not.toBeInTheDocument();
    expect(screen.getAllByText("Error swallowed").length).toBeGreaterThan(0);
    expect(screen.getByText("Unbounded loop")).toBeInTheDocument();
  });

  it("marks the bounce between rounds and the developer run that answered it", () => {
    renderPanel();
    const [, round1] = screen.getAllByTestId("review-round");
    const moves = within(round1).getByTestId("round-moves");
    expect(within(moves).getByText("code_review → need_revision")).toBeInTheDocument();
    expect(within(moves).getByText("Sent back")).toBeInTheDocument();
    expect(within(round1).getByText(/backend-developer/)).toBeInTheDocument();
  });

  it("opens a run's trace from the reviewer and from the developer's answer", () => {
    const onOpenRun = vi.fn();
    renderPanel(onOpenRun);
    const [, round1] = screen.getAllByTestId("review-round");
    fireEvent.click(within(round1).getByRole("button", { name: "Open trace" }));
    expect(onOpenRun).toHaveBeenCalledWith("run-d1");
    fireEvent.click(within(round1).getByRole("button", { name: "Open run" }));
    expect(onOpenRun).toHaveBeenCalledWith("run-r1");
  });

  it("expands a finding to its file link and trail", () => {
    renderPanel();
    const [round2] = screen.getAllByTestId("review-round");
    const row = within(round2).getByText("Missing nil check").closest("li") as HTMLElement;
    fireEvent.click(within(row).getByRole("button"));
    expect(within(row).getByRole("link", { name: "api/a.go:4" })).toHaveAttribute("href", "https://github.com/o/r/pull/1/files#diff-abcR4");
    expect(within(row).getByText("Round 2: Fixed")).toBeInTheDocument();
  });

  it("says so when nothing has been reviewed", () => {
    render(
      <I18nProvider>
        <ReviewHistoryPanel history={{ ...history, rounds: [], findings: [] }} />
      </I18nProvider>,
    );
    expect(screen.getByText("No review has happened on this task yet.")).toBeInTheDocument();
  });

  it("singular counts read as singular", () => {
    const line = summaryLine({ ...history.summary, rounds: 1, findings_raised: 1, bounces: 1 }, (k, p) =>
      renderT(k, p),
    );
    expect(line).toContain("1 review round ·");
    expect(line).toContain("1 bounce");
  });
});

import { tStatic } from "@/hooks/useI18n";
function renderT(key: string, params?: Record<string, string | number>) {
  return tStatic(key, params);
}

describe("ReviewHistoryToggle", () => {
  it("shows the summary and keeps the timeline folded until asked", () => {
    render(
      <I18nProvider>
        <ReviewHistoryToggle history={history} />
      </I18nProvider>,
    );
    expect(screen.queryByTestId("review-history")).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Show history" }));
    expect(screen.getByTestId("review-history")).toBeInTheDocument();
  });

  it("renders nothing for a task never reviewed", () => {
    const { container } = render(
      <I18nProvider>
        <ReviewHistoryToggle history={{ ...history, rounds: [] }} />
      </I18nProvider>,
    );
    expect(container).toBeEmptyDOMElement();
  });
});
