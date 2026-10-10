import { render, screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";
import { AdvancedConfigRoute } from "@/components/layout/AdvancedConfigRoute";
import { SettingsLayout } from "@/components/layout/SettingsLayout";
import { I18nProvider } from "@/hooks/useI18n";
import { BoardSettingsPage } from "@/pages/BoardSettingsPage";

const features = vi.hoisted(() => ({ ADVANCED_TEAM_CONFIG: false }));
vi.mock("@/lib/features", () => features);
vi.mock("@/pages/BoardSettingsPage", () => ({ BoardSettingsPage: () => <div>board-settings-page</div> }));

function renderAt(path: string) {
  return render(
    <I18nProvider>
      <MemoryRouter initialEntries={[path]}>
        <Routes>
          <Route path="settings" element={<SettingsLayout />}>
            <Route index element={<div>general-page</div>} />
            <Route path="board" element={<AdvancedConfigRoute><BoardSettingsPage /></AdvancedConfigRoute>} />
          </Route>
          <Route
            path="agents/new"
            element={<AdvancedConfigRoute redirectTo="/board"><div>new-agent-page</div></AdvancedConfigRoute>}
          />
          <Route path="board" element={<div>board-page</div>} />
        </Routes>
      </MemoryRouter>
    </I18nProvider>,
  );
}

describe("advanced team configuration switch", () => {
  it("hides the Board, Roles and Workflows tabs and redirects their routes when off", () => {
    features.ADVANCED_TEAM_CONFIG = false;
    renderAt("/settings/board");
    expect(screen.getByText("general-page")).toBeTruthy();
    expect(screen.queryByText("board-settings-page")).toBeNull();
    expect(screen.queryByRole("link", { name: /^board$/i })).toBeNull();
    expect(screen.queryByRole("link", { name: /roles|roller/i })).toBeNull();
    expect(screen.queryByRole("link", { name: /workflows|iş akışları/i })).toBeNull();
  });

  it("redirects the new agent route to the board when off", () => {
    features.ADVANCED_TEAM_CONFIG = false;
    renderAt("/agents/new");
    expect(screen.getByText("board-page")).toBeTruthy();
  });

  it("still renders the hidden pages and tabs when on", () => {
    features.ADVANCED_TEAM_CONFIG = true;
    renderAt("/settings/board");
    expect(screen.getByText("board-settings-page")).toBeTruthy();
    expect(screen.getByRole("link", { name: /^board$/i })).toBeTruthy();
  });
});
