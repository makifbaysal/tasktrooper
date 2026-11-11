import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { ReviewFlag, ReviewHistory } from "@/api";
import { ReviewFlagsCard } from "@/components/board/review/ReviewFlagsCard";
import { ReviewFlagsPanel } from "@/components/board/review/ReviewSection";
import { I18nProvider } from "@/hooks/useI18n";

const markApi = vi.fn();
vi.mock("@/api", async (orig) => {
  const mod = await orig<typeof import("@/api")>();
  return { ...mod, api: { ...mod.api, markReviewFlagReviewed: (...args: unknown[]) => markApi(...args) } };
});
vi.mock("sonner", () => ({ toast: { error: vi.fn(), success: vi.fn() } }));

const agentFlag: ReviewFlag = {
  id: "agent-1", source: "agent", code: "agent_uncertain", reason: "touches payment logic I could not run",
  question: "does a retry double charge?", file: "pay/charge.go", line: 12, file_url: "https://github.com/o/r/pull/1/files#diff-xR12",
  agent_name: "security-agent", raised_at: "2026-10-08T09:00:00Z",
};
const autoFlag: ReviewFlag = {
  id: "auto-sensitive_path-migrations-abc", source: "automatic", code: "sensitive_path", reason: "migrations", count: 2,
  files: ["db/migrations/1.sql", "db/migrations/2.sql"], file: "db/migrations/1.sql", raised_at: "2026-10-08T09:00:00Z",
};
const reviewedFlag: ReviewFlag = {
  id: "auto-large_diff-40", source: "automatic", code: "large_diff", count: 55, raised_at: "2026-10-08T09:00:00Z",
  reviewed_by: "ayse", reviewed_at: "2026-10-08T10:00:00Z",
};

function renderCard(flags: ReviewFlag[], onMark = vi.fn()) {
  render(
    <I18nProvider>
      <ReviewFlagsCard flags={flags} onMarkReviewed={onMark} />
    </I18nProvider>,
  );
  return onMark;
}

describe("ReviewFlagsCard", () => {
  it("lists each open flag with its reason, place, question and who flagged it", () => {
    renderCard([agentFlag, autoFlag, reviewedFlag]);
    expect(screen.getByTestId("review-flags-count")).toHaveTextContent("2 open");
    const rows = screen.getAllByTestId("review-flag");
    expect(rows).toHaveLength(2);
    expect(within(rows[0]).getByText("touches payment logic I could not run")).toBeInTheDocument();
    expect(within(rows[0]).getByText("Question: does a retry double charge?")).toBeInTheDocument();
    expect(within(rows[0]).getByText("pay/charge.go:12")).toBeInTheDocument();
    expect(within(rows[0]).getByText("Flagged by security-agent")).toBeInTheDocument();
    expect(within(rows[0]).getByRole("link", { name: /Open file\/diff/ })).toHaveAttribute("href", agentFlag.file_url);
    expect(within(rows[1]).getByText("Sensitive area changed: database migrations (2 files)")).toBeInTheDocument();
    expect(within(rows[1]).getByText("Automatic")).toBeInTheDocument();
  });

  it("keeps reviewed flags out of the way, with who and when", () => {
    renderCard([agentFlag, reviewedFlag]);
    expect(screen.queryByText(/Very large change/)).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Reviewed (1)" }));
    expect(screen.getByText(/Very large change: 55 files/)).toBeInTheDocument();
    expect(screen.getByText(/Reviewed by ayse/)).toBeInTheDocument();
  });

  it("marks a flag reviewed from its own button", async () => {
    const onMark = renderCard([agentFlag, autoFlag]);
    fireEvent.click(within(screen.getAllByTestId("review-flag")[1]).getByRole("button", { name: "Mark reviewed" }));
    await waitFor(() => expect(onMark).toHaveBeenCalledWith(autoFlag));
  });

  it("renders nothing without flags", () => {
    const { container } = render(
      <I18nProvider>
        <ReviewFlagsCard flags={[]} onMarkReviewed={vi.fn()} />
      </I18nProvider>,
    );
    expect(container).toBeEmptyDOMElement();
  });

  it("words the coverage gap and the QA evidence flags", () => {
    renderCard([
      { id: "auto-coverage_gap-1", source: "automatic", code: "coverage_gap", count: 3, raised_at: "x" },
      { id: "auto-qa_no_evidence-1", source: "automatic", code: "qa_no_evidence", reason: "user can log in", raised_at: "x" },
    ]);
    expect(screen.getByText("3 changed files no reviewer opened")).toBeInTheDocument();
    expect(screen.getByText("QA approved a criterion with no passing test evidence: user can log in")).toBeInTheDocument();
  });
});

describe("ReviewFlagsPanel", () => {
  beforeEach(() => markApi.mockReset());
  const history = (flags: ReviewFlag[]): ReviewHistory => ({
    task_id: "t1", summary: { rounds: 0, findings_raised: 0, fixed: 0, open: 0, withdrawn: 0, reopened: 0, bounces: 0, loop_guard_stops: 0, qa_failures: 0, open_flags: flags.length },
    rounds: [], findings: [], flags,
  });

  it("stores who marked the flag and re-reads the history", async () => {
    markApi.mockResolvedValue({ ...agentFlag, reviewed_by: "local", reviewed_at: "now" });
    const reload = vi.fn().mockResolvedValue(undefined);
    render(
      <I18nProvider>
        <ReviewFlagsPanel repositoryId="r1" taskId="t1" history={history([agentFlag])} reload={reload} />
      </I18nProvider>,
    );
    fireEvent.click(screen.getByRole("button", { name: "Mark reviewed" }));
    await waitFor(() => expect(markApi).toHaveBeenCalledWith("r1", "t1", "agent-1"));
    await waitFor(() => expect(reload).toHaveBeenCalled());
  });

  it("shows nothing before the history has loaded", () => {
    const { container } = render(
      <I18nProvider>
        <ReviewFlagsPanel repositoryId="r1" taskId="t1" history={null} reload={vi.fn()} />
      </I18nProvider>,
    );
    expect(container).toBeEmptyDOMElement();
  });
});
