import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { ApiError, type ProjectsOverview, type Release, type ReleaseImpact } from "@/api";
import { I18nProvider } from "@/hooks/useI18n";
import { writeCache } from "@/lib/uiCache";
import { incident, release, shopRepo } from "@/test/opsConsoleFixtures";

if (!Element.prototype.scrollIntoView) {
  Element.prototype.scrollIntoView = () => {};
}

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

const mocks = vi.hoisted(() => ({
  getProjectsOverview: vi.fn(),
  listIncidents: vi.fn(),
  listAllReleases: vi.fn(),
  getRelease: vi.fn(),
  getReleaseImpact: vi.fn(),
  getComponentSeries: vi.fn(),
  rollbackRelease: vi.fn(),
  getDeployMatrix: vi.fn(),
  getIncident: vi.fn(),
}));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return { ...actual, api: { ...actual.api, ...mocks } };
});

import { OpsShell } from "@/components/operations/shell/OpsShell";
import OpsChangesPage from "@/pages/ops/OpsChangesPage";
import OpsIssuesPage from "@/pages/ops/OpsIssuesPage";

const overview = {
  projects: [{ id: "p-shop", name: "Shop", repositories: [shopRepo] }],
  unassigned: [],
} as unknown as ProjectsOverview;

const FROM = Date.parse("2026-10-15T00:00:00Z");
const TO = Date.parse("2026-10-16T00:00:00Z");
const RANGE = `range=custom&from=${FROM}&to=${TO}`;
const off = { mode: "on_merge", auto_rollback: false, verify: { soak_minutes: 10, max_new_errors: 0, max_error_rate_delta: 2 } } as Release["profile"];

const bad = release("3.0.0", "r-shop", {
  id: "rel-bad",
  component_id: "c-api",
  status: "awaiting_verdict",
  profile: off,
  deployed_at: "2026-10-15T10:00:00Z",
  checks: {
    early_stop: "error rate rose 3.2 points (limit 2)",
    impact: {
      checked_at: "2026-10-15T10:30:00Z",
      pending: false,
      verdict: "worse",
      signals: [
        { signal: "http.error_ratio", before: 0.4, after: 3.6, delta: 3.2, delta_pct: 800, verdict: "worse", max_delta: 2, breached: true },
        { signal: "http.requests", before: 100, after: 98, delta: -2, delta_pct: -2, verdict: "neutral" },
      ],
      unchecked: ["max_p95_delta_pct"],
    },
  },
});
const measuring = release("2.9.0", "r-shop", {
  id: "rel-measuring",
  component_id: "c-api",
  status: "verifying",
  profile: { ...off, auto_rollback: true },
  deployed_at: "2026-10-15T09:00:00Z",
  checks: { impact: { checked_at: "2026-10-15T09:20:00Z", pending: true, verdict: "neutral", signals: [] } },
});
const shipped = release("2.8.0", "r-shop", {
  id: "rel-shipped",
  component_id: "c-api",
  status: "released",
  profile: off,
  deployed_at: "2026-10-15T07:50:00Z",
  finished_at: new Date(Date.now() - 60 * 60_000).toISOString(),
});
const FEED = [bad, measuring, shipped];

const impact: ReleaseImpact = {
  release_id: "rel-bad",
  component_id: "c-api",
  environment: "production",
  deployed_at: "2026-10-15T10:00:00Z",
  window_minutes: 60,
  pending: false,
  verdict: "worse",
  computed_at: "2026-10-15T11:00:00Z",
  signals: [
    { signal: "http.requests", unit: "req/s", before: 100, after: 98, baseline: 101, delta: -2, delta_pct: -2, higher_is_better: true, verdict: "neutral" },
    { signal: "http.error_ratio", unit: "%", before: 0.4, after: 3.6, baseline: 0.5, delta: 3.2, delta_pct: 800, higher_is_better: false, verdict: "worse" },
  ],
};

function renderAt(path: string) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <I18nProvider>
        <Routes>
          <Route path="/operations" element={<OpsShell />}>
            <Route path="changes" element={<OpsChangesPage />} />
            <Route path="issues" element={<OpsIssuesPage />} />
          </Route>
        </Routes>
      </I18nProvider>
    </MemoryRouter>,
  );
}

const CACHE_KEYS = [
  "operations.entities.overview",
  "operations.console.openIncidents",
  "operations.console.shippedReleases",
  "operations.changes.all",
  "operations.releaseImpact.rel-bad",
  "operations.releaseImpact.rel-shipped",
  "operations.issues.all.live",
];

beforeEach(() => {
  sessionStorage.clear();
  localStorage.clear();
  for (const key of CACHE_KEYS) writeCache(key, undefined);
  Object.values(mocks).forEach((fn) => fn.mockReset());
  mocks.getProjectsOverview.mockResolvedValue(overview);
  mocks.listIncidents.mockResolvedValue({ incidents: [], count: 0 });
  mocks.listAllReleases.mockResolvedValue({ releases: FEED });
  mocks.getRelease.mockImplementation((id: string) => {
    const found = FEED.find((r) => r.id === id);
    return found ? Promise.resolve(found) : Promise.reject(new ApiError("release not found", 404));
  });
  mocks.getReleaseImpact.mockResolvedValue(impact);
  mocks.getComponentSeries.mockResolvedValue({ from: "", to: "", step_seconds: 60, signals: [] });
  mocks.getDeployMatrix.mockResolvedValue({ repos: [], envs: [] });
});

describe("Changes impact", () => {
  it("shows each change's verdict and the move that decided it", async () => {
    renderAt(`/operations/changes?${RANGE}`);
    const table = await screen.findByRole("table", { name: "Changes" });
    const row = (version: string) => within(table).getByRole("link", { name: `Open change ${version}` }).closest("tr")!;
    await waitFor(() => expect(within(row("3.0.0")).getByText("Worse")).toBeInTheDocument());
    expect(within(row("3.0.0")).getByText("· Error rate +3.2 pt")).toBeInTheDocument();
    expect(within(row("2.9.0")).getByText("Measuring")).toBeInTheDocument();
    expect(within(row("2.8.0")).queryByText("Worse")).not.toBeInTheDocument();
  });

  it("opens the release with its before and after, held to the profile's thresholds", async () => {
    renderAt(`/operations/changes?${RANGE}&release=rel-bad`);
    const panel = await screen.findByRole("dialog");
    await waitFor(() => expect(mocks.getReleaseImpact).toHaveBeenCalledWith("rel-bad"));
    const section = await within(panel).findByRole("region", { name: "Impact" });
    const table = await within(section).findByRole("table", { name: "Golden signals before and after the deploy" });
    const errorRow = within(table).getByRole("rowheader", { name: /Error rate/ }).closest("tr")!;
    expect(within(errorRow).getByText("0.4%")).toBeInTheDocument();
    expect(within(errorRow).getByText("3.6%")).toBeInTheDocument();
    expect(within(errorRow).getByText("+3.2 pt")).toBeInTheDocument();
    expect(within(errorRow).getByText("Held to ≤ +2 pt")).toBeInTheDocument();
    expect(within(errorRow).getByText("Breached")).toBeInTheDocument();
    expect(within(section).getByText(/Not checked, because the signal is not bound or not readable: p95 latency threshold/)).toBeInTheDocument();

    const at = Date.parse("2026-10-15T10:00:00Z");
    await waitFor(() =>
      expect(mocks.getComponentSeries).toHaveBeenCalledWith(
        "c-api",
        expect.objectContaining({ env: "production", from: at - 3_600_000, to: at + 3_600_000, signals: ["http.requests", "http.error_ratio"] }),
      ),
    );
  });

  it("approves a suggested rollback with the repository's name typed", async () => {
    mocks.rollbackRelease.mockResolvedValue({ ...bad, status: "rolling_back" });
    renderAt(`/operations/changes?${RANGE}&release=rel-bad`);
    const panel = await screen.findByRole("dialog");
    expect(await within(panel).findByText("Rollback suggested · 3.0.0")).toBeInTheDocument();
    fireEvent.click(within(panel).getByRole("button", { name: "Approve rollback" }));

    const dialog = (await screen.findAllByRole("dialog")).find((d) => within(d).queryByText("Roll back 3.0.0?"))!;
    const confirm = within(dialog).getByRole("button", { name: "Approve rollback" });
    expect(confirm).toBeDisabled();
    expect(within(dialog).getByLabelText("Why roll back?")).toHaveValue("error rate rose 3.2 points (limit 2)");
    fireEvent.change(within(dialog).getByRole("textbox", { name: /shop/ }), { target: { value: "shop" } });
    expect(confirm).toBeEnabled();
    fireEvent.click(confirm);
    await waitFor(() => expect(mocks.rollbackRelease).toHaveBeenCalledWith("rel-bad", "shop", "error rate rose 3.2 points (limit 2)"));
  });

  it("offers the rollback on the issue a production release is held responsible for", async () => {
    const onset = new Date(Date.parse(shipped.finished_at!) + 5 * 60_000).toISOString();
    const issue = incident("i-1", "r-shop", "critical", { title: "Checkout is down", first_seen_at: onset, last_seen_at: onset });
    mocks.listIncidents.mockResolvedValue({ incidents: [issue], count: 1 });
    mocks.getIncident.mockResolvedValue(issue);
    renderAt("/operations/issues?id=i-1");
    const panel = await screen.findByRole("dialog");
    expect(await within(panel).findByText("Rollback suggested · 2.8.0")).toBeInTheDocument();
    expect(within(panel).getByText("A production issue began within 15 minutes of this deploy: Checkout is down")).toBeInTheDocument();
    expect(within(panel).getByRole("link", { name: "Open 2.8.0" })).toHaveAttribute("href", "/operations/changes?release=rel-shipped");
  });
});
