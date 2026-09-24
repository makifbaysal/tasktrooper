import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { DeploySettingsSection } from "./DeploySettingsSection";
import { I18nProvider } from "@/hooks/useI18n";

const { getRepository, listStoreCredentials, getEnvInventory } = vi.hoisted(() => ({
  getRepository: vi.fn(),
  listStoreCredentials: vi.fn(),
  getEnvInventory: vi.fn(),
}));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return {
    ...actual,
    api: { ...actual.api, getRepository, listStoreCredentials, getEnvInventory },
  };
});

// The children are stubbed to their props: what is under test is which scope
// each one is handed, not what they render for it.
vi.mock("@/components/projects/DeployTargetsSection", () => ({
  DeployTargetsSection: ({ subProjectPath, kind }: { subProjectPath?: string; kind?: string }) => (
    <div data-testid="targets" data-scope={subProjectPath ?? ""} data-kind={kind ?? ""} />
  ),
}));

vi.mock("@/components/projects/MobileStorePanel", () => ({
  MobileStorePanel: ({ mobilePlatform }: { mobilePlatform: string }) => (
    <div data-testid="store" data-platform={mobilePlatform} />
  ),
}));

function renderSection() {
  return render(
    <MemoryRouter>
      <I18nProvider>
        <DeploySettingsSection repositoryId="repo-1" />
      </I18nProvider>
    </MemoryRouter>,
  );
}

describe("DeploySettingsSection scope", () => {
  beforeEach(() => {
    Element.prototype.scrollIntoView = vi.fn();
    getRepository.mockReset();
    listStoreCredentials.mockReset().mockResolvedValue([]);
    getEnvInventory.mockReset().mockResolvedValue({ files: [], keys: [] });
    getRepository.mockResolvedValue({
      id: "repo-1",
      name: "pishio",
      description: "",
      root_path: "/tmp/pishio",
      kind: "monorepo",
      sub_projects: [
        { path: "backend", kind: "backend" },
        { path: "mobile", kind: "mobile", mobile_platform: "ios" },
      ],
    });
  });

  it("starts on the repository itself and hands that scope to the targets section", async () => {
    renderSection();

    expect(await screen.findByTestId("targets")).toHaveAttribute("data-scope", "");
    expect(screen.getByTestId("targets")).toHaveAttribute("data-kind", "monorepo");
  });

  it("moves the addresses to the sub-project the scope names", async () => {
    renderSection();
    fireEvent.click(await screen.findByLabelText(/scope/i));
    fireEvent.click(await screen.findByRole("option", { name: "backend" }));

    await waitFor(() => expect(screen.getByTestId("targets")).toHaveAttribute("data-scope", "backend"));
    expect(screen.getByTestId("targets")).toHaveAttribute("data-kind", "backend");
  });

  // A mobile sub-project ships through a store console, so that scope gets the
  // store panel; a non-mobile scope gets none (the Deploy & Runtime tab, a
  // later UI step, is where that story now lives).
  it("swaps in the store panel on a mobile sub-project", async () => {
    renderSection();
    fireEvent.click(await screen.findByLabelText(/scope/i));
    fireEvent.click(await screen.findByRole("option", { name: "mobile" }));

    await waitFor(() => expect(screen.getByTestId("store")).toBeInTheDocument());
    expect(screen.getByTestId("store")).toHaveAttribute("data-platform", "ios");
    expect(screen.getByTestId("targets")).toHaveAttribute("data-kind", "mobile");
  });
});
