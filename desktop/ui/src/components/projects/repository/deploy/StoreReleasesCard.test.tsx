import "@testing-library/jest-dom/vitest";
import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { api, type StoreCredentialView } from "@/api";
import { StoreReleasesCard } from "@/components/projects/repository/deploy/StoreReleasesCard";
import { I18nProvider } from "@/hooks/useI18n";

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return { ...actual, api: { ...actual.api, listStoreCredentials: vi.fn() } };
});

vi.mock("@/components/projects/MobileStorePanel", async () => {
  const actual = await vi.importActual<typeof import("@/components/projects/MobileStorePanel")>(
    "@/components/projects/MobileStorePanel",
  );
  return { ...actual, MobileStorePanel: () => <div data-testid="mobile-store-panel" /> };
});

function credential(provider: StoreCredentialView["provider"], configured: boolean): StoreCredentialView {
  return { provider, configured, updated_at: "2024-01-01T00:00:00Z" };
}

function renderCard(mobilePlatform: "ios" | "android" | "cross_platform") {
  return render(
    <MemoryRouter>
      <I18nProvider>
        <StoreReleasesCard repositoryId="repo-1" mobilePlatform={mobilePlatform} />
      </I18nProvider>
    </MemoryRouter>,
  );
}

describe("StoreReleasesCard", () => {
  beforeEach(() => vi.mocked(api.listStoreCredentials).mockReset());

  it("asks for a store account, not a cloud one, when none is connected", async () => {
    vi.mocked(api.listStoreCredentials).mockResolvedValue([credential("asc", false), credential("google_play", false)]);
    renderCard("cross_platform");

    expect(await screen.findByText("No store accounts connected")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Connect a store account" })).toBeInTheDocument();
    expect(screen.queryByText(/cloud account/i)).not.toBeInTheDocument();
    expect(screen.queryByTestId("mobile-store-panel")).not.toBeInTheDocument();
  });

  it("ignores a connected store the component does not ship to", async () => {
    vi.mocked(api.listStoreCredentials).mockResolvedValue([credential("asc", false), credential("google_play", true)]);
    renderCard("ios");

    expect(await screen.findByText("No store accounts connected")).toBeInTheDocument();
  });

  it("shows the store panel once a matching store account is connected", async () => {
    vi.mocked(api.listStoreCredentials).mockResolvedValue([credential("asc", true), credential("google_play", false)]);
    renderCard("ios");

    expect(await screen.findByTestId("mobile-store-panel")).toBeInTheDocument();
  });
});
