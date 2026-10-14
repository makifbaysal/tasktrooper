import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { AppSettings } from "@/api";
import { I18nProvider } from "@/hooks/useI18n";
import { SettingsPage } from "@/pages/SettingsPage";

if (!Element.prototype.scrollIntoView) {
  Element.prototype.scrollIntoView = () => {};
}

const { getSettings, updateSettings, setStoredLocale } = vi.hoisted(() => ({
  getSettings: vi.fn(),
  updateSettings: vi.fn(),
  setStoredLocale: vi.fn(),
}));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return { ...actual, setStoredLocale, api: { ...actual.api, getSettings, updateSettings } };
});

const SETTINGS = {
  default_language: "en",
  max_concurrent_agents: 0,
  max_concurrent_tasks: 0,
  boilerplate_catalog_repo: "",
} as unknown as AppSettings;

function stubShell() {
  window.__tasktrooperDesktop = {
    info: () => Promise.resolve({ app: "TaskTrooper", version: "1.0.0", platform: "darwin" }),
    account: {
      signIn: vi.fn(),
      signOut: vi.fn(),
      state: () => Promise.resolve({ mode: "local" }),
      openKeys: vi.fn(),
    },
    updates: {
      status: () => Promise.resolve({ phase: "current" }),
      subscribe: () => () => undefined,
      check: vi.fn(),
      restart: vi.fn(),
    },
    analytics: {
      get: () => Promise.resolve({ available: true, enabled: true, forcedOff: false }),
      set: vi.fn(),
    },
  } as never;
}

function renderPage() {
  return render(
    <I18nProvider>
      <SettingsPage />
    </I18nProvider>,
  );
}

beforeEach(() => {
  vi.clearAllMocks();
  getSettings.mockResolvedValue(SETTINGS);
  updateSettings.mockImplementation((patch: Partial<AppSettings>) => Promise.resolve({ ...SETTINGS, ...patch }));
});

afterEach(() => {
  delete window.__tasktrooperDesktop;
});

describe("SettingsPage", () => {
  it("in a browser, says where the desktop-only settings are instead of showing them", async () => {
    renderPage();
    expect(await screen.findByText(/are set in the TaskTrooper desktop app/)).toBeInTheDocument();
    expect(screen.queryByRole("heading", { name: "Desktop app" })).not.toBeInTheDocument();
    expect(screen.queryByRole("switch", { name: "Anonymous usage statistics" })).not.toBeInTheDocument();
  });

  it("in the desktop app, offers the anonymous usage switch under Desktop app", async () => {
    stubShell();
    renderPage();
    expect(await screen.findByRole("switch", { name: "Anonymous usage statistics" })).toBeChecked();
    expect(screen.getByRole("heading", { name: "Desktop app" })).toBeInTheDocument();
    expect(screen.queryByText(/are set in the TaskTrooper desktop app/)).not.toBeInTheDocument();
  });

  it("saves a language as soon as it is picked", async () => {
    renderPage();
    fireEvent.click(await screen.findByLabelText("Language"));
    fireEvent.click(await screen.findByRole("option", { name: "Türkçe" }));

    await waitFor(() => expect(updateSettings).toHaveBeenCalledWith({ default_language: "tr" }));
    await waitFor(() => expect(setStoredLocale).toHaveBeenCalledWith("tr"));
  });

  it("puts the previous language back when the save is refused", async () => {
    updateSettings.mockRejectedValue(new Error("nope"));
    renderPage();
    fireEvent.click(await screen.findByLabelText("Language"));
    fireEvent.click(await screen.findByRole("option", { name: "Türkçe" }));

    await waitFor(() => expect(updateSettings).toHaveBeenCalled());
    await waitFor(() => expect(screen.getByLabelText("Language")).toHaveTextContent("English"));
    expect(setStoredLocale).toHaveBeenLastCalledWith("en");
  });
});
