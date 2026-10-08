import "@testing-library/jest-dom/vitest";
import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it } from "vitest";
import type { CatalogSyncProgress } from "@/api";
import { CatalogSyncIndicator } from "@/components/layout/CatalogSyncIndicator";
import { I18nProvider } from "@/hooks/useI18n";

const idle: CatalogSyncProgress = {
  running: false, new_agent: false, agents_done: 0, agents_total: 0, skills_done: 0, skills_total: 0,
};

function renderIndicator(progress: CatalogSyncProgress | null) {
  return render(
    <I18nProvider>
      <MemoryRouter>
        <CatalogSyncIndicator progress={progress} />
      </MemoryRouter>
    </I18nProvider>,
  );
}

describe("CatalogSyncIndicator", () => {
  it("shows the agent being added and its skill count, linking to the catalog page", () => {
    renderIndicator({
      running: true, agent: "game-developer", new_agent: true,
      agents_done: 3, agents_total: 11, skills_done: 12, skills_total: 41,
    });
    const link = screen.getByTestId("catalog-sync-indicator");
    expect(link).toHaveAttribute("href", "/settings/catalog");
    expect(screen.getByText("Adding game-developer")).toBeInTheDocument();
    expect(screen.getByText("12/41 skills")).toBeInTheDocument();
    expect(link).toHaveAttribute("title", expect.stringContaining("agent 4 of 11"));
  });

  it("stays hidden while idle and during a pass with nothing new", () => {
    const { container, rerender } = renderIndicator(idle);
    expect(container).toBeEmptyDOMElement();
    rerender(
      <I18nProvider>
        <MemoryRouter>
          <CatalogSyncIndicator progress={{ ...idle, running: true, agent: "qa-agent", agents_done: 5, agents_total: 11 }} />
        </MemoryRouter>
      </I18nProvider>,
    );
    expect(screen.queryByTestId("catalog-sync-indicator")).not.toBeInTheDocument();
  });
});
