import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { TaskReviews } from "@/api";
import { CodeReviewApprovals } from "@/components/board/CodeReviewApprovals";
import { I18nProvider } from "@/hooks/useI18n";

const decided = "2026-10-08T09:00:00Z";

const reviews: TaskReviews = {
  column: "code_review",
  rounds: [
    {
      round: 1,
      entered_at: "2026-10-07T09:00:00Z",
      left_at: "2026-10-07T10:00:00Z",
      outcome: "rejected",
      reviewers: [
        { agent_id: "a-1", agent_name: "system-architect", verdict: "approve", decided_at: decided, required: false },
        { agent_id: "a-2", agent_name: "security-agent", verdict: "reject", decided_at: decided, required: false },
      ],
    },
    {
      round: 2,
      entered_at: "2026-10-08T08:00:00Z",
      outcome: "open",
      reviewers: [
        { agent_id: "a-1", agent_name: "system-architect", verdict: "approve", decided_at: decided, required: true },
        { agent_id: "a-2", agent_name: "security-agent", verdict: "pending", required: true },
      ],
    },
  ],
};

function renderApprovals(value: TaskReviews | null, inCodeReview = true) {
  return render(
    <I18nProvider>
      <CodeReviewApprovals reviews={value} inCodeReview={inCodeReview} />
    </I18nProvider>,
  );
}

describe("CodeReviewApprovals", () => {
  it("shows the open round's verdict per reviewer, pending ones included", () => {
    renderApprovals(reviews);

    expect(screen.getByText("Round 2")).toBeInTheDocument();
    expect(screen.getByText("In review")).toBeInTheDocument();
    const architect = screen.getByText("system-architect").closest("li") as HTMLElement;
    expect(within(architect).getByText("Approved")).toBeInTheDocument();
    const security = screen.getByText("security-agent").closest("li") as HTMLElement;
    expect(within(security).getByText("Pending")).toBeInTheDocument();
  });

  it("keeps earlier rounds folded until asked", () => {
    renderApprovals(reviews);

    expect(screen.queryByText("Round 1")).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Earlier rounds (1)" }));

    expect(screen.getByText("Round 1")).toBeInTheDocument();
    expect(screen.getAllByText("Changes requested").length).toBeGreaterThanOrEqual(2);
  });

  it("renders nothing for a task that never reached code review", () => {
    const { container } = renderApprovals({ column: "code_review", rounds: [] }, false);
    expect(container).toBeEmptyDOMElement();
  });

  it("says the reviewers have not started when the card just arrived", () => {
    renderApprovals({ column: "code_review", rounds: [] });
    expect(screen.getByText("Waiting for the reviewers to start.")).toBeInTheDocument();
  });
});
