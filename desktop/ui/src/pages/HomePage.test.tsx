import { render, screen } from "@testing-library/react";
import { MemoryRouter, Outlet, Route, Routes } from "react-router-dom";
import { describe, expect, it } from "vitest";
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
    ...ctx,
  };
  return render(
    <MemoryRouter initialEntries={["/home"]}>
      <Routes>
        <Route element={<Outlet context={full} />}>
          <Route path="home" element={<HomePage />} />
          <Route path="board" element={<p>board page</p>} />
          <Route path="agents/:id/chat" element={<p>lead chat</p>} />
        </Route>
      </Routes>
    </MemoryRouter>,
  );
}

describe("HomePage", () => {
  it("redirects to the lead's chat", () => {
    renderHome({ leadAgent: { id: "pm", name: "PM" } as Agent });
    expect(screen.getByText("lead chat")).toBeTruthy();
  });

  it("shows a spinner while the workspace is loading", () => {
    const { container } = renderHome({ workspaceLoading: true });
    expect(container.querySelector("svg.animate-spin")).not.toBeNull();
    expect(screen.queryByText("board page")).toBeNull();
  });

  it("falls back to the board when nobody can lead", () => {
    renderHome({});
    expect(screen.getByText("board page")).toBeTruthy();
  });
});
