import "@testing-library/jest-dom/vitest";
import { render, screen, within } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it } from "vitest";
import type { Agent } from "@/api";
import { LeadTeamOverview } from "@/components/chat/LeadTeamOverview";
import { I18nProvider } from "@/hooks/useI18n";

const agent = (id: string, catalog_slug?: string, enabled = true) =>
  ({ id, name: id, catalog_slug, enabled }) as Agent;

function renderTeam(agents: Agent[]) {
  return render(
    <I18nProvider>
      <MemoryRouter>
        <LeadTeamOverview agents={agents} />
      </MemoryRouter>
    </I18nProvider>,
  );
}

function groupOf(label: string) {
  return screen.getByText(label).closest("div") as HTMLElement;
}

describe("LeadTeamOverview", () => {
  it("puts each built-in agent in the stage it works, in flow order", () => {
    renderTeam([
      agent("game-developer", "game-developer"),
      agent("backend-developer", "backend-developer"),
      agent("data-scientist", "data-scientist"),
      agent("security-agent", "security-agent"),
      agent("system-architect", "system-architect"),
    ]);

    const hrefs = (label: string) =>
      within(groupOf(label)).getAllByRole("link").map((l) => l.getAttribute("href"));
    expect(hrefs("Build")).toEqual([
      "/agents/backend-developer/chat",
      "/agents/data-scientist/chat",
      "/agents/game-developer/chat",
    ]);
    expect(hrefs("Review")).toEqual(["/agents/system-architect/chat", "/agents/security-agent/chat"]);
    expect(within(groupOf("Plan")).getByText("system-architect")).toBeInTheDocument();
  });

  it("lists custom agents last and hides disabled ones and empty stages", () => {
    renderTeam([agent("my-reviewer"), agent("qa-agent", "qa-agent", false)]);

    expect(screen.getByText("Your agents")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: /my-reviewer/ })).toHaveAttribute("href", "/agents/my-reviewer/chat");
    expect(screen.queryByText("Test & ship")).not.toBeInTheDocument();
    expect(screen.queryByText("Design")).not.toBeInTheDocument();
  });

  it("renders nothing without agents", () => {
    const { container } = renderTeam([]);
    expect(container).toBeEmptyDOMElement();
  });
});
