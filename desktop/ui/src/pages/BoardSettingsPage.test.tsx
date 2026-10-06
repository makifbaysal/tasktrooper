import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeAll, describe, expect, it, vi } from "vitest";
import type { WorkspaceConfig } from "@/api";
import { I18nProvider } from "@/hooks/useI18n";
import { BoardSettingsPage } from "@/pages/BoardSettingsPage";

const { getWorkspaceConfig, updateBoardColumns, setBoardTransitions } = vi.hoisted(() => ({
  getWorkspaceConfig: vi.fn<() => Promise<WorkspaceConfig>>(),
  updateBoardColumns: vi.fn(),
  setBoardTransitions: vi.fn(),
}));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return { ...actual, api: { ...actual.api, getWorkspaceConfig, updateBoardColumns, setBoardTransitions } };
});

beforeAll(() => {
  // React Flow measures its viewport; jsdom has no layout engine.
  globalThis.ResizeObserver ??= class {
    observe() {}
    unobserve() {}
    disconnect() {}
  } as unknown as typeof ResizeObserver;
});

const column = (slug: string, label: string, position: number) => ({
  id: slug,
  slug,
  label,
  position,
  is_backlog: slug === "backlog",
});

describe("BoardSettingsPage", () => {
  it("resets the rules to the recommended ones for the columns the board has", async () => {
    getWorkspaceConfig.mockResolvedValue({
      settings: { key_prefix: "T" },
      columns: [column("backlog", "Backlog", 0), column("todo", "Todo", 1), column("in_progress", "In Progress", 2)],
      members: [],
      subscriptions: [],
      transitions: [{ from: "todo", to: "backlog" }],
      default_transitions: [
        { from: "backlog", to: "todo" },
        { from: "todo", to: "in_progress" },
        { from: "in_progress", to: "code_review" },
      ],
    });
    updateBoardColumns.mockResolvedValue(undefined);
    setBoardTransitions.mockResolvedValue(undefined);

    render(
      <I18nProvider>
        <BoardSettingsPage />
      </I18nProvider>,
    );

    fireEvent.click(await screen.findByRole("button", { name: "Reset to defaults" }));
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() =>
      expect(setBoardTransitions).toHaveBeenCalledWith([
        { from: "backlog", to: "todo" },
        { from: "todo", to: "in_progress" },
      ]),
    );
  });
});
