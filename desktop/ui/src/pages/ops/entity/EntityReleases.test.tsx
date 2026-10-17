import "@testing-library/jest-dom/vitest";
import { act, fireEvent, render, renderHook, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { StoreAppView, StoreTestBuild, StoreTracks, TrackRelease } from "@/api";
import type { OpsEntity } from "@/components/operations/shell/useOpsEntities";
import { I18nProvider } from "@/hooks/useI18n";
import { CACHE_MOBILE_APPS, CACHE_STORE_CREDENTIALS } from "@/hooks/useMobileReleases";
import { useStoreTestBuilds } from "@/hooks/useStoreTestBuilds";
import { writeCache } from "@/lib/uiCache";

const apiMock = vi.hoisted(() => ({
  listAllStoreApps: vi.fn(),
  listStoreCredentials: vi.fn(),
  storeAppTracks: vi.fn(),
  promoteStoreChannel: vi.fn(),
  setAndroidRollout: vi.fn(),
  haltAndroid: vi.fn(),
  submitIOS: vi.fn(),
  releaseIOS: vi.fn(),
  listStoreTestBuilds: vi.fn(),
  listStoreTestGroups: vi.fn(),
}));

// ChartSlot (for the data-sources path) brings uPlot along; jsdom has no canvas.
vi.mock("uplot", () => ({ default: class {} }));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return { ...actual, api: { ...actual.api, ...apiMock } };
});

import EntityReleases from "@/pages/ops/entity/EntityReleases";

const entity: OpsEntity = {
  id: "c-1",
  name: "Acme mobile",
  role: "mobile",
  repositoryId: "r-1",
  repositoryName: "acme-app",
  projectIds: [],
  environments: [],
};

const release = (overrides: Partial<TrackRelease> = {}): TrackRelease => ({
  has_release: true,
  version: "2.4.0",
  build: "412",
  status: "live",
  ...overrides,
});

function tracksOf(production: Partial<TrackRelease>): StoreTracks {
  return {
    internal: release({ audience: "12 internal testers" }),
    external: release({ status: "in_review" }),
    production: release({ version: "2.3.0", build: "398", ...production }),
  };
}

function storeApp(overrides: Partial<StoreAppView> = {}): StoreAppView {
  return {
    id: "app-ios",
    repository_id: "r-1",
    repository_name: "acme-app",
    platform: "ios",
    identifier: "com.acme.app",
    store_app_id: "1234",
    state: "live",
    review_state: "approved",
    last_released_version: "2.3.0",
    tracks: tracksOf({ status: "rolling_out", user_fraction: 0.2 }),
    tracks_synced_at: new Date().toISOString(),
    created_at: "2026-10-01T00:00:00Z",
    updated_at: "2026-10-01T00:00:00Z",
    ...overrides,
  };
}

const android = () =>
  storeApp({
    id: "app-android",
    platform: "android",
    identifier: "com.acme.app",
    store_app_id: "",
    review_state: undefined,
    tracks: tracksOf({ status: "rolling_out", user_fraction: 0.1 }),
  });

function credentials(asc: boolean, play: boolean) {
  return [
    { provider: "asc", configured: asc, updated_at: "" },
    { provider: "google_play", configured: play, updated_at: "" },
  ];
}

function renderPage(platform?: string) {
  return render(
    <MemoryRouter initialEntries={[`/operations/entity/c-1/releases${platform ? `?platform=${platform}` : ""}`]}>
      <I18nProvider>
        <EntityReleases entity={entity} env="production" />
      </I18nProvider>
    </MemoryRouter>,
  );
}

beforeEach(() => {
  sessionStorage.clear();
  writeCache(CACHE_MOBILE_APPS, undefined);
  writeCache(CACHE_STORE_CREDENTIALS, undefined);
  Object.values(apiMock).forEach((fn) => fn.mockReset());
  apiMock.listAllStoreApps.mockResolvedValue([storeApp(), android(), storeApp({ id: "other", repository_id: "r-2" })]);
  apiMock.listStoreCredentials.mockResolvedValue(credentials(true, true));
  apiMock.storeAppTracks.mockResolvedValue(tracksOf({ status: "rolling_out", user_fraction: 0.2 }));
  apiMock.promoteStoreChannel.mockResolvedValue(undefined);
  apiMock.setAndroidRollout.mockResolvedValue(undefined);
  apiMock.listStoreTestBuilds.mockResolvedValue([]);
  apiMock.listStoreTestGroups.mockResolvedValue([]);
});

describe("EntityReleases", () => {
  it("lays the three channels out in order with the production rollout up front", async () => {
    renderPage();
    const production = await screen.findByRole("group", { name: "Production channel" });
    expect(screen.getByRole("group", { name: "Internal channel" })).toHaveTextContent("12 internal testers");
    expect(screen.getByRole("group", { name: "External channel" })).toHaveTextContent("In review");
    const gauge = within(production).getByTestId("rollout-gauge");
    expect(gauge).toHaveTextContent("20%");
    expect(gauge).toHaveTextContent("Day 5 of 7");
    expect(within(gauge).getByRole("progressbar")).toHaveAttribute("aria-valuenow", "20");
    expect(screen.getByRole("button", { name: /iOS/ })).toHaveAttribute("aria-pressed", "true");
    expect(apiMock.storeAppTracks).not.toHaveBeenCalled();
  });

  it("only offers the next channel from each stage, never a skip", async () => {
    renderPage();
    const internal = await screen.findByRole("group", { name: "Internal channel" });
    const external = screen.getByRole("group", { name: "External channel" });
    const production = screen.getByRole("group", { name: "Production channel" });
    expect(within(internal).getByRole("button", { name: /Promote to External/ })).toBeEnabled();
    expect(within(internal).queryByRole("button", { name: /Promote to Production/ })).toBeNull();
    expect(within(external).getByRole("button", { name: /Promote to Production/ })).toBeEnabled();
    expect(within(production).queryByRole("button", { name: /Promote/ })).toBeNull();

    fireEvent.click(within(internal).getByRole("button", { name: /Promote to External/ }));
    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).queryByRole("textbox")).toBeNull();
    fireEvent.click(within(dialog).getByRole("button", { name: "Promote" }));
    await waitFor(() => expect(apiMock.promoteStoreChannel).toHaveBeenCalledWith("r-1", "ios", "internal", "external", undefined, undefined));
  });

  it("holds a production promote until the repository name is typed", async () => {
    renderPage();
    const external = await screen.findByRole("group", { name: "External channel" });
    fireEvent.click(within(external).getByRole("button", { name: /Promote to Production/ }));
    const dialog = await screen.findByRole("dialog");
    expect(dialog).toHaveTextContent("App Review");
    const confirm = within(dialog).getByRole("button", { name: "Promote" });
    expect(confirm).toBeDisabled();
    fireEvent.change(within(dialog).getByRole("textbox"), { target: { value: "acme" } });
    expect(confirm).toBeDisabled();
    fireEvent.change(within(dialog).getByRole("textbox"), { target: { value: "acme-app" } });
    expect(confirm).toBeEnabled();
    fireEvent.click(confirm);
    await waitFor(() => expect(apiMock.promoteStoreChannel).toHaveBeenCalledWith("r-1", "ios", "external", "production", "acme-app", undefined));
  });

  it("does not open production before the app is live", async () => {
    apiMock.listAllStoreApps.mockResolvedValue([storeApp({ state: "test_ready" })]);
    renderPage();
    const external = await screen.findByRole("group", { name: "External channel" });
    expect(within(external).getByRole("button", { name: /Promote to Production/ })).toBeDisabled();
    expect(external).toHaveTextContent("Production opens once the app is live in the store.");
  });

  it("changes an Android rollout only after a confirm, and only upwards", async () => {
    renderPage("android");
    const production = await screen.findByRole("group", { name: "Production channel" });
    expect(within(production).getByTestId("rollout-gauge")).toHaveTextContent("10%");
    const share = screen.getByLabelText("New rollout share");
    const change = screen.getByRole("button", { name: /Change rollout…/ });

    fireEvent.change(share, { target: { value: "5" } });
    expect(change).toBeDisabled();
    expect(screen.getByText("Pick a share above 10% and at most 100%.")).toBeInTheDocument();

    fireEvent.change(share, { target: { value: "50" } });
    expect(change).toBeEnabled();
    fireEvent.click(change);
    const dialog = await screen.findByRole("dialog");
    expect(dialog).toHaveTextContent("Roll out to 50% of users?");
    expect(apiMock.setAndroidRollout).not.toHaveBeenCalled();
    fireEvent.click(within(dialog).getByRole("button", { name: "Change rollout" }));
    await waitFor(() => expect(apiMock.setAndroidRollout).toHaveBeenCalledWith("r-1", 0.5, undefined));
  });

  it("refuses a share that rounds to nothing, which Play would read as everyone", async () => {
    apiMock.listAllStoreApps.mockResolvedValue([{ ...android(), tracks: tracksOf({ status: "rolling_out", user_fraction: undefined }) }]);
    renderPage("android");
    await screen.findByRole("group", { name: "Production channel" });
    const share = screen.getByLabelText("New rollout share");
    const change = screen.getByRole("button", { name: /Change rollout…/ });
    fireEvent.change(share, { target: { value: "0.004" } });
    expect(change).toBeDisabled();
    fireEvent.change(share, { target: { value: "0.01" } });
    expect(change).toBeEnabled();
  });

  it("asks for the typed name before completing a rollout, and sends what was typed", async () => {
    renderPage("android");
    await screen.findByRole("group", { name: "Production channel" });
    fireEvent.change(screen.getByLabelText("New rollout share"), { target: { value: "100" } });
    fireEvent.click(screen.getByRole("button", { name: /Change rollout…/ }));
    const dialog = await screen.findByRole("dialog");
    const confirm = within(dialog).getByRole("button", { name: "Change rollout" });
    expect(confirm).toBeDisabled();
    fireEvent.change(within(dialog).getByRole("textbox"), { target: { value: "acme-app " } });
    expect(confirm).toBeEnabled();
    fireEvent.click(confirm);
    await waitFor(() => expect(apiMock.setAndroidRollout).toHaveBeenCalledWith("r-1", 1, "acme-app "));
  });

  it("starts a Play production promote as a staged rollout, 10% unless changed", async () => {
    apiMock.listAllStoreApps.mockResolvedValue([android()]);
    renderPage("android");
    const external = await screen.findByRole("group", { name: "External channel" });
    fireEvent.click(within(external).getByRole("button", { name: /Promote to Production/ }));
    const dialog = await screen.findByRole("dialog");
    const share = within(dialog).getByLabelText("Staged rollout share");
    expect(share).toHaveValue("10");
    fireEvent.change(share, { target: { value: "0" } });
    fireEvent.change(within(dialog).getByLabelText("Type acme-app to confirm"), { target: { value: "acme-app" } });
    const confirm = within(dialog).getByRole("button", { name: "Promote" });
    expect(confirm).toBeDisabled();
    fireEvent.change(share, { target: { value: "25" } });
    expect(confirm).toBeEnabled();
    fireEvent.click(confirm);
    await waitFor(() =>
      expect(apiMock.promoteStoreChannel).toHaveBeenCalledWith("r-1", "android", "external", "production", "acme-app", 0.25),
    );
  });

  it("keeps a refused store write's dialog open", async () => {
    apiMock.promoteStoreChannel.mockRejectedValue(new Error("storeops: no build on external"));
    renderPage();
    const internal = await screen.findByRole("group", { name: "Internal channel" });
    fireEvent.click(within(internal).getByRole("button", { name: /Promote to External/ }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.click(within(dialog).getByRole("button", { name: "Promote" }));
    await waitFor(() => expect(apiMock.promoteStoreChannel).toHaveBeenCalled());
    expect(await within(screen.getByRole("dialog")).findByText("No build on external")).toBeInTheDocument();
  });

  it("gates iOS Release on the production channel just read, not the row's review state", async () => {
    apiMock.listAllStoreApps.mockResolvedValue([
      storeApp({ review_state: "approved", tracks: tracksOf({ status: "live", pending_version: "2.4.0", pending_status: "in_review" }) }),
    ]);
    renderPage();
    const release = await screen.findByRole("button", { name: /^Release$/ });
    expect(release).toBeDisabled();
    expect(screen.getByText("2.4.0 is still in App Review.")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Submit for review/ })).toBeDisabled();
  });

  it("offers iOS Release once the pending version is approved, and sends the typed name", async () => {
    apiMock.releaseIOS.mockResolvedValue(undefined);
    apiMock.listAllStoreApps.mockResolvedValue([
      storeApp({ review_state: "waiting_for_review", tracks: tracksOf({ status: "live", pending_version: "2.4.0", pending_status: "approved" }) }),
    ]);
    renderPage();
    const release = await screen.findByRole("button", { name: /^Release$/ });
    expect(release).toBeEnabled();
    expect(screen.getByRole("button", { name: /Submit for review/ })).toBeDisabled();
    fireEvent.click(release);
    const dialog = await screen.findByRole("dialog");
    expect(dialog).toHaveTextContent("Apple has approved 2.4.0");
    fireEvent.change(within(dialog).getByRole("textbox"), { target: { value: "acme-app" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Release" }));
    await waitFor(() => expect(apiMock.releaseIOS).toHaveBeenCalledWith("r-1", "acme-app"));
  });

  it("reads the channels live when the saved read is stale", async () => {
    apiMock.listAllStoreApps.mockResolvedValue([storeApp({ tracks_synced_at: "2026-01-01T00:00:00Z" })]);
    renderPage();
    await waitFor(() => expect(apiMock.storeAppTracks).toHaveBeenCalledWith("r-1", "ios"));
  });

  it("explains a repository with no store app and links to its store settings and Integrations", async () => {
    apiMock.listAllStoreApps.mockResolvedValue([storeApp({ repository_id: "r-2" })]);
    apiMock.listStoreCredentials.mockResolvedValue(credentials(false, false));
    renderPage();
    expect(await screen.findByText("No store app linked")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: /Open store settings/ })).toHaveAttribute("href", "/repositories/r-1?tab=deploy");
    expect(await screen.findByRole("link", { name: /Connect a store/ })).toHaveAttribute("href", "/settings/integrations");
  });

  it("points at Integrations when the app's store credential is missing", async () => {
    apiMock.listStoreCredentials.mockResolvedValue(credentials(false, true));
    renderPage();
    expect(await screen.findByText("App Store Connect isn't connected")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: /Open Integrations/ })).toHaveAttribute("href", "/settings/integrations");
  });

  it("turns a halted rollout into an incident notice that links to the entity's issues", async () => {
    apiMock.listAllStoreApps.mockResolvedValue([android()].map((a) => ({ ...a, tracks: tracksOf({ status: "halted", user_fraction: 0.2 }) })));
    renderPage();
    expect(await screen.findByText("Production rollout halted")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: /View issues/ })).toHaveAttribute("href", "/operations/issues?entity=c-1");
  });
});

describe("test build polling", () => {
  const build = (status: StoreTestBuild["status"]): StoreTestBuild => ({
    id: `b-${status}`,
    repository_id: "r-1",
    platform: "ios",
    attempt: 1,
    sequence: 1,
    build_number: "1.0.1",
    status,
    has_artifact: true,
    groups: null,
    trigger: "manual",
    created_at: "2026-10-16T00:00:00Z",
    updated_at: "2026-10-16T00:00:00Z",
  });

  beforeEach(() => {
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("re-reads every 5 s while a build is moving and stops once none is", async () => {
    apiMock.listStoreTestBuilds.mockResolvedValue([build("building")]);
    const { result } = renderHook(() => useStoreTestBuilds("r-1", { platform: "ios", limit: 20 }));
    await act(() => vi.advanceTimersByTimeAsync(0));
    expect(result.current.builds?.[0].status).toBe("building");
    expect(apiMock.listStoreTestBuilds).toHaveBeenCalledTimes(1);

    apiMock.listStoreTestBuilds.mockResolvedValue([build("ready")]);
    await act(() => vi.advanceTimersByTimeAsync(5_000));
    expect(apiMock.listStoreTestBuilds).toHaveBeenCalledTimes(2);
    expect(result.current.builds?.[0].status).toBe("ready");

    await act(() => vi.advanceTimersByTimeAsync(20_000));
    expect(apiMock.listStoreTestBuilds).toHaveBeenCalledTimes(2);
  });

  it("never polls when nothing is moving", async () => {
    apiMock.listStoreTestBuilds.mockResolvedValue([build("ready"), build("action_required")]);
    renderHook(() => useStoreTestBuilds("r-1", { platform: "ios", limit: 20 }));
    await act(() => vi.advanceTimersByTimeAsync(30_000));
    expect(apiMock.listStoreTestBuilds).toHaveBeenCalledTimes(1);
  });
});
