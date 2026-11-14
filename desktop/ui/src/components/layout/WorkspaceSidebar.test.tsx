import { fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";
import type { Agent } from "@/api";
import { WorkspaceSidebar } from "@/components/layout/WorkspaceSidebar";
import { I18nProvider } from "@/hooks/useI18n";

vi.mock("@/hooks/useSetup", () => ({ useSetup: () => ({ needsWork: false }) }));
vi.mock("@/components/workspace/NewAgentDialog", () => ({ NewAgentDialog: () => null }));
vi.mock("@/components/workspace/TeamEditDialog", () => ({
  TeamEditDialog: ({ open }: { open: boolean }) => (open ? <div>team editor open</div> : null),
}));

const AGENTS_HEADER = /agent chats|ajan sohbetleri/i;

function makeAgent(id: string, name: string): Agent {
  return { id, name, enabled: true } as Agent;
}

const pm = makeAgent("pm", "Product Manager");
const dev = makeAgent("dev", "Developer");
const qa = makeAgent("qa", "QA Engineer");

function renderSidebar(leadAgent: Agent | null, unreadCounts: Record<string, number> = {}) {
  return render(
    <I18nProvider>
      <MemoryRouter>
        <WorkspaceSidebar
          config={null}
          agents={[dev, pm, qa]}
          leadAgent={leadAgent}
          loading={false}
          collapsed={false}
          onToggle={() => {}}
          mobileOpen={false}
          onMobileClose={() => {}}
          onRefresh={() => {}}
          unreadCounts={unreadCounts}
        />
      </MemoryRouter>
    </I18nProvider>,
  );
}

describe("WorkspaceSidebar", () => {
  it("puts the lead first in the team block, links it, and hides the flat list header", () => {
    renderSidebar(pm);
    const chatLinks = screen
      .getAllByRole("link")
      .filter((a) => a.getAttribute("href")?.startsWith("/agents/"));
    expect(chatLinks.map((a) => a.getAttribute("href"))).toEqual([
      "/agents/pm/chat",
      "/agents/dev/chat",
      "/agents/qa/chat",
    ]);
    expect(screen.getByRole("link", { name: /Product Manager/ })).toBeTruthy();
    expect(screen.getByRole("link", { name: /Developer/ })).toBeTruthy();
    expect(screen.queryByText(AGENTS_HEADER)).toBeNull();
  });

  it("opens the team editor from Edit team", () => {
    renderSidebar(pm);
    expect(screen.queryByText("team editor open")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: /edit team|ekibi düzenle/i }));
    expect(screen.getByText("team editor open")).toBeTruthy();
  });

  it("shows an unread count badge on a team member, not a count-less dot", () => {
    renderSidebar(pm, { dev: 3 });
    expect(screen.getByText("3")).toBeTruthy();
  });

  it("caps the badge at 9+ for a large unread count", () => {
    renderSidebar(pm, { dev: 12 });
    expect(screen.getByText("9+")).toBeTruthy();
  });

  it("shows no badge for an agent with no unread count", () => {
    renderSidebar(pm, { dev: 0 });
    expect(screen.queryByText("0")).toBeNull();
  });

  it("falls back to the flat agent list without a lead", () => {
    renderSidebar(null);
    expect(screen.getByText(AGENTS_HEADER)).toBeTruthy();
    const chatLinks = screen
      .getAllByRole("link")
      .filter((a) => a.getAttribute("href")?.startsWith("/agents/"));
    expect(chatLinks.map((a) => a.getAttribute("href"))).toEqual([
      "/agents/dev/chat",
      "/agents/pm/chat",
      "/agents/qa/chat",
    ]);
  });

  it("offers Edit team but no hand-made agent button while advanced team config is off", () => {
    renderSidebar(pm);
    expect(screen.queryByTitle(/new agent|yeni ajan/i)).toBeNull();
    expect(screen.getByLabelText(/edit team|ekibi düzenle/i)).toBeTruthy();
  });
});
