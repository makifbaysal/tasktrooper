import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { ConfidenceBadge } from "@/components/projects/model/ConfidenceBadge";
import { I18nProvider } from "@/hooks/useI18n";

function renderConfidence(confidence?: Parameters<typeof ConfidenceBadge>[0]["confidence"]) {
  return render(
    <I18nProvider>
      <ConfidenceBadge confidence={confidence} />
    </I18nProvider>,
  );
}

describe("ConfidenceBadge", () => {
  it("renders nothing when there is no confidence", () => {
    const { container } = renderConfidence(undefined);
    expect(container).toBeEmptyDOMElement();
  });

  it("gives medium confidence the warning token — this is the review queue's own signal", () => {
    renderConfidence("medium");
    const badge = screen.getByText("Medium");
    expect(badge.className).toContain("bg-warning/15");
  });

  it("gives exact confidence the success token, distinguishable from medium", () => {
    renderConfidence("exact");
    const badge = screen.getByText("Exact");
    expect(badge.className).toContain("bg-success/15");
  });
});
