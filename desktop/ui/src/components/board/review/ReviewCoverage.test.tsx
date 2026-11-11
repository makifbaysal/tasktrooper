import "@testing-library/jest-dom/vitest";
import { render, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { RoundCoverage } from "@/api";
import { RoundCoverageView, coverageRatio } from "@/components/board/review/ReviewCoverage";
import { I18nProvider } from "@/hooks/useI18n";

const coverage: RoundCoverage = {
  changed_files: 15, changed_lines: 600,
  reviewers: [
    {
      agent_id: "a1", agent_name: "system-architect", basis: ["trace", "diff"], aspects: ["correctness", "tests"],
      files_total: 15, files_reviewed: 12, files_opened: 8, lines_total: 600, lines_reviewed: 480, diff_known: true,
      unread_files: ["svc/big.go", "svc/other.go", "svc/third.go"], unread_count: 3,
      not_reviewed: [
        { scope: "run_terminal", reason: "toolchain missing", kind: "check", source: "auto", count: 1 },
        { scope: "web/", reason: "no browser to run the UI", kind: "declared", source: "declared" },
      ],
    },
    {
      agent_id: "a2", agent_name: "security-agent", basis: ["trace"], aspects: ["security"],
      files_total: 4, files_reviewed: 4, files_opened: 4, lines_total: 100, lines_reviewed: 100, diff_known: false,
      unread_files: [], unread_count: 0,
      not_reviewed: [{ scope: "security", reason: "outside_role", kind: "role", source: "auto", count: 2, files: ["ui/a.css"] }],
    },
  ],
  nobody_reviewed: [{ path: "svc/big.go", lines: 2000, file_url: "https://github.com/o/r/pull/1/files#diff-x" }],
};

describe("RoundCoverageView", () => {
  it("shows per reviewer: aspects, files and lines covered", () => {
    render(
      <I18nProvider>
        <RoundCoverageView coverage={coverage} />
      </I18nProvider>,
    );
    const cards = screen.getAllByTestId("reviewer-coverage");
    expect(cards).toHaveLength(2);
    expect(within(cards[0]).getByText("correctness")).toBeInTheDocument();
    expect(within(cards[0]).getByText(/12 of 15 files · 480 of 600 lines \(80%\)/)).toBeInTheDocument();
    expect(within(cards[0]).getByRole("progressbar")).toHaveAttribute("aria-valuenow", "80");
  });

  it("highlights what a reviewer did not review, from declarations and from the records", () => {
    render(
      <I18nProvider>
        <RoundCoverageView coverage={coverage} />
      </I18nProvider>,
    );
    const [first, second] = screen.getAllByTestId("not-reviewed");
    expect(within(first).getByText("run_terminal could not run: toolchain missing")).toBeInTheDocument();
    expect(within(first).getByText("web/: no browser to run the UI")).toBeInTheDocument();
    expect(within(first).getByText("Files it did not open (3)")).toBeInTheDocument();
    expect(within(second).getByText("Outside this reviewer's role (security): 2 files")).toBeInTheDocument();
  });

  it("says when the diff a reviewer was shown is unknown", () => {
    render(
      <I18nProvider>
        <RoundCoverageView coverage={coverage} />
      </I18nProvider>,
    );
    expect(screen.getAllByText(/was not recorded for this round/)).toHaveLength(1);
  });

  it("lists the files nobody reviewed prominently, linked to the diff", () => {
    render(
      <I18nProvider>
        <RoundCoverageView coverage={coverage} />
      </I18nProvider>,
    );
    const box = screen.getByTestId("nobody-reviewed");
    expect(within(box).getByText("Nobody reviewed these")).toBeInTheDocument();
    expect(within(box).getByRole("link", { name: "svc/big.go" })).toHaveAttribute("href", "https://github.com/o/r/pull/1/files#diff-x");
  });

  it("has no nobody-reviewed box when every file was covered", () => {
    render(
      <I18nProvider>
        <RoundCoverageView coverage={{ ...coverage, nobody_reviewed: [] }} />
      </I18nProvider>,
    );
    expect(screen.queryByTestId("nobody-reviewed")).not.toBeInTheDocument();
  });

  it("computes the ratio without dividing by zero", () => {
    expect(coverageRatio(1, 3)).toBe(33);
    expect(coverageRatio(0, 0)).toBe(0);
  });
});
