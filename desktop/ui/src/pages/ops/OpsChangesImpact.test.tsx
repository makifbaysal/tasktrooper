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
  dismissRollbackProposal: vi.fn(),
  getDeployMatrix: vi.fn(),
  getIncident: vi.fn(),
  listSignals: vi.fn(),
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
  rollback_proposal: { proposed_at: "2026-10-15T10:31:00Z", by: "agent", reason: "verify_failed", note: "Error rate +3.2 pt past the 2 pt limit", incident_id: null },
  rollback_allowed: true,
  rollback_refusal: "",
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
  tasks: [{ id: "t-checkout", key: "SHOP-12", title: "Checkout rewrite" } as Release["tasks"][number]],
  rollback_proposal: { proposed_at: new Date(Date.now() - 50 * 60_000).toISOString(), by: "agent", reason: "health_incident", note: "Checkout errors since 2.8.0", incident_id: null },
  rollback_allowed: false,
  rollback_refusal: "newer_shipped",
  deployed_at: "2026-10-15T07:50:00Z",
  finished_at: new Date(Date.now() - 60 * 60_000).toISOString(),
});
// Failed, with failing checks and auto rollback off: still nothing proposed.
const failed = release("2.7.0", "r-shop", {
  id: "rel-failed",
  component_id: "c-api",
  status: "failed",
  profile: off,
  failure_reason: "deploy failed",
  rollback_allowed: true,
  checks: { early_stop: "smoke failed" },
});
// The soak's check signals carry no unit: the catalog's says this one is a ratio.
const unthresholded = release("2.6.0", "r-shop", {
  id: "rel-ratio",
  component_id: "c-api",
  status: "released",
  profile: off,
  deployed_at: "2026-10-15T06:00:00Z",
  checks: {
    impact: {
      checked_at: "2026-10-15T07:00:00Z",
      pending: false,
      verdict: "worse",
      signals: [
        { signal: "http.error_ratio", before: 0.4, after: 1.4, delta: 1, delta_pct: 250, verdict: "worse" },
        { signal: "http.requests", before: 100, after: 99, delta: -1, delta_pct: -1, verdict: "neutral" },
      ],
    },
  },
});
const FEED = [bad, measuring, shipped, failed, unthresholded];

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
  "operations.sources.signals",
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
  mocks.listSignals.mockResolvedValue({
    signals: [
      { id: "http.error_ratio", kind: "metric", unit: "%", roles: ["backend"], aggregation: "ratio", better: "lower" },
      { id: "http.latency", kind: "metric", unit: "ms", roles: ["backend"], aggregation: "quantile", better: "lower" },
    ],
  });
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
    // Worded in the catalog's unit: percentage points for a ratio, never its raw +250%.
    await waitFor(() => expect(mocks.listSignals).toHaveBeenCalled());
    expect(await within(row("2.6.0")).findByText("· Error rate +1 pt")).toBeInTheDocument();
    expect(within(row("2.6.0")).queryByText(/\+250%/)).not.toBeInTheDocument();
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

  it("reads a threshold whose signal never settled as not checked, not passed", async () => {
    const settled = {
      ...bad,
      checks: {
        impact: {
          checked_at: "2026-10-15T10:30:00Z",
          pending: false,
          verdict: "neutral" as const,
          signals: [{ signal: "http.error_ratio", before: 0.4, after: 0.6, delta: 0.2, delta_pct: 50, verdict: "neutral" as const, max_delta: 2, settling: true }],
          unchecked: ["max_error_rate_delta", "max_p95_delta_pct"],
        },
      },
    };
    mocks.getRelease.mockResolvedValue(settled);
    renderAt(`/operations/changes?${RANGE}&release=rel-bad`);
    const panel = await screen.findByRole("dialog");
    const section = await within(panel).findByRole("region", { name: "Impact" });
    const table = await within(section).findByRole("table", { name: "Golden signals before and after the deploy" });
    const errorRow = within(table).getByRole("rowheader", { name: /Error rate/ }).closest("tr")!;
    expect(within(errorRow).getByText("Not checked — still settling")).toBeInTheDocument();
    expect(within(errorRow).queryByText("Breached")).not.toBeInTheDocument();
    expect(within(section).getByText("Not checked, because the signal was still settling when the soak ended: error rate threshold")).toBeInTheDocument();
    expect(within(section).getByText("Not checked, because the signal is not bound or not readable: p95 latency threshold")).toBeInTheDocument();
  });

  it("says a rollback can't run on a server with no git reverter", async () => {
    mocks.getRelease.mockResolvedValue({ ...shipped, rollback_refusal: "not_configured" });
    renderAt(`/operations/changes?${RANGE}&release=rel-shipped`);
    const panel = await screen.findByRole("dialog");
    expect(await within(panel).findByText(/this server has no git access to revert the release's merges/)).toBeInTheDocument();
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
    expect(within(dialog).getByLabelText("Why roll back?")).toHaveValue("Error rate +3.2 pt past the 2 pt limit");
    fireEvent.change(within(dialog).getByRole("textbox", { name: /shop/ }), { target: { value: "shop" } });
    expect(confirm).toBeEnabled();
    fireEvent.click(confirm);
    await waitFor(() => expect(mocks.rollbackRelease).toHaveBeenCalledWith("rel-bad", "shop", "Error rate +3.2 pt past the 2 pt limit"));
  });

  it("says why a proposed rollback cannot run now, offers no approval, and dismisses it", async () => {
    mocks.dismissRollbackProposal.mockResolvedValue({
      ...shipped,
      rollback_proposal: null,
      rollback_dismissal: { dismissed_at: new Date(Date.now() - 60_000).toISOString(), incident_id: null },
    });
    renderAt(`/operations/changes?${RANGE}&release=rel-shipped`);
    const panel = await screen.findByRole("dialog");
    expect(await within(panel).findByText("Rollback suggested · 2.8.0")).toBeInTheDocument();
    expect(within(panel).getByText("Checkout errors since 2.8.0")).toBeInTheDocument();
    expect(within(panel).getByText("A health incident")).toBeInTheDocument();
    expect(within(panel).getByText("Proposed by Release engineer", { exact: false })).toBeInTheDocument();
    expect(within(panel).getByText("It can't be rolled back now: a newer release of this component has shipped since.")).toBeInTheDocument();
    expect(within(panel).queryByRole("button", { name: "Approve rollback" })).not.toBeInTheDocument();
    fireEvent.click(within(panel).getByRole("button", { name: "Dismiss suggestion" }));
    await waitFor(() => expect(mocks.dismissRollbackProposal).toHaveBeenCalledWith("rel-shipped"));
    await waitFor(() => expect(within(panel).queryByText("Rollback suggested · 2.8.0")).not.toBeInTheDocument());
    expect(within(panel).getByText("Rollback suggestion dismissed 1 minute ago")).toBeInTheDocument();
  });

  it("suggests no rollback the server did not propose", async () => {
    renderAt(`/operations/changes?${RANGE}&release=rel-failed`);
    const panel = await screen.findByRole("dialog");
    expect(await within(panel).findByText("deploy failed")).toBeInTheDocument();
    expect(within(panel).queryByText(/Rollback suggested/)).not.toBeInTheDocument();
  });

  it("shows the proposal on the issue the server attributed it to, never by repository alone", async () => {
    const onset = new Date(Date.parse(shipped.finished_at!) + 5 * 60_000).toISOString();
    const attributed = incident("i-1", "r-shop", "critical", { title: "Checkout is down", first_seen_at: onset, last_seen_at: onset, task_id: "t-checkout" });
    const named = incident("i-2", "r-shop", "high", { title: "Errors spiking", first_seen_at: onset, last_seen_at: onset });
    const bystander = incident("i-3", "r-shop", "high", { title: "Disk almost full", first_seen_at: onset, last_seen_at: onset });
    const proposedFor = { ...bad, rollback_proposal: { ...bad.rollback_proposal!, incident_id: "i-2" } };
    mocks.listAllReleases.mockResolvedValue({ releases: [proposedFor, measuring, shipped, failed] });
    mocks.listIncidents.mockResolvedValue({ incidents: [attributed, named, bystander], count: 3 });

    mocks.getIncident.mockResolvedValue(attributed);
    const first = renderAt("/operations/issues?id=i-1");
    let panel = await screen.findByRole("dialog");
    expect(await within(panel).findByText("Rollback suggested · 2.8.0")).toBeInTheDocument();
    expect(within(panel).getByRole("link", { name: "Open 2.8.0" })).toHaveAttribute("href", "/operations/changes?release=rel-shipped");
    first.unmount();

    mocks.getIncident.mockResolvedValue(named);
    const second = renderAt("/operations/issues?id=i-2");
    panel = await screen.findByRole("dialog");
    expect(await within(panel).findByText("Rollback suggested · 3.0.0")).toBeInTheDocument();
    expect(within(panel).getByRole("button", { name: "Approve rollback" })).toBeInTheDocument();
    second.unmount();

    mocks.getIncident.mockResolvedValue(bystander);
    renderAt("/operations/issues?id=i-3");
    panel = await screen.findByRole("dialog");
    expect(await within(panel).findByText("Disk almost full", { selector: "h2" })).toBeInTheDocument();
    expect(within(panel).queryByText(/Rollback suggested/)).not.toBeInTheDocument();
  });
});
