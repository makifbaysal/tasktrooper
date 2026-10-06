import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { CatalogPending, CatalogSyncState } from "@/api";
import { CatalogPage } from "@/pages/CatalogPage";
import { I18nProvider } from "@/hooks/useI18n";

const { getCatalogStatus, listCatalogPending, applyCatalogPending, dismissCatalogPending } = vi.hoisted(() => ({
  getCatalogStatus: vi.fn(),
  listCatalogPending: vi.fn(),
  applyCatalogPending: vi.fn(),
  dismissCatalogPending: vi.fn(),
}));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return {
    ...actual,
    api: { ...actual.api, getCatalogStatus, listCatalogPending, applyCatalogPending, dismissCatalogPending },
  };
});

vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

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
});
