import "@testing-library/jest-dom/vitest";
import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter, Route, Routes, useLocation } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ApiError, type ProjectsOverview, type Release } from "@/api";
import { I18nProvider } from "@/hooks/useI18n";
import { writeCache } from "@/lib/uiCache";
import { release, shopRepo, siteRepo } from "@/test/opsConsoleFixtures";

vi.mock("uplot", () => ({
  default: class {
    static pxRatio = 1;
    static rangeNum = (min: number, max: number) => [min, max];
    setData() {}
    setSize() {}
    redraw() {}
    destroy() {}
  },
}));

const { getProjectsOverview, listIncidents, listAllReleases, getRelease, rollbackRelease, deployRelease, finishRelease, getDeployMatrix } = vi.hoisted(
  () => ({
    getDeployMatrix: vi.fn(),
    getProjectsOverview: vi.fn(),
    listIncidents: vi.fn(),
    listAllReleases: vi.fn(),
    getRelease: vi.fn(),
    rollbackRelease: vi.fn(),
    deployRelease: vi.fn(),
    finishRelease: vi.fn(),
  }),
);

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return {
    ...actual,
    api: { ...actual.api, getProjectsOverview, listIncidents, listAllReleases, getRelease, rollbackRelease, deployRelease, finishRelease, getDeployMatrix },
  };
});

import { OpsShell } from "@/components/operations/shell/OpsShell";
import OpsChangesPage from "@/pages/ops/OpsChangesPage";
import OpsEntityPage from "@/pages/ops/OpsEntityPage";
import EntityChanges from "@/pages/ops/entity/EntityChanges";
import { EntitySubRoute } from "@/pages/ops/entity/EntitySubRoute";

const overview = {
  projects: [{ id: "p-shop", name: "Shop", repositories: [{ ...shopRepo, remote_url: "git@github.com:acme/shop.git" }] }],
  unassigned: [siteRepo],
} as unknown as ProjectsOverview;

const FROM = Date.parse("2026-10-14T00:00:00Z");
const TO = Date.parse("2026-10-16T00:00:00Z");
const RANGE = `range=custom&from=${FROM}&to=${TO}`;
const CHANGE_STATUSES = ["pending", "deploying", "verifying", "awaiting_verdict", "rolling_back", "released", "rolled_back", "failed"];

const shipped = release("2.0.0", "r-shop", {
  id: "rel-200",
  component_id: "c-api",
  status: "released",
  commit_sha: "abcdef1234567",
  deploy_started_at: "2026-10-15T09:58:00Z",
  deployed_at: "2026-10-15T10:00:00Z",
  finished_at: "2026-10-15T10:20:00Z",
  tasks: [{ id: "t-1", key: "SHOP-1", title: "Faster checkout" }],
  checks: {
    health: [
      { at: "2026-10-15T10:05:00Z", ok: true, status: 200, latency_ms: 41 },
      { at: "2026-10-15T10:10:00Z", ok: true, status: 200, latency_ms: 38 },
    ],
    smoke: [{ check: { path: "/health" }, url: "https://api.shop.test/health", at: "2026-10-15T10:06:00Z", ok: true, status: 200 }],
  },
});
const failed = release("1.9.0", "r-shop", {
  id: "rel-190",
  component_id: "c-api",
  status: "failed",
  created_at: "2026-10-14T09:00:00Z",
  deployed_at: "2026-10-14T10:00:00Z",
  failure_reason: "smoke checks failed",
  checks: {
    new_errors: [
      { fingerprint: "a", message: "TypeError: x is undefined", count: 9, first_seen: "2026-10-14T10:02:00Z", last_seen: "", new: true },
      { fingerprint: "b", message: "Timeout", count: 2, first_seen: "2026-10-14T10:03:00Z", last_seen: "", new: true },
    ],
    early_stop: "2 new error groups",
  },
});
const verifying = release("site-9", "r-site", {
  id: "rel-site",
  status: "verifying",
  deployed_at: "2026-10-15T11:00:00Z",
  commit_sha: "1234567abcdef",
});
const staged = release("2.1.0-rc", "r-shop", {
  id: "rel-rc",
  component_id: "c-api",
  deploy: { env: "stage" } as Release["deploy"],
  deployed_at: "2026-10-15T08:00:00Z",
});
const FEED = [verifying, shipped, staged, failed];

function Where() {
  const { pathname, search } = useLocation();
  return <p data-testid="where">{`${pathname}${search}`}</p>;
}

function renderAt(path: string) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <I18nProvider>
        <Routes>
          <Route path="/operations" element={<OpsShell />}>
            <Route path="changes" element={<OpsChangesPage />} />
            <Route path="entity/:componentId" element={<OpsEntityPage />}>
              <Route path="changes" element={<EntitySubRoute tab="changes" page={EntityChanges} />} />
            </Route>
          </Route>
        </Routes>
        <Where />
      </I18nProvider>
    </MemoryRouter>,
  );
}

const CACHE_KEYS = [
  "operations.entities.overview",
  "operations.console.openIncidents",
  "operations.console.shippedReleases",
  "operations.changes.all",
  "operations.changes.c:c-api",
  "operations.changes.r:r-site",
  "operations.entity.releases.c:c-api",
  "operations.entity.releases.r:r-site",
  "operations.entity.incidents.r-shop",
  "operations.entity.incidents.r-site",
];

beforeEach(() => {
  sessionStorage.clear();
  localStorage.clear();
  for (const key of CACHE_KEYS) writeCache(key, undefined);
  getProjectsOverview.mockReset().mockResolvedValue(overview);
  listIncidents.mockReset().mockResolvedValue({ incidents: [], count: 0 });
  listAllReleases.mockReset().mockImplementation((opts: { repositoryId?: string; componentId?: string }) => {
    const list = FEED.filter(
      (r) => (!opts.repositoryId || r.repository_id === opts.repositoryId) && (!opts.componentId || r.component_id === opts.componentId),
    );
    return Promise.resolve({ releases: list });
  });
  getRelease.mockReset().mockImplementation((id: string) => {
    const found = FEED.find((r) => r.id === id);
    return found ? Promise.resolve(found) : Promise.reject(new ApiError("release not found", 404));
  });
  rollbackRelease.mockReset();
  deployRelease.mockReset();
  finishRelease.mockReset();
  getDeployMatrix.mockReset().mockResolvedValue({ repos: [], envs: [] });
});

afterEach(() => {
  vi.useRealTimers();
});

const feedCalls = () => listAllReleases.mock.calls.filter(([opts]) => (opts.statuses ?? []).includes("pending"));
const pagedCalls = () => feedCalls().filter(([opts]) => (opts.statuses ?? []).includes("released"));

async function changeRows() {
  const table = await screen.findByRole("table", { name: "Changes" });
  await waitFor(() => expect(within(table).getAllByRole("link", { name: /^Open change/ }).length).toBeGreaterThan(0));
  return within(table)
    .getAllByRole("row")
    .filter((r) => within(r).queryByRole("link", { name: /^Open change/ }));
}

describe("OpsChangesPage", () => {
  it("lists the window's production changes by day, newest first, with their facts", async () => {
    renderAt(`/operations/changes?${RANGE}`);
    const rows = await changeRows();
    expect(rows.map((r) => within(r).getByRole("link", { name: /^Open change/ }).textContent)).toEqual(["site-9", "2.0.0", "1.9.0"]);
    expect(pagedCalls()[0][0]).toEqual({ statuses: CHANGE_STATUSES, limit: 100, before: new Date(TO).toISOString() });

    const table = screen.getByRole("table", { name: "Changes" });
    expect(table.querySelectorAll("tbody")).toHaveLength(2);
    expect(screen.queryByText("2.1.0-rc")).not.toBeInTheDocument();

    const [site, api, broken] = rows;
    expect(within(site).getByText("Verifying")).toBeInTheDocument();
    expect(within(site).getByText("Not checked yet")).toBeInTheDocument();
    expect(within(api).getByText("Released")).toBeInTheDocument();
    expect(within(api).getByRole("link", { name: "api" })).toHaveAttribute("href", `/operations/entity/c-api/summary?${RANGE}`);
    expect(within(api).getByRole("link", { name: "Commit abcdef1 on GitHub" })).toHaveAttribute(
      "href",
      "https://github.com/acme/shop/commit/abcdef1234567",
    );
    expect(within(api).getByRole("link", { name: "SHOP-1" })).toHaveAttribute("href", "/board?task=t-1");
    expect(within(api).getByText("Health 2/2")).toBeInTheDocument();
    expect(within(api).getByText("Smoke 1/1")).toBeInTheDocument();
    expect(within(api).getByText("22m 0s")).toBeInTheDocument();
    expect(within(broken).getByText("Failed")).toBeInTheDocument();
    expect(within(broken).getByText("2 new errors")).toBeInTheDocument();
    expect(within(broken).getByText("Stopped early")).toBeInTheDocument();
  });

  it("filters by status and search through the URL", async () => {
    renderAt(`/operations/changes?${RANGE}`);
    await changeRows();
    fireEvent.click(screen.getByRole("button", { name: /^Failed/ }));
    await waitFor(() => expect(screen.getByTestId("where")).toHaveTextContent("status=failed"));
    expect((await changeRows()).map((r) => within(r).getByRole("link", { name: /^Open change/ }).textContent)).toEqual(["1.9.0"]);

    fireEvent.click(screen.getByRole("button", { name: /^All/ }));
    fireEvent.change(screen.getByRole("searchbox", { name: "Search changes" }), { target: { value: "shop-1" } });
    await waitFor(() => expect(screen.getByTestId("where")).toHaveTextContent("q=shop-1"));
    expect((await changeRows()).map((r) => within(r).getByRole("link", { name: /^Open change/ }).textContent)).toEqual(["2.0.0"]);
  });

  it("opens a shared ?release= link even when the list does not hold that change", async () => {
    const older = release("0.9.0", "r-shop", { id: "rel-090", component_id: "c-api", status: "rolled_back", deployed_at: "2026-09-01T10:00:00Z" });
    getRelease.mockImplementation((id: string) => (id === "rel-090" ? Promise.resolve(older) : Promise.reject(new Error("no"))));
    renderAt(`/operations/changes?${RANGE}&release=rel-090`);
    const panel = await screen.findByRole("dialog", { name: "0.9.0" });
    expect(getRelease).toHaveBeenCalledWith("rel-090");
    expect(within(panel).getAllByText("Rolled back", { selector: "div" }).length).toBeGreaterThan(0);
    expect(within(panel).getByText("Nothing to do on this change right now.")).toBeInTheDocument();
    expect(within(panel).queryByRole("button", { name: "Roll back" })).not.toBeInTheDocument();
    fireEvent.keyDown(panel, { key: "Escape" });
    await waitFor(() => expect(screen.getByTestId("where")).not.toHaveTextContent("release="));
  });

  it("opens a change from its row", async () => {
    renderAt(`/operations/changes?${RANGE}`);
    const [, api] = await changeRows();
    fireEvent.click(within(api).getByRole("link", { name: "Open change 2.0.0" }));
    await waitFor(() => expect(screen.getByTestId("where")).toHaveTextContent("release=rel-200"));
    const panel = await screen.findByRole("dialog", { name: "2.0.0" });
    expect(within(panel).getByRole("region", { name: "Verification" })).toBeInTheDocument();
    expect(within(panel).getByText("41 ms")).toBeInTheDocument();
  });

  it("rolls a production release back only once the repository's name and a reason are typed", async () => {
    const live = release("3.0.0", "r-shop", {
      id: "rel-300",
      component_id: "c-api",
      status: "released",
      deployed_at: new Date(Date.now() - 2 * 3_600_000).toISOString(),
      finished_at: new Date(Date.now() - 3_600_000).toISOString(),
    });
    getRelease.mockResolvedValue(live);
    rollbackRelease.mockResolvedValue({ ...live, status: "rolling_back", updated_at: new Date().toISOString() });
    renderAt(`/operations/changes?${RANGE}&release=rel-300`);

    const panel = await screen.findByRole("dialog", { name: "3.0.0" });
    fireEvent.click(within(panel).getByRole("button", { name: "Roll back" }));
    const confirm = await screen.findByRole("dialog", { name: "Roll back 3.0.0?" });
    const go = within(confirm).getByRole("button", { name: "Roll back" });
    expect(go).toBeDisabled();

    fireEvent.change(within(confirm).getByLabelText("Why roll back?"), { target: { value: "checkout broke" } });
    expect(go).toBeDisabled();
    fireEvent.change(within(confirm).getByLabelText("Type shop to confirm"), { target: { value: "sho" } });
    expect(go).toBeDisabled();
    fireEvent.change(within(confirm).getByLabelText("Type shop to confirm"), { target: { value: "shop" } });
    expect(go).toBeEnabled();

    await act(async () => {
      fireEvent.click(go);
    });
    expect(rollbackRelease).toHaveBeenCalledWith("rel-300", "shop", "checkout broke");
    await waitFor(() => expect(screen.queryByRole("dialog", { name: "Roll back 3.0.0?" })).not.toBeInTheDocument());
    const updated = await screen.findByRole("dialog", { name: "3.0.0" });
    await waitFor(() => expect(within(updated).getAllByText("Rolling back", { selector: "div" }).length).toBeGreaterThan(0));
  });

  it("shows the server's refusal as it said it", async () => {
    const live = release("3.0.0", "r-shop", { id: "rel-300", component_id: "c-api", status: "awaiting_verdict" });
    getRelease.mockResolvedValue(live);
    rollbackRelease.mockRejectedValue(new ApiError("rollback is not allowed: a newer release 3.1.0 already shipped", 409));
    renderAt(`/operations/changes?${RANGE}&release=rel-300`);

    const panel = await screen.findByRole("dialog", { name: "3.0.0" });
    fireEvent.click(within(panel).getByRole("button", { name: "Roll back" }));
    const confirm = await screen.findByRole("dialog", { name: "Roll back 3.0.0?" });
    fireEvent.change(within(confirm).getByLabelText("Why roll back?"), { target: { value: "errors" } });
    fireEvent.change(within(confirm).getByLabelText("Type shop to confirm"), { target: { value: "shop" } });
    await act(async () => {
      fireEvent.click(within(confirm).getByRole("button", { name: "Roll back" }));
    });
    const kept = screen.getByRole("dialog", { name: "Roll back 3.0.0?" });
    expect(await within(kept).findByText("The server refused this")).toBeInTheDocument();
    expect(within(kept).getByText("rollback is not allowed: a newer release 3.1.0 already shipped")).toBeInTheDocument();
    expect(within(kept).getByLabelText("Why roll back?")).toHaveValue("errors");

    fireEvent.click(within(kept).getByRole("button", { name: "Cancel" }));
    const again = await screen.findByRole("dialog", { name: "3.0.0" });
    expect(await within(again).findByText("The server refused this")).toBeInTheDocument();
  });

  it("asks for the typed phrase outside production too, and sends what was typed", async () => {
    const stage = release("2.1.0-rc", "r-shop", {
      id: "rel-rc",
      component_id: "c-api",
      status: "failed",
      deploy: { env: "stage" } as Release["deploy"],
    });
    getRelease.mockResolvedValue(stage);
    rollbackRelease.mockResolvedValue({ ...stage, status: "rolling_back" });
    renderAt(`/operations/changes?${RANGE}&env=staging&release=rel-rc`);

    const panel = await screen.findByRole("dialog", { name: "2.1.0-rc" });
    fireEvent.click(within(panel).getByRole("button", { name: "Roll back" }));
    const confirm = await screen.findByRole("dialog", { name: "Roll back 2.1.0-rc?" });
    fireEvent.change(within(confirm).getByLabelText("Why roll back?"), { target: { value: "bad build" } });
    const go = within(confirm).getByRole("button", { name: "Roll back" });
    expect(go).toBeDisabled();
    fireEvent.change(within(confirm).getByLabelText("Type shop to confirm"), { target: { value: " shop " } });
    expect(go).toBeEnabled();
    await act(async () => {
      fireEvent.click(go);
    });
    expect(rollbackRelease).toHaveBeenCalledWith("rel-rc", " shop ", "bad build");
  });

  it("offers Deploy on a pending dispatch release", async () => {
    const pending = release("2.2.0", "r-shop", { id: "rel-220", component_id: "c-api", status: "pending", mode: "dispatch" });
    getRelease.mockResolvedValue(pending);
    deployRelease.mockResolvedValue({ ...pending, status: "deploying" });
    renderAt(`/operations/changes?${RANGE}&release=rel-220`);
    const panel = await screen.findByRole("dialog", { name: "2.2.0" });
    expect(within(panel).queryByRole("button", { name: "Roll back" })).not.toBeInTheDocument();
    fireEvent.click(within(panel).getByRole("button", { name: "Deploy" }));
    const confirm = await screen.findByRole("dialog", { name: "Deploy 2.2.0?" });
    fireEvent.change(within(confirm).getByLabelText("Type shop to confirm"), { target: { value: "shop" } });
    await act(async () => {
      fireEvent.click(within(confirm).getByRole("button", { name: "Deploy" }));
    });
    expect(deployRelease).toHaveBeenCalledWith("rel-220", "shop");
  });
});

describe("EntityChanges", () => {
  it("reads a component's changes by component, without the entity column", async () => {
    renderAt(`/operations/entity/c-api/changes?${RANGE}`);
    const rows = await changeRows();
    expect(rows.map((r) => within(r).getByRole("link", { name: /^Open change/ }).textContent)).toEqual(["2.0.0", "1.9.0"]);
    expect(feedCalls().some(([opts]) => opts.componentId === "c-api")).toBe(true);
    const table = screen.getByRole("table", { name: "Changes" });
    expect(within(table).queryByRole("columnheader", { name: "Entity" })).not.toBeInTheDocument();
  });

  it("reads a repository's only component by repository", async () => {
    renderAt(`/operations/entity/c-site/changes?${RANGE}`);
    const rows = await changeRows();
    expect(rows.map((r) => within(r).getByRole("link", { name: /^Open change/ }).textContent)).toEqual(["site-9"]);
    expect(feedCalls().some(([opts]) => opts.repositoryId === "r-site" && !opts.componentId)).toBe(true);
  });

  it("polls every 15 s while a listed change is moving", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    renderAt(`/operations/entity/c-site/changes?${RANGE}`);
    await changeRows();
    const before = feedCalls().length;
    await act(async () => {
      vi.advanceTimersByTime(15_500);
    });
    await waitFor(() => expect(feedCalls().length).toBeGreaterThan(before));
  });

  it("slows down to once a minute when nothing is moving", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    renderAt(`/operations/entity/c-api/changes?${RANGE}`);
    await changeRows();
    const before = feedCalls().length;
    await act(async () => {
      vi.advanceTimersByTime(15_500);
    });
    expect(feedCalls().length).toBe(before);
  });
});
