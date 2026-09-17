import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { ActivityPanel } from "@/components/chat/ActivityPanel";
import { I18nProvider } from "@/hooks/useI18n";

function renderPanel(embedded: boolean) {
  render(
    <I18nProvider>
      <ActivityPanel
        activeRuns={[]}
        runs={[]}
        selectedRunId={null}
        onSelectRun={() => {}}
        steps={[]}
        plan={null}
        embedded={embedded}
      />
    </I18nProvider>,
  );
}

describe("ActivityPanel elevation", () => {
  it("shares the Card Level-1 raised shadow when shown as a standalone panel", () => {
    renderPanel(false);
    const panel = screen.getByTestId("activity-panel");
    expect(panel.className).toContain("shadow-[var(--shadow-raised)]");
  });

  it("uses the micro type scale for the uppercase section labels", () => {
    renderPanel(false);
    const label = screen.getByText("Active runs");
    expect(label.className).toContain("text-micro");
    expect(label.className).not.toContain("text-xs");
  });
});
