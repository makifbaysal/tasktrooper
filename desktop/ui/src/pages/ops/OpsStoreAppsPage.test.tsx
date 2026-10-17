import "@testing-library/jest-dom/vitest";
import { render, screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { ProjectsOverview, StoreAppView } from "@/api";
import { I18nProvider } from "@/hooks/useI18n";
import { CACHE_MOBILE_APPS, CACHE_STORE_CREDENTIALS } from "@/hooks/useMobileReleases";
import { writeCache } from "@/lib/uiCache";
import { repo } from "@/test/opsConsoleFixtures";

const apiMock = vi.hoisted(() => ({
  getProjectsOverview: vi.fn(),
  listAllStoreApps: vi.fn(),
  listStoreCredentials: vi.fn(),
  storeAppTracks: vi.fn(),
  listStoreTestBuilds: vi.fn(),
  listStoreTestGroups: vi.fn(),
}));

vi.mock("uplot", () => ({ default: class {} }));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return { ...actual, api: { ...actual.api, ...apiMock } };
});

import OpsStoreAppsPage from "@/pages/ops/OpsStoreAppsPage";

const app: StoreAppView = {
  id: "app-ios",
  repository_id: "r-tools",
  repository_name: "tools",
  platform: "ios",
  identifier: "com.acme.tools",
  store_app_id: "1234",
  state: "live",
  tracks: {
    internal: { has_release: true, version: "1.1", status: "live" },
    external: { has_release: true, version: "1.1", status: "live" },
    production: { has_release: true, version: "1.0", status: "live" },
  },
  tracks_synced_at: new Date().toISOString(),
  created_at: "2026-10-01T00:00:00Z",
  updated_at: "2026-10-01T00:00:00Z",
};

function renderAt(path: string) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <I18nProvider>
        <Routes>
          <Route path="/operations/mobile/app/:repositoryId" element={<OpsStoreAppsPage />} />
        </Routes>
      </I18nProvider>
    </MemoryRouter>,
  );
}

beforeEach(() => {
  sessionStorage.clear();
  for (const key of [CACHE_MOBILE_APPS, CACHE_STORE_CREDENTIALS, "operations.entities.overview"]) writeCache(key, undefined);
  Object.values(apiMock).forEach((fn) => fn.mockReset());
  apiMock.getProjectsOverview.mockResolvedValue({
    projects: [],
    unassigned: [repo("r-tools", "tools", [], [])],
  } as unknown as ProjectsOverview);
  apiMock.listAllStoreApps.mockResolvedValue([app]);
  apiMock.listStoreCredentials.mockResolvedValue([{ provider: "asc", configured: true, updated_at: "" }]);
  apiMock.listStoreTestBuilds.mockResolvedValue([]);
  apiMock.listStoreTestGroups.mockResolvedValue([]);
});

describe("OpsStoreAppsPage", () => {
  it("shows a repository's store apps when no mobile component carries them", async () => {
    renderAt("/operations/mobile/app/r-tools");
    expect(await screen.findByRole("heading", { name: "tools — store apps" })).toBeInTheDocument();
    expect(await screen.findByRole("group", { name: "Production channel" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: /Open repository/ })).toHaveAttribute("href", "/repositories/r-tools");
  });

  it("says so for a repository it doesn't know", async () => {
    apiMock.listAllStoreApps.mockResolvedValue([]);
    renderAt("/operations/mobile/app/r-gone");
    expect(await screen.findByText("No such repository")).toBeInTheDocument();
  });
});
