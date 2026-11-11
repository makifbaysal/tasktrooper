import "@testing-library/jest-dom/vitest";
import { render, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { I18nProvider } from "@/hooks/useI18n";
import { abReport, landingReport, messagesReport, report } from "@/test/audienceFixtures";
import { ReportView } from "./ReportView";

function renderReport(r = report()) {
  return render(
    <I18nProvider>
      <ReportView report={r} title="Would they pay?" audience="Freelance designers" />
    </I18nProvider>,
  );
}

describe("ReportView", () => {
  it("leads with the verdict, the index and the written summary", () => {
    renderReport();
    expect(screen.getByRole("heading", { name: "Would they pay?" })).toBeInTheDocument();
    expect(screen.getByText("58")).toBeInTheDocument();
    expect(screen.getByText("Promising")).toBeInTheDocument();
    expect(screen.getByText("Interest is lukewarm and price-bound.")).toBeInTheDocument();
    expect(screen.getByText("Mean intent is middling.")).toBeInTheDocument();
  });

  it("shows the computed numbers: respondents, mean, split and what they would pay", () => {
    renderReport();
    expect(screen.getAllByText("People interviewed").length).toBeGreaterThan(0);
    expect(screen.getByText("1 could not answer")).toBeInTheDocument();
    expect(screen.getByText("5.8 / 10")).toBeInTheDocument();
    expect(screen.getByText("2 yes, 1 maybe, 1 no")).toBeInTheDocument();
    expect(screen.getByText("50%")).toBeInTheDocument();
    expect(screen.getAllByText("$12").length).toBeGreaterThan(0);
    expect(screen.getByText("$5 - $20")).toBeInTheDocument();
  });

  it("charts the intent scores, the price histogram and the buy rate at each tested price", () => {
    renderReport();
    expect(screen.getByRole("img", { name: "Purchase intent" })).toBeInTheDocument();
    expect(screen.getByRole("img", { name: "Highest price they would pay" })).toBeInTheDocument();
    expect(screen.getByText("75% would buy (4 asked)")).toBeInTheDocument();
    expect(screen.getByText("25% would buy (4 asked)")).toBeInTheDocument();
  });

  it("breaks the result down by segment with the insight written for it", () => {
    renderReport();
    const table = screen.getByRole("table");
    expect(within(table).getByText("Freelancers")).toBeInTheDocument();
    expect(within(table).getByText("Freelancers balk at the price.")).toBeInTheDocument();
    expect(within(table).getByText("Agency leads")).toBeInTheDocument();
    expect(within(table).getByText("$20")).toBeInTheDocument();
  });

  it("lists objections and needs with how many people said each, and who", () => {
    renderReport();
    expect(screen.getByText("Too expensive")).toBeInTheDocument();
    expect(screen.getByText(/3 of 4 people · 75%/)).toBeInTheDocument();
    expect(screen.getByText("Ayse Demir, Can, Deniz")).toBeInTheDocument();
    expect(screen.getByText("Invoicing")).toBeInTheDocument();
    expect(screen.getByText("Spreadsheets")).toBeInTheDocument();
  });

  it("quotes people word for word with who said it, and ends with recommendations, method and the disclaimer", () => {
    renderReport();
    expect(screen.getByText("too pricey for me")).toBeInTheDocument();
    expect(screen.getByText("Ayse Demir")).toBeInTheDocument();
    expect(screen.getByText("Add a free tier")).toBeInTheDocument();
    expect(screen.getByText("High")).toBeInTheDocument();
    expect(screen.getByText("A free tier.")).toBeInTheDocument();
    expect(screen.getByText("4 synthetic personas were each asked the same questions.")).toBeInTheDocument();
    expect(screen.getByText("Read this as a hypothesis, not as data")).toBeInTheDocument();
    expect(screen.getByText("These findings come from synthetic personas, not real customers.")).toBeInTheDocument();
  });

  it("says so when the written analysis could not be produced", () => {
    renderReport(report({ synthesis_failed: true }));
    expect(screen.getByText("The written analysis could not be produced")).toBeInTheDocument();
  });

  it("does not invent a price section when nobody gave a price", () => {
    const base = report();
    renderReport(report({ metrics: { ...base.metrics, price: { ...base.metrics.price, answered: 0, not_willing: 0, histogram: [], tests: [] } } }));
    expect(screen.getByText("No one gave a price.")).toBeInTheDocument();
    expect(screen.queryByText("Highest price they would pay")).not.toBeInTheDocument();
  });
});

describe("ReportView by study kind", () => {
  it("reads a landing page test as an act-now report with frictions, clarity and recommended edits, and no price section", () => {
    renderReport(landingReport());
    expect(screen.getByText("Act-now score")).toBeInTheDocument();
    expect(screen.getByText("Landing page test report")).toBeInTheDocument();
    expect(screen.getByText("Clarity of the offer", { selector: "h2" })).toBeInTheDocument();
    expect(screen.getByText("https://acme.test/")).toBeInTheDocument();
    expect(screen.getByText("Top frictions")).toBeInTheDocument();
    expect(screen.getByText("No pricing")).toBeInTheDocument();
    expect(screen.getByText("What worked")).toBeInTheDocument();
    expect(screen.getByText("Recommended edits")).toBeInTheDocument();
    expect(screen.queryByText("Willingness to pay")).not.toBeInTheDocument();
    expect(screen.queryByText("Purchase-intent index")).not.toBeInTheDocument();
    expect(within(screen.getByRole("table")).queryByText("Median price")).not.toBeInTheDocument();
  });

  it("ranks the lines of a message test, names the winner and lists the stronger lines people wrote", () => {
    renderReport(messagesReport());
    expect(screen.getByText("Best-line score")).toBeInTheDocument();
    expect(screen.getByText("Ranking of the lines")).toBeInTheDocument();
    const winner = screen.getByText("Invoices that chase themselves");
    expect(winner).toBeInTheDocument();
    expect(screen.getByText("Boards for agents")).toBeInTheDocument();
    expect(screen.getAllByText("Winner").length).toBeGreaterThan(0);
    expect(screen.getByText("100% picked it as the best")).toBeInTheDocument();
    expect(screen.getByText("Stronger lines people wrote")).toBeInTheDocument();
    expect(screen.getByText("Get paid faster", { selector: "blockquote" })).toBeInTheDocument();
    expect(screen.getByText("Suggested rewrites")).toBeInTheDocument();
  });

  it("compares the two versions side by side, with the confidence, the position-bias warning and the sample-size honesty note", () => {
    renderReport(abReport());
    expect(screen.getByText("Act-now score")).toBeInTheDocument();
    expect(screen.getAllByText("Preferred by most: version A").length).toBeGreaterThan(0);
    const table = screen.getAllByRole("table")[0];
    expect(within(table).getByText("Act-now score (0-10)")).toBeInTheDocument();
    expect(within(table).getByText("Clarity (0-10)")).toBeInTheDocument();
    expect(within(table).getByText("Share who would act")).toBeInTheDocument();
    expect(within(table).getByText("Preferred by")).toBeInTheDocument();
    expect(within(table).getByText("\u22123.5")).toBeInTheDocument();
    expect(within(table).getByText("Large (1.40)")).toBeInTheDocument();
    expect(within(table).getByText("Medium (0.90)")).toBeInTheDocument();
    expect(within(table).getByText("A 4 · B 0 · same 0")).toBeInTheDocument();
    expect(screen.getAllByText("Lean").length).toBeGreaterThan(0);
    expect(screen.getByText(/The page shown first won 80% of the time/)).toBeInTheDocument();
    expect(screen.getByText("How to read these differences")).toBeInTheDocument();
    expect(screen.getByText(/not statistically significant results, and no p-values are computed/)).toBeInTheDocument();
  });

  it("shows each version on its own: preference, objections, reasons and quotes labelled as synthetic", () => {
    renderReport(abReport());
    expect(screen.getByRole("heading", { name: "Version A" })).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Version B" })).toBeInTheDocument();
    expect(screen.getByText("jargon")).toBeInTheDocument();
    expect(screen.getByText("vague")).toBeInTheDocument();
    expect(screen.getByText("Clear offer")).toBeInTheDocument();
    expect(screen.getByText("I got it at once.")).toBeInTheDocument();
    expect(screen.getByText("Too much jargon.")).toBeInTheDocument();
    expect(screen.getAllByText(/synthetic persona/).length).toBeGreaterThanOrEqual(2);
    expect(screen.getByText("Preference by segment")).toBeInTheDocument();
  });

  it("keeps the interview wording for a report without a kind", () => {
    renderReport(report({ kind: undefined }));
    expect(screen.getByText("Purchase-intent index")).toBeInTheDocument();
    expect(screen.getByText("Willingness to pay")).toBeInTheDocument();
  });
});
