import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { I18nProvider } from "@/hooks/useI18n";
import type { StudyComparison } from "@/api";

const { compareAudienceStudy, rerunAudienceStudy } = vi.hoisted(() => ({ compareAudienceStudy: vi.fn(), rerunAudienceStudy: vi.fn() }));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return { ...actual, api: { ...actual.api, compareAudienceStudy, rerunAudienceStudy } };
});
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

import { RerunStudyDialog } from "./RerunStudyDialog";
import { StudyComparisonView } from "./StudyComparisonView";

const comparison: StudyComparison = {
  before: { id: "s1", title: "Would they pay?", respondents: 4 },
  after: { id: "s2", title: "Would they pay? (re-run)", respondents: 4 },
  kind: "user_interviews",
  changes: "Added a free tier.",
  paired: 4,
  improved: 2,
  worse: 0,
  same: 2,
  verdict: { key: "verdict_score", before: 57.5, after: 72.5, delta: 15 },
  verdict_labels: ["promising", "strong"],
  metrics: [
    { key: "intent_mean", before: 5.8, after: 7.3, delta: 1.5 },
    { key: "no_pct", before: 25, after: 0, delta: -25 },
  ],
  segments: [{ name: "Freelancers", before_intent: 5, after_intent: 7, before_yes_pct: 33, after_yes_pct: 67 }],
  objections: [
    { label: "Too expensive", before_count: 3, after_count: 1, before_pct: 75, after_pct: 25, status: "down" },
    { label: "No mobile app", before_count: 2, after_count: 0, before_pct: 50, after_pct: 0, status: "resolved" },
    { label: "Missing integrations", before_count: 0, after_count: 2, before_pct: 0, after_pct: 50, status: "new" },
  ],
  needs: [],
  alternatives: [],
  strengths: [],
  people: [
    { persona_id: "p1", name: "Ayse Demir", segment: "Freelancers", before_intent: 2, after_intent: 6, before_verdict: "no", after_verdict: "maybe", after_quote: "A free tier changes it for me." },
  ],
};

function wrap(node: React.ReactNode) {
  return render(<I18nProvider>{node}</I18nProvider>);
}

beforeEach(() => {
  compareAudienceStudy.mockReset();
  rerunAudienceStudy.mockReset();
});

describe("StudyComparisonView", () => {
  it("leads with the verdict before and after, then what went away and what is new", async () => {
    compareAudienceStudy.mockResolvedValue(comparison);
    wrap(<StudyComparisonView studyId="s2" />);

    expect(await screen.findByText(/57.5 → 72.5/)).toBeInTheDocument();
    expect(screen.getByText("+15")).toBeInTheDocument();
    expect(screen.getByText("Added a free tier.")).toBeInTheDocument();
    expect(screen.getByText(/4 people answered both: 2 scored higher, 0 lower, 2 the same\./)).toBeInTheDocument();
    expect(screen.getByText("Gone")).toBeInTheDocument();
    expect(screen.getByText("New")).toBeInTheDocument();
    expect(screen.getByText("Less")).toBeInTheDocument();
    expect(screen.getByText(/A free tier changes it for me/)).toBeInTheDocument();
    expect(screen.getByText(/not a measurement of real customers/)).toBeInTheDocument();
  });

  it("shows nothing when there is nothing to compare with", async () => {
    compareAudienceStudy.mockRejectedValue(new Error("no earlier study"));
    const { container } = wrap(<StudyComparisonView studyId="s2" />);
    await waitFor(() => expect(container.querySelector(".h-40")).toBeNull());
    expect(screen.queryByText("Before and after")).not.toBeInTheDocument();
  });
});

describe("RerunStudyDialog", () => {
  it("plans the new study with the note of what changed", async () => {
    rerunAudienceStudy.mockResolvedValue({ id: "s2" });
    const onPlanned = vi.fn();
    wrap(<RerunStudyDialog studyId="s1" open onOpenChange={() => {}} onPlanned={onPlanned} />);

    fireEvent.change(screen.getByLabelText("What did you change?"), { target: { value: "  Added a free tier.  " } });
    fireEvent.click(screen.getByRole("button", { name: "Plan the new study" }));

    await waitFor(() => expect(rerunAudienceStudy).toHaveBeenCalledWith("s1", "Added a free tier."));
    await waitFor(() => expect(onPlanned).toHaveBeenCalledWith({ id: "s2" }));
  });
});
