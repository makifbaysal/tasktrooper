import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import type { StudyAnswer, StudyKind } from "@/api";
import { I18nProvider } from "@/hooks/useI18n";
import { StudyAnswers } from "./StudyAnswers";

function answer(extra: Partial<StudyAnswer["answers"]>): StudyAnswer {
  return {
    id: "x1",
    persona_id: "p1",
    persona: { name: "Şule İpek", segment: "Freelancers", role: "UX designer" },
    status: "ok",
    answers: { intent_score: 7, verdict: "yes", would_pay_now: false, reasoning: "Because it is clear.", ...extra },
  };
}

function open(kind: StudyKind, a: StudyAnswer, variants?: string[]) {
  render(
    <I18nProvider>
      <StudyAnswers answers={[a]} currency="USD" kind={kind} variants={variants} onTalk={vi.fn()} />
    </I18nProvider>,
  );
  fireEvent.click(screen.getByRole("button", { name: /Şule İpek/ }));
}

describe("StudyAnswers by study kind", () => {
  it("shows a visitor's first impression, clarity, trust signals and the one change", () => {
    open(
      "landing_page_test",
      answer({
        max_price: 9,
        landing: { first_impression: "Looks like invoicing.", clarity_score: 8, trust_signals: ["4,000 freelancers"], change: "Show the price" },
      }),
    );
    expect(screen.getByText("Looks like invoicing.")).toBeInTheDocument();
    expect(screen.getByText("8/10")).toBeInTheDocument();
    expect(screen.getByText("4,000 freelancers")).toBeInTheDocument();
    expect(screen.getByText("Show the price")).toBeInTheDocument();
    expect(screen.queryByText("$9")).not.toBeInTheDocument();
  });

  it("shows a reader's rating of every line with the line's own text, the best line and the stronger one", () => {
    open(
      "message_testing",
      answer({
        ratings: [
          { variant: 1, clarity: 3, relevance: 3, credibility: 3, appeal: 3 },
          { variant: 2, clarity: 9, relevance: 8, credibility: 7, appeal: 9, comment: "plain" },
        ],
        best_variant: 2,
        stronger: "Get paid faster",
      }),
      ["Boards for agents", "Invoices that chase themselves"],
    );
    expect(screen.getByText("Boards for agents")).toBeInTheDocument();
    expect(screen.getByText(/clarity 9, relevance 8, credibility 7, appeal 9/)).toBeInTheDocument();
    expect(screen.getAllByText("Invoices that chase themselves").length).toBe(2);
    expect(screen.getByText("Get paid faster")).toBeInTheDocument();
  });

  it("shows both scorecards in the order the person saw them, then the preference and the reason", () => {
    const evil = "<script>alert(1)</script>";
    open(
      "ab_test",
      answer({
        shown_first: "b",
        preferred: "a",
        reasoning: "Clearer offer.",
        page_a: { first_impression: "Plain.", clarity_score: 8, act_score: 7, verdict: "yes", objections: ["no price"], change: "Show the price" },
        page_b: { first_impression: evil, clarity_score: 3, act_score: 2, verdict: "no", objections: [evil], missing: ["proof"], trust_signals: ["logos"], change: evil },
      }),
    );
    const headings = screen.getAllByText(/^Version [AB]/).map((el) => el.textContent);
    expect(headings.slice(0, 2)).toEqual(["Version B (seen first)", "Version A (seen second)"]);
    expect(screen.getByText("Plain.")).toBeInTheDocument();
    expect(screen.getByText("7/10 · Yes")).toBeInTheDocument();
    expect(screen.getByText("2/10 · No")).toBeInTheDocument();
    expect(screen.getByText("no price")).toBeInTheDocument();
    expect(screen.getByText("proof")).toBeInTheDocument();
    expect(screen.getByText("logos")).toBeInTheDocument();
    expect(screen.getAllByText(evil).length).toBe(3);
    expect(document.querySelector("script")).toBeNull();
    expect(screen.getByText("Preferred")).toBeInTheDocument();
    expect(screen.getByText("Clearer offer.")).toBeInTheDocument();
    expect(screen.queryByText("Why")).toBeInTheDocument();
  });

  it("keeps the interview rows as they were, price included", () => {
    open("user_interviews", answer({ max_price: 9, price_reactions: [{ price: 9, would_buy: true }], alternatives: ["Spreadsheets"] }));
    expect(screen.getByText("$9")).toBeInTheDocument();
    expect(screen.getByText("Uses instead")).toBeInTheDocument();
    expect(screen.getByText("$9: would buy")).toBeInTheDocument();
  });
});
