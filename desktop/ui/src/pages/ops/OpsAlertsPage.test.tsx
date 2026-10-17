import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter, Route, Routes, useLocation } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { AlertRule } from "@/api";
import { I18nProvider } from "@/hooks/useI18n";
import { writeCache } from "@/lib/uiCache";
import { opsOverview } from "@/test/opsConsoleFixtures";

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

const apiMock = vi.hoisted(() => ({
  getProjectsOverview: vi.fn(),
  listAlertRules: vi.fn(),
  updateAlertRule: vi.fn(),
  deleteAlertRule: vi.fn(),
  listAlertRuleEvents: vi.fn(),
  getAlertRule: vi.fn(),
  getComponentSeries: vi.fn(),
  listSignals: vi.fn(),
  listRecipes: vi.fn(),
  backtestAlertRule: vi.fn(),
  listNotificationChannels: vi.fn(),
  getComponentSignals: vi.fn(),
  listDataSources: vi.fn(),
  listDataSourceKinds: vi.fn(),
  createAlertRule: vi.fn(),
}));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return { ...actual, api: { ...actual.api, ...apiMock } };
});

import { OpsShell } from "@/components/operations/shell/OpsShell";
import OpsAlertsPage from "@/pages/ops/OpsAlertsPage";
import OpsEntityPage from "@/pages/ops/OpsEntityPage";
import EntityAlerts from "@/pages/ops/entity/EntityAlerts";
import { EntitySubRoute } from "@/pages/ops/entity/EntitySubRoute";

function rule(id: string, overrides: Partial<AlertRule> = {}): AlertRule {
  return {
    id,
    name: id,
    component_id: "c-api",
    environment: "production",
    signal: "http.latency",
    data_source_id: null,
    query: "",
    condition: { op: "gt", threshold: 800, for_seconds: 600, window_seconds: 300 },
    severity: "high",
    channels: ["desktop"],
    open_incident: true,
    enabled: true,
    state: "ok",
    state_since: "2026-10-15T10:00:00Z",
    last_value: 420,
    last_evaluated_at: new Date(Date.now() - 60_000).toISOString(),
    created_at: "2026-10-15T09:00:00Z",
    updated_at: "2026-10-15T09:00:00Z",
    ...overrides,
  };
}

const RULES = [
  rule("Slow checkout", { state: "firing", last_value: 1200 }),
  rule("Errors up", { signal: "http.error_ratio", state: "pending", condition: { op: "change_pct_gt", threshold: 50, for_seconds: 0, window_seconds: 600 }, last_value: 75 }),
  rule("Web vitals", { component_id: "c-web", signal: "web.lcp", condition: { op: "gt", threshold: 2500, for_seconds: 900, window_seconds: 300 } }),
  rule("Staging latency", { environment: "staging", state: "firing" }),
];

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
            <Route path="alerts" element={<OpsAlertsPage />} />
            <Route path="entity/:componentId" element={<OpsEntityPage />}>
              <Route path="alerts" element={<EntitySubRoute tab="alerts" page={EntityAlerts} />} />
            </Route>
          </Route>
        </Routes>
        <Where />
      </I18nProvider>
    </MemoryRouter>,
  );
}

async function rows() {
  const table = await screen.findByRole("table", { name: "Alert rules" });
  return within(table)
    .getAllByRole("row")
    .slice(1)
    .map((r) => within(r).getAllByRole("cell")[1].textContent);
}

beforeEach(() => {
  sessionStorage.clear();
  localStorage.clear();
  for (const key of ["operations.entities.overview", "operations.alertRules.all", "operations.sources.recipes", "operations.sources.signals", "operations.notificationChannels", "operations.alertRules.c-api"]) {
    writeCache(key, undefined);
  }
  Object.values(apiMock).forEach((fn) => fn.mockReset());
  apiMock.getProjectsOverview.mockResolvedValue(opsOverview);
  apiMock.listAlertRules.mockImplementation((opts: { componentId?: string }) =>
    Promise.resolve({ rules: RULES.filter((r) => !opts.componentId || r.component_id === opts.componentId) }),
  );
  apiMock.listSignals.mockResolvedValue({
    signals: [
      { id: "http.latency", kind: "metric", unit: "ms", roles: ["backend"], aggregation: "quantile" },
      { id: "http.error_ratio", kind: "metric", unit: "%", roles: ["backend"], aggregation: "ratio" },
      { id: "web.lcp", kind: "metric", unit: "ms", roles: ["frontend"], aggregation: "quantile" },
    ],
  });
  apiMock.listRecipes.mockResolvedValue({ recipes: [] });
  apiMock.listAlertRuleEvents.mockResolvedValue({
    events: [
      { at: "2026-10-15T10:00:00Z", from_state: "ok", to_state: "pending", value: 900 },
      { at: "2026-10-15T10:10:00Z", from_state: "pending", to_state: "firing", value: 1200, incident_id: "inc-9" },
    ],
  });
  apiMock.getComponentSeries.mockResolvedValue({ from: "", to: "", step_seconds: 60, signals: [] });
  apiMock.listNotificationChannels.mockResolvedValue({
    channels: [
      { kind: "desktop", available: true },
      { kind: "slack", available: false },
    ],
  });
  apiMock.listDataSources.mockResolvedValue({ data_sources: [] });
  apiMock.listDataSourceKinds.mockResolvedValue({ kinds: [] });
});

describe("OpsAlertsPage", () => {
  it("lists the environment's rules worst first, the condition in words, with states as filters", async () => {
    renderAt("/operations/alerts");
    await waitFor(async () => expect(await rows()).toEqual(["Slow checkout", "Errors up", "Web vitals"]));
    expect(apiMock.listAlertRules).toHaveBeenCalledWith({ componentId: undefined });

    const table = screen.getByRole("table", { name: "Alert rules" });
    expect(within(table).getByText("Response time (p95) > 800 ms for 10 min")).toBeInTheDocument();
    expect(within(table).getByText("Error rate rises by more than 50% over 10 min")).toBeInTheDocument();
    expect(within(table).getByText("1.2 s")).toBeInTheDocument();
    expect(within(table).getByText("+75%")).toBeInTheDocument();
    expect(within(table).getByRole("link", { name: "web" })).toHaveAttribute("href", "/operations/entity/c-web/alerts");

    const strip = screen.getByRole("group", { name: "Filter by state" });
    const firing = within(strip).getByRole("button", { name: /Firing/ });
    expect(firing).toHaveTextContent("1");
    fireEvent.click(firing);
    await waitFor(async () => expect(await rows()).toEqual(["Slow checkout"]));
    expect(screen.getByTestId("where")).toHaveTextContent("state=firing");

    fireEvent.click(firing);
    fireEvent.change(screen.getByRole("searchbox", { name: "Search alert rules" }), { target: { value: "lcp" } });
    await waitFor(async () => expect(await rows()).toEqual(["Web vitals"]));
  });

  it("switches a rule off in place and keeps the server's answer", async () => {
    apiMock.updateAlertRule.mockImplementation((id: string, body: Partial<AlertRule>) =>
      Promise.resolve({ ...RULES.find((r) => r.id === id)!, ...body }),
    );
    renderAt("/operations/alerts");
    const toggle = await screen.findByRole("switch", { name: "Evaluate Errors up" });
    fireEvent.click(toggle);
    await waitFor(() => expect(apiMock.updateAlertRule).toHaveBeenCalledWith("Errors up", { enabled: false }));
    await waitFor(() => expect(screen.getByRole("switch", { name: "Evaluate Errors up" })).not.toBeChecked());
  });

  it("opens a rule from its link with its state changes, and deletes it once confirmed", async () => {
    apiMock.deleteAlertRule.mockResolvedValue(undefined);
    renderAt("/operations/alerts?rule=Slow%20checkout");
    const panel = await screen.findByRole("dialog");
    expect(within(panel).getByRole("heading", { name: "Slow checkout" })).toBeInTheDocument();
    await waitFor(() => expect(apiMock.listAlertRuleEvents).toHaveBeenCalledWith("Slow checkout", 100));
    const timeline = await within(panel).findByRole("list", { name: "State changes" });
    expect(within(timeline).getAllByRole("listitem")).toHaveLength(2);
    expect(within(timeline).getByRole("link", { name: "Open the issue" })).toHaveAttribute("href", "/operations/issues?id=inc-9");
    expect(apiMock.getComponentSeries).toHaveBeenCalledWith("c-api", expect.objectContaining({ env: "production", signals: ["http.latency"] }));

    fireEvent.click(within(panel).getByRole("button", { name: "Delete" }));
    const confirm = await screen.findByRole("alertdialog").catch(() => screen.getAllByRole("dialog").at(-1)!);
    fireEvent.click(within(confirm).getByRole("button", { name: "Delete" }));
    await waitFor(() => expect(apiMock.deleteAlertRule).toHaveBeenCalledWith("Slow checkout"));
    await waitFor(() => expect(screen.getByTestId("where")).not.toHaveTextContent("rule="));
    await waitFor(async () => expect(await rows()).toEqual(["Errors up", "Web vitals"]));
  });

  it("follows a rule's link into its own environment", async () => {
    renderAt("/operations/alerts?rule=Staging%20latency");
    await waitFor(() => expect(screen.getByTestId("where")).toHaveTextContent("env=staging"));
    const panel = await screen.findByRole("dialog");
    expect(within(panel).getByRole("heading", { name: "Staging latency" })).toBeInTheDocument();
  });

  it("opens a notification's link on the entity's own Alerts, reading a rule the list does not hold", async () => {
    apiMock.getAlertRule.mockResolvedValue(rule("Elsewhere", { component_id: "c-web", name: "Web errors" }));
    renderAt("/operations/entity/c-api/alerts?rule=Elsewhere");
    await waitFor(() => expect(apiMock.listAlertRules).toHaveBeenCalledWith({ componentId: "c-api" }));
    await waitFor(() => expect(apiMock.getAlertRule).toHaveBeenCalledWith("Elsewhere"));
    const panel = await screen.findByRole("dialog");
    expect(await within(panel).findByRole("heading", { name: "Web errors" })).toBeInTheDocument();
  });

  it("opens a notification's link on a library's own Alerts: every role has the section", async () => {
    apiMock.listAlertRules.mockResolvedValue({
      rules: [rule("Lib rule", { component_id: "c-lib", name: "Bundle errors", signal: "", data_source_id: "ds-1", query: "up" })],
    });
    renderAt("/operations/entity/c-lib/alerts?rule=Lib%20rule");
    const panel = await screen.findByRole("dialog");
    expect(await within(panel).findByRole("heading", { name: "Bundle errors" })).toBeInTheDocument();
    expect(screen.getByTestId("where")).toHaveTextContent("/operations/entity/c-lib/alerts?rule=Lib%20rule");
  });

  it("charts a web vital at p75 from the series read, with no replay, and keeps the console's window on the issue link", async () => {
    const at = Date.now() - 30 * 60_000;
    apiMock.getComponentSeries.mockResolvedValue({
      from: "",
      to: "",
      step_seconds: 60,
      signals: [
        {
          signal: "web.lcp",
          unit: "ms",
          bound: true,
          series: ["p50", "p75", "p95", "p99"].map((name, i) => ({ name, labels: {}, points: [[at, 1000 * (i + 1)]] })),
          previous: [],
          error: null,
        },
      ],
    });
    apiMock.listAlertRuleEvents.mockResolvedValue({
      events: [{ at: new Date(at).toISOString(), from_state: "ok", to_state: "firing", value: 2600, incident_id: "inc-7" }],
    });
    renderAt("/operations/alerts?rule=Web%20vitals&range=6h");
    const panel = await screen.findByRole("dialog");
    await waitFor(() => expect(apiMock.getComponentSeries).toHaveBeenCalledWith("c-web", expect.objectContaining({ signals: ["web.lcp"] })));
    expect(apiMock.backtestAlertRule).not.toHaveBeenCalled();
    expect(await within(panel).findByRole("link", { name: "Open the issue" })).toHaveAttribute("href", "/operations/issues?id=inc-7&range=6h");
  });

  it("says a rule whose reads fail held its state, in the table and the panel", async () => {
    apiMock.listAlertRules.mockResolvedValue({
      rules: [rule("Flaky", { error_count: 4, last_error: "prometheus: connection refused" }), rule("Stale", { error_count: 12, last_error: "" })],
    });
    renderAt("/operations/alerts?rule=Flaky");
    const table = await screen.findByRole("table", { name: "Alert rules", hidden: true });
    expect(within(table).getByText("The last 4 reads failed: prometheus: connection refused")).toBeInTheDocument();
    expect(within(table).getByText("The last 12 reads failed: —")).toBeInTheDocument();
    const panel = await screen.findByRole("dialog");
    expect(within(panel).getByText("The signal can't be read")).toBeInTheDocument();
    expect(within(panel).getByText("It held its state for 4 evaluations; after 10 it reads as no data.")).toBeInTheDocument();
    expect(within(panel).getByText("prometheus: connection refused")).toBeInTheDocument();
  });

  it("shows a live broker signal's value in the panel, not a one-point chart", async () => {
    apiMock.listAlertRules.mockResolvedValue({
      rules: [rule("Backlog", { component_id: "c-api", signal: "queue.backlog", condition: { op: "gt", threshold: 100, for_seconds: 0, window_seconds: 300 } })],
    });
    apiMock.getComponentSeries.mockResolvedValue({
      from: "",
      to: "",
      step_seconds: 60,
      signals: [{ signal: "queue.backlog", unit: "messages", bound: true, live: true, series: [{ name: "lag", labels: {}, points: [[Date.now(), 42]] }], previous: [], error: null }],
    });
    renderAt("/operations/alerts?rule=Backlog");
    const panel = await screen.findByRole("dialog");
    expect(await within(panel).findByText("42 messages")).toBeInTheDocument();
    expect(within(panel).getByText(/Live value — connect an exporter-backed source for history/)).toBeInTheDocument();
  });

  it("shows a channel the rule names that does not deliver yet", async () => {
    apiMock.listAlertRules.mockResolvedValue({ rules: [rule("Paged", { channels: ["desktop", "slack"] })] });
    renderAt("/operations/alerts?rule=Paged");
    const panel = await screen.findByRole("dialog");
    expect(await within(panel).findByText("Desktop notification, Slack (doesn't deliver yet)")).toBeInTheDocument();
  });

  it("takes the console to the environment a new rule was saved in", async () => {
    apiMock.listAlertRules.mockResolvedValue({ rules: [] });
    apiMock.getComponentSignals.mockResolvedValue({
      component_id: "c-api",
      role: "backend",
      environment: "staging",
      signals: [
        {
          signal: "http.latency",
          kind: "metric",
          unit: "ms",
          golden: true,
          status: "bound",
          binding: { id: "b-1", signal: "http.latency" },
          suggestions: [],
        },
      ],
    });
    apiMock.createAlertRule.mockImplementation((body: Partial<AlertRule>) => Promise.resolve(rule("ar-new", { ...body, id: "ar-new" } as Partial<AlertRule>)));
    renderAt("/operations/entity/c-api/alerts");
    fireEvent.click((await screen.findAllByRole("button", { name: "New rule" }))[0]);
    const editor = await screen.findByRole("dialog");
    fireEvent.click(within(editor).getByLabelText("Environment"));
    fireEvent.click(screen.getByRole("option", { name: "Staging" }));
    await waitFor(() => expect(apiMock.getComponentSignals).toHaveBeenCalledWith("c-api", "staging"));
    await waitFor(() => expect(within(editor).getByLabelText(/^Signal$/)).toBeEnabled());
    fireEvent.click(within(editor).getByLabelText(/^Signal$/));
    fireEvent.click(screen.getByRole("option", { name: /Response time/ }));
    fireEvent.change(within(editor).getByLabelText("Threshold"), { target: { value: "800" } });
    fireEvent.click(within(editor).getByRole("button", { name: "Save rule" }));
    await waitFor(() => expect(apiMock.createAlertRule).toHaveBeenCalledWith(expect.objectContaining({ environment: "staging" })));
    await waitFor(() => expect(screen.getByTestId("where")).toHaveTextContent("env=staging"));
  });
});
