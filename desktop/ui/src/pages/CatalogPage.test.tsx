import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { CatalogPending, CatalogSyncState } from "@/api";
import { CatalogPage } from "@/pages/CatalogPage";
import { I18nProvider } from "@/hooks/useI18n";

const { getCatalogStatus, listCatalogPending, applyCatalogPending, dismissCatalogPending, syncCatalog, toastInfo } = vi.hoisted(() => ({
  getCatalogStatus: vi.fn(),
  listCatalogPending: vi.fn(),
  applyCatalogPending: vi.fn(),
  dismissCatalogPending: vi.fn(),
  syncCatalog: vi.fn(),
  toastInfo: vi.fn(),
}));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return {
    ...actual,
    api: { ...actual.api, getCatalogStatus, listCatalogPending, applyCatalogPending, dismissCatalogPending, syncCatalog },
  };
});

vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn(), info: toastInfo } }));

const state: CatalogSyncState = {
  repo_ref: "dir:/catalog",
  last_sync_at: "2026-10-06T10:00:00Z",
  pending_count: 3,
  updated_at: "2026-10-06T10:00:00Z",
};

const parked: CatalogPending = {
  id: "p-1",
  agent_slug: "backend-developer",
  agent_name: "Backend Developer",
  kind: "skill",
  name: "go-testing",
  action: "merge",
  reason: "LLM merge basarisiz: no chat model",
  created_at: "2026-10-06T10:00:00Z",
};

function renderPage() {
  return render(
    <I18nProvider>
      <CatalogPage />
    </I18nProvider>,
  );
}

describe("CatalogPage", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    getCatalogStatus.mockResolvedValue({ configured: true, state });
  });

  it("applies the catalog version of a parked item and reloads the list", async () => {
    listCatalogPending.mockResolvedValueOnce({ items: [parked], count: 1 }).mockResolvedValueOnce({ items: [], count: 0 });
    applyCatalogPending.mockResolvedValue(undefined);
    renderPage();

    fireEvent.click(await screen.findByRole("button", { name: "Use catalog version" }));

    await waitFor(() => expect(applyCatalogPending).toHaveBeenCalledWith("p-1"));
    expect(dismissCatalogPending).not.toHaveBeenCalled();
    expect(await screen.findByText(/Nothing waiting for you/)).toBeInTheDocument();
    expect(listCatalogPending).toHaveBeenCalledTimes(2);
  });

  it("counts waiting items from the live list, not the last sync's snapshot", async () => {
    listCatalogPending.mockResolvedValue({ items: [parked], count: 1 });
    renderPage();

    expect(await screen.findByText("go-testing", { exact: false })).toBeInTheDocument();
    expect(screen.queryByText("3")).not.toBeInTheDocument();
  });

  it("shows the running sync's progress and does not start a second one", async () => {
    getCatalogStatus.mockResolvedValue({
      configured: true,
      state,
      progress: {
        running: true, agent: "security-agent", new_agent: true,
        agents_done: 8, agents_total: 11, skills_done: 6, skills_total: 24,
        agents_added: ["data-scientist", "game-developer"],
      },
    });
    listCatalogPending.mockResolvedValue({ items: [], count: 0 });
    renderPage();

    const card = await screen.findByTestId("catalog-sync-progress");
    expect(card).toHaveTextContent("Adding security-agent");
    expect(card).toHaveTextContent("6 of 24 new skills indexed");
    expect(card).toHaveTextContent("Agent 9 of 11");
    expect(card).toHaveTextContent("added: data-scientist, game-developer");
    expect(screen.getByRole("button", { name: /Syncing/ })).toBeDisabled();
  });

  it("answers a press during a running sync without an error", async () => {
    listCatalogPending.mockResolvedValue({ items: [], count: 0 });
    syncCatalog.mockResolvedValue({
      running: true,
      progress: { running: true, new_agent: false, agents_done: 0, agents_total: 11, skills_done: 0, skills_total: 0 },
    });
    renderPage();

    fireEvent.click(await screen.findByRole("button", { name: "Sync now" }));

    await waitFor(() => expect(toastInfo).toHaveBeenCalledWith("A sync is already running; its progress is shown here."));
  });
});
