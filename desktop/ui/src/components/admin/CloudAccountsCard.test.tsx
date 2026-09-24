import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it, vi, beforeEach } from "vitest";
import type { CloudAccount } from "@/api";
import { CloudAccountsCard } from "@/components/admin/CloudAccountsCard";
import { I18nProvider } from "@/hooks/useI18n";

const { listCloudAccounts, deleteCloudAccount } = vi.hoisted(() => ({
  listCloudAccounts: vi.fn(),
  deleteCloudAccount: vi.fn(),
}));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return {
    ...actual,
    api: { ...actual.api, listCloudAccounts, deleteCloudAccount },
  };
});

function account(overrides: Partial<CloudAccount> = {}): CloudAccount {
  return {
    id: "acc-1",
    provider: "vercel",
    label: "Production Vercel",
    meta: { team_slug: "acme" },
    status: "ok",
    verified_at: "2026-01-01T00:00:00Z",
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    ...overrides,
  };
}

function renderCard() {
  return render(
    <I18nProvider>
      <CloudAccountsCard />
    </I18nProvider>,
  );
}

describe("CloudAccountsCard", () => {
  beforeEach(() => {
    listCloudAccounts.mockReset();
    deleteCloudAccount.mockReset();
  });

  it("lists connected accounts with their status and identity", async () => {
    listCloudAccounts.mockResolvedValue({ accounts: [account()] });
    renderCard();

    expect(await screen.findByText("Production Vercel")).toBeInTheDocument();
    expect(screen.getByText("Connected")).toBeInTheDocument();
    expect(screen.getByText("acme")).toBeInTheDocument();
  });

  it("removes an account after confirming", async () => {
    listCloudAccounts.mockResolvedValue({ accounts: [account()] });
    deleteCloudAccount.mockResolvedValue(undefined);
    renderCard();

    await screen.findByText("Production Vercel");
    fireEvent.click(screen.getByRole("button", { name: /remove/i }));

    const dialog = await screen.findByRole("dialog");
    fireEvent.click(within(dialog).getByRole("button", { name: "Remove" }));

    await waitFor(() => expect(deleteCloudAccount).toHaveBeenCalledWith("acc-1"));
    await waitFor(() => expect(screen.queryByText("Production Vercel")).not.toBeInTheDocument());
  });
});
