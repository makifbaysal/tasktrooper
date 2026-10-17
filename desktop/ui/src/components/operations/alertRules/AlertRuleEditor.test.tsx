import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { ApiError, type AlertRule, type AlertRuleSuggestion, type ComponentSignal } from "@/api";
import { OpsProvider } from "@/components/operations/shell/OpsContext";
import type { OpsEntity } from "@/components/operations/shell/useOpsEntities";
import { I18nProvider } from "@/hooks/useI18n";
import { writeCache } from "@/lib/uiCache";
import { dataSource, prometheusKind } from "@/test/dataSourceFixtures";

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
  getComponentSignals: vi.fn(),
  listDataSources: vi.fn(),
  listDataSourceKinds: vi.fn(),
  createAlertRule: vi.fn(),
  updateAlertRule: vi.fn(),
  backtestAlertRule: vi.fn(),
  suggestAlertRules: vi.fn(),
  listNotificationChannels: vi.fn(),
}));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return { ...actual, api: { ...actual.api, ...apiMock } };
});

import { AlertRuleEditor } from "@/components/operations/alertRules/AlertRuleEditor";

const entity: OpsEntity = {
  id: "c-api",
  name: "api",
  role: "backend",
  repositoryId: "r-shop",
  repositoryName: "shop",
  projectIds: [],
  environments: [],
};

function signal(id: string, unit: string, bound: boolean): ComponentSignal {
  return {
    signal: id,
    kind: "metric",
    unit,
    golden: true,
    status: bound ? "bound" : "unbound",
    binding: bound ? ({ id: `b-${id}`, signal: id } as ComponentSignal["binding"]) : null,
    suggestions: [],
  };
}

function saved(body: Partial<AlertRule>, id = "ar-new"): AlertRule {
  return {
    id,
    state: "ok",
    state_since: "2026-10-16T10:00:00Z",
    created_at: "2026-10-16T10:00:00Z",
    updated_at: "2026-10-16T10:00:00Z",
    ...body,
  } as AlertRule;
}

function suggestion(name: string, sig: string, threshold: number): AlertRuleSuggestion {
  return {
    name,
    component_id: "c-api",
    environment: "production",
    signal: sig,
    data_source_id: null,
    query: "",
    condition: { op: "gt", threshold, for_seconds: 300, window_seconds: 300 },
    severity: "high",
    channels: [],
    open_incident: true,
    enabled: true,
  };
}

function renderEditor(props: Partial<React.ComponentProps<typeof AlertRuleEditor>> = {}) {
  const onSaved = vi.fn();
  const onOpenChange = vi.fn();
  render(
    <MemoryRouter>
      <I18nProvider>
        <OpsProvider>
          <AlertRuleEditor
            open
            onOpenChange={onOpenChange}
            entities={[entity]}
            lockedEntity={entity}
            defaultEnv="production"
            catalogUnits={new Map([["http.latency", "ms"]])}
            onSaved={onSaved}
            {...props}
          />
        </OpsProvider>
      </I18nProvider>
    </MemoryRouter>,
  );
  return { onSaved, onOpenChange };
}

function pick(label: RegExp, option: RegExp) {
  fireEvent.click(screen.getByLabelText(label));
  fireEvent.click(screen.getByRole("option", { name: option }));
}

beforeEach(() => {
  sessionStorage.clear();
  for (const key of ["operations.signals.c-api.production", "operations.sources.kinds", "operations.sources.list", "operations.notificationChannels"]) {
    writeCache(key, undefined);
  }
  Object.values(apiMock).forEach((fn) => fn.mockReset());
  apiMock.getComponentSignals.mockResolvedValue({
    component_id: "c-api",
    role: "backend",
    environment: "production",
    signals: [signal("http.latency", "ms", true), signal("http.error_ratio", "%", true), signal("infra.cpu", "cores", false)],
  });
  apiMock.listDataSources.mockResolvedValue({ data_sources: [dataSource()] });
  apiMock.listDataSourceKinds.mockResolvedValue({ kinds: [prometheusKind] });
  apiMock.createAlertRule.mockImplementation((body: Partial<AlertRule>) => Promise.resolve(saved(body)));
  apiMock.listNotificationChannels.mockResolvedValue({
    channels: [
      { kind: "desktop", available: true },
      { kind: "in_app", available: false },
      { kind: "slack", available: false },
    ],
  });
});

describe("AlertRuleEditor", () => {
  it("offers only the entity's bound signals and checks the draft before any call", async () => {
    renderEditor();
    await waitFor(() => expect(apiMock.getComponentSignals).toHaveBeenCalledWith("c-api", "production"));
    fireEvent.click(screen.getByRole("button", { name: "Save rule" }));
    expect(await screen.findAllByText("Required")).toHaveLength(2);
    expect(apiMock.createAlertRule).not.toHaveBeenCalled();
    expect(screen.getByRole("button", { name: "Run backtest" })).toBeDisabled();

    fireEvent.click(screen.getByLabelText(/^Signal$/));
    expect(screen.getAllByRole("option").map((o) => o.textContent)).toEqual(["Response timehttp.latency", "Error ratehttp.error_ratio"]);
  });

  it("backtests the draft over the days asked, then saves it named after its condition", async () => {
    apiMock.backtestAlertRule.mockResolvedValue({
      fires: [{ start: "2026-10-14T03:00:00Z", end: "2026-10-14T03:40:00Z", peak: 950 }],
      series: [{ name: "http.latency", points: [[Date.parse("2026-10-14T03:00:00Z"), 950]] }],
      step_seconds: 300,
    });
    const { onSaved, onOpenChange } = renderEditor();
    await waitFor(() => expect(apiMock.getComponentSignals).toHaveBeenCalled());
    await waitFor(() => expect(screen.getByLabelText(/^Signal$/)).toBeEnabled());
    pick(/^Signal$/, /Response time/);
    fireEvent.change(screen.getByLabelText("Threshold"), { target: { value: "800" } });
    expect(screen.getByText("Response time (p95) > 800 ms for 5 min")).toBeInTheDocument();

    fireEvent.change(screen.getByLabelText("Days"), { target: { value: "3" } });
    fireEvent.click(screen.getByRole("button", { name: "Run backtest" }));
    await waitFor(() => expect(apiMock.backtestAlertRule).toHaveBeenCalledTimes(1));
    expect(apiMock.backtestAlertRule.mock.calls[0][0]).toMatchObject({
      component_id: "c-api",
      environment: "production",
      signal: "http.latency",
      condition: { op: "gt", threshold: 800, for_seconds: 300, window_seconds: 300 },
      channels: [],
      days: 3,
    });
    expect(await screen.findByText("Times it would have fired in the last 3 days: 1")).toBeInTheDocument();
    const fires = screen.getByRole("list", { name: "When it would have fired" });
    expect(within(fires).getByText("Peak 950 ms")).toBeInTheDocument();

    fireEvent.change(screen.getByLabelText("Threshold"), { target: { value: "900" } });
    expect(screen.getByText("The rule changed since this backtest. Run it again.")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Save rule" }));
    await waitFor(() => expect(apiMock.createAlertRule).toHaveBeenCalledTimes(1));
    expect(apiMock.createAlertRule.mock.calls[0][0]).toMatchObject({
      name: "Response time (p95) > 900 ms for 5 min",
      signal: "http.latency",
      condition: { op: "gt", threshold: 900 },
      severity: "medium",
      open_incident: false,
      channels: ["desktop"],
    });
    expect(apiMock.createAlertRule.mock.calls[0][0]).not.toHaveProperty("data_source_id");
    expect(onSaved).toHaveBeenCalledTimes(1);
    expect(onOpenChange).toHaveBeenCalledWith(false);
  });

  it("shows the server's field errors in place and keeps the dialog", async () => {
    apiMock.createAlertRule.mockRejectedValue(new ApiError("invalid rule", 400, "", { "condition.window_seconds": "too_long" }));
    const { onOpenChange } = renderEditor();
    await waitFor(() => expect(screen.getByLabelText(/^Signal$/)).toBeEnabled());
    pick(/^Signal$/, /Error rate/);
    fireEvent.change(screen.getByLabelText("Threshold"), { target: { value: "2" } });
    fireEvent.click(screen.getByRole("button", { name: "Save rule" }));
    expect(await screen.findByText("Too long")).toBeInTheDocument();
    expect(onOpenChange).not.toHaveBeenCalledWith(false);
  });

  it("fills the form with each suggestion in turn and saves them one by one", async () => {
    apiMock.suggestAlertRules.mockResolvedValue({
      rules: [suggestion("High error ratio", "http.error_ratio", 2), suggestion("Slow responses (p95)", "http.latency", 1000)],
    });
    const { onSaved, onOpenChange } = renderEditor({ suggest: true });
    await waitFor(() => expect(apiMock.suggestAlertRules).toHaveBeenCalledWith("c-api", "production"));
    expect(await screen.findByText("Suggested rules: 2")).toBeInTheDocument();
    expect(screen.getByLabelText("Name")).toHaveValue("High error ratio");
    expect(screen.getByLabelText("Threshold")).toHaveValue("2");
    const desktop = screen.getByRole("checkbox", { name: "Desktop notification" });
    expect(desktop).toBeChecked();
    expect(desktop).toBeDisabled();
    expect(screen.getByText(/these channels are used when it doesn.t/)).toBeInTheDocument();
    expect(screen.getByRole("checkbox", { name: "Slack" })).toBeDisabled();

    fireEvent.click(screen.getByRole("button", { name: "Save and review the next" }));
    await waitFor(() => expect(apiMock.createAlertRule).toHaveBeenCalledTimes(1));
    expect(apiMock.createAlertRule.mock.calls[0][0]).toMatchObject({ name: "High error ratio", signal: "http.error_ratio", channels: ["desktop"] });
    expect(await screen.findByText("Suggested rules: 1")).toBeInTheDocument();
    expect(screen.getByLabelText("Name")).toHaveValue("Slow responses (p95)");
    expect(onOpenChange).not.toHaveBeenCalledWith(false);

    fireEvent.click(screen.getByRole("button", { name: "Save and review the next" }));
    await waitFor(() => expect(apiMock.createAlertRule).toHaveBeenCalledTimes(2));
    expect(apiMock.createAlertRule.mock.calls[1][0]).toMatchObject({ name: "Slow responses (p95)", condition: { threshold: 1000 } });
    expect(onSaved).toHaveBeenCalledTimes(2);
    expect(onOpenChange).toHaveBeenCalledWith(false);
  });

  it("words a fall as its size and sends it negative", async () => {
    const { onSaved } = renderEditor();
    await waitFor(() => expect(screen.getByLabelText(/^Signal$/)).toBeEnabled());
    pick(/^Signal$/, /Response time/);
    pick(/^When the value$/, /falls by more than/);
    fireEvent.change(screen.getByLabelText("Threshold"), { target: { value: "30" } });
    expect(screen.getByText("Response time (p95) falls by more than 30% over 5 min for 5 min")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Save rule" }));
    await waitFor(() => expect(apiMock.createAlertRule).toHaveBeenCalledTimes(1));
    expect(apiMock.createAlertRule.mock.calls[0][0]).toMatchObject({ condition: { op: "change_pct_lt", threshold: -30 } });
    expect(onSaved).toHaveBeenCalled();
  });

  it("keeps a channel the rule names that cannot deliver, to be cleared, and says so when the server refuses it", async () => {
    const existing = saved(
      {
        name: "Slow",
        component_id: "c-api",
        environment: "production",
        signal: "http.latency",
        data_source_id: null,
        query: "",
        condition: { op: "gt", threshold: 800, for_seconds: 0, window_seconds: 300 },
        severity: "high",
        channels: ["desktop", "slack", "pager" as never],
        open_incident: false,
        enabled: true,
      },
      "ar-1",
    );
    apiMock.updateAlertRule.mockRejectedValueOnce(new ApiError("invalid rule", 400, "", { "channels.1": "unavailable" }));
    apiMock.updateAlertRule.mockImplementation((_id: string, body: Partial<AlertRule>) => Promise.resolve(saved(body, "ar-1")));
    renderEditor({ rule: existing });
    await waitFor(() => expect(apiMock.listNotificationChannels).toHaveBeenCalled());
    const slack = await screen.findByRole("checkbox", { name: "Slack" });
    await waitFor(() => expect(slack).toBeChecked());
    expect(slack).toBeEnabled();
    expect(screen.getByRole("checkbox", { name: "pager" })).toBeChecked();
    expect(screen.getByText("Unknown here")).toBeInTheDocument();
    expect(screen.getByRole("checkbox", { name: "In the app" })).toBeDisabled();

    fireEvent.click(screen.getByRole("button", { name: "Save rule" }));
    await waitFor(() => expect(apiMock.updateAlertRule).toHaveBeenCalledTimes(1));
    expect(apiMock.updateAlertRule.mock.calls[0][1]).toMatchObject({ channels: ["desktop", "slack", "pager"] });
    const error = await screen.findByText("Doesn't deliver here yet: clear it to save");
    expect(screen.getByRole("group", { name: "Channels" })).toHaveAttribute("aria-describedby", error.id);
    expect(screen.getByText("Check the fields marked below.")).toBeInTheDocument();

    fireEvent.click(slack);
    expect(slack).not.toBeChecked();
    expect(slack).toBeDisabled();
    fireEvent.click(screen.getByRole("button", { name: "Save rule" }));
    await waitFor(() => expect(apiMock.updateAlertRule).toHaveBeenCalledTimes(2));
    expect(apiMock.updateAlertRule.mock.calls[1][1]).toMatchObject({ channels: ["desktop", "pager"] });
  });

  it("puts the server's environment, op and severity errors on their fields, and any other on the form", async () => {
    apiMock.createAlertRule.mockRejectedValue(
      new ApiError("invalid rule", 400, "", { environment: "invalid_option", "condition.op": "invalid", severity: "required", limit: "invalid" }),
    );
    renderEditor();
    await waitFor(() => expect(screen.getByLabelText(/^Signal$/)).toBeEnabled());
    pick(/^Signal$/, /Response time/);
    fireEvent.change(screen.getByLabelText("Threshold"), { target: { value: "800" } });
    fireEvent.click(screen.getByRole("button", { name: "Save rule" }));
    expect(await screen.findByText("limit: Not valid")).toBeInTheDocument();
    const described = (label: RegExp | string) => screen.getByLabelText(label).getAttribute("aria-describedby") ?? "";
    expect(document.getElementById(described("Environment"))).toHaveTextContent("Not allowed here");
    expect(document.getElementById(described(/^When the value$/))).toHaveTextContent("Not valid");
    expect(document.getElementById(described("Severity"))).toHaveTextContent("Required");
    expect(screen.getByLabelText("Environment")).toHaveAttribute("aria-invalid", "true");
  });
});
