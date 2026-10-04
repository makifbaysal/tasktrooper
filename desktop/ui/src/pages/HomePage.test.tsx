import { fireEvent, render, screen } from "@testing-library/react";
import { I18nProvider } from "@/hooks/useI18n";
import { MemoryRouter, Outlet, Route, Routes } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";

const createSession = vi.hoisted(() => vi.fn());
vi.mock("@/api", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/api")>();
  return {
    ...actual,
    api: {
      ...actual.api,
      createSession,
      listAgents: () => Promise.resolve({ agents: [] }),
      listInitiativeProjects: () => Promise.resolve({ projects: [] }),
      listRepositories: () => Promise.resolve({ repositories: [] }),
    },
  };
});
import type { Agent } from "@/api";
import type { WorkspaceOutletContext } from "@/hooks/useWorkspaceOutlet";
import { HomePage } from "@/pages/HomePage";

function renderHome(ctx: Partial<WorkspaceOutletContext>) {
  const full: WorkspaceOutletContext = {
    config: null,
    agents: [],
    refreshWorkspace: () => {},
    leadAgent: null,
    workspaceLoading: false,
    teamPreparing: false,
    ...ctx,
  };
  return render(
    <I18nProvider>
    <MemoryRouter initialEntries={["/home"]}>
      <Routes>
        <Route element={<Outlet context={full} />}>
          <Route path="home" element={<HomePage />} />
          <Route path="board" element={<p>board page</p>} />
          <Route path="agents/:id/chat" element={<p>lead chat</p>} />
          <Route path="agents/:id/chat/:sessionId" element={<p>lead chat</p>} />
        </Route>
      </Routes>
    </MemoryRouter>
    </I18nProvider>,
  );
}

describe("HomePage", () => {
  it("is the lead's welcome screen, with no chat history beside it", () => {
    renderHome({ leadAgent: { id: "pm", name: "product-manager" } as Agent });
    expect(screen.getByRole("heading", { name: "What shall we work on?" })).toBeTruthy();
    expect(screen.queryByText("lead chat")).toBeNull();
  });

  it("opens a new lead conversation carrying the message", async () => {
    createSession.mockResolvedValue({ id: "s-9" });
    renderHome({ leadAgent: { id: "pm", name: "product-manager" } as Agent });
    fireEvent.change(screen.getByRole("textbox"), { target: { value: "Kupon ekleyelim" } });
    fireEvent.keyDown(screen.getByRole("textbox"), { key: "Enter" });
    expect(await screen.findByText("lead chat")).toBeTruthy();
    expect(createSession).toHaveBeenCalledWith(expect.objectContaining({ agent_id: "pm" }));
  });

  it("shows a spinner while the workspace is loading", () => {
    const { container } = renderHome({ workspaceLoading: true });
    expect(container.querySelector("svg.animate-spin")).not.toBeNull();
    expect(screen.queryByText("board page")).toBeNull();
  });

  it("waits on the catalog's first sync instead of sending a fresh install to the board", () => {
    renderHome({ teamPreparing: true });
    expect(screen.queryByText("board page")).toBeNull();
    expect(screen.getByRole("link", { name: /board/i })).toBeTruthy();
  });

  it("falls back to the board when nobody can lead", () => {
    renderHome({});
    expect(screen.getByText("board page")).toBeTruthy();
  });
});
