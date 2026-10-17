import "@testing-library/jest-dom/vitest";
import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { ApiError } from "@/api";
import { I18nProvider } from "@/hooks/useI18n";
import { writeCache } from "@/lib/uiCache";
import { cloudAccount, dataSource, gcpMonitoringKind, prometheusKind, recipes, signals } from "@/test/dataSourceFixtures";

const { plots, FakePlot } = vi.hoisted(() => {
  const plots: { opts: { series: { label?: string }[] }; data: unknown[] }[] = [];
  class FakePlot {
    static pxRatio = 1;
    static rangeNum = (min: number, max: number) => [min, max];
    opts: { series: { label?: string }[] };
    data: unknown[];
    constructor(opts: { series: { label?: string }[] }, data: unknown[]) {
      this.opts = opts;
      this.data = data;
      plots.push(this);
    }
    setData(data: unknown[]) {
      this.data = data;
    }
    setSize() {}
    redraw() {}
    destroy() {}
  }
  return { plots, FakePlot };
});

vi.mock("uplot", () => ({ default: FakePlot }));

const apiMock = vi.hoisted(() => ({
  listDataSourceKinds: vi.fn(),
  listDataSources: vi.fn(),
  listRecipes: vi.fn(),
  listSignals: vi.fn(),
  verifyDataSource: vi.fn(),
  deleteDataSource: vi.fn(),
  discoverDataSource: vi.fn(),
  queryDataSource: vi.fn(),
  listCloudAccounts: vi.fn(),
}));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return { ...actual, api: { ...actual.api, ...apiMock } };
});

import { DataSourcesPage } from "@/pages/DataSourcesPage";

const healthy = dataSource();
const broken = dataSource({
  id: "ds-2",
  name: "Staging Mimir",
  config: { url: "http://mimir.internal:9009/prometheus", auth: "none" },
  secrets_set: [],
  status: "error",
  status_detail: "401 Unauthorized",
  info: { version: "", detail: "" },
  verified_at: null,
});
const fresh = dataSource({ id: "ds-3", name: "Local", config: { url: "http://localhost:9090" }, status: "unverified", verified_at: null, info: { version: "", detail: "" } });

function renderPage(path = "/operations/sources") {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <I18nProvider>
        <DataSourcesPage />
      </I18nProvider>
    </MemoryRouter>,
  );
}

beforeEach(() => {
  sessionStorage.clear();
  for (const key of ["kinds", "list", "recipes", "signals", "cloudAccounts"]) writeCache(`operations.sources.${key}`, undefined);
  plots.length = 0;
  Object.values(apiMock).forEach((fn) => fn.mockReset());
  apiMock.listDataSourceKinds.mockResolvedValue({ kinds: [prometheusKind] });
  apiMock.listDataSources.mockResolvedValue({ data_sources: [healthy, broken, fresh] });
  apiMock.listRecipes.mockResolvedValue({ recipes });
  apiMock.listCloudAccounts.mockResolvedValue({ accounts: [] });
  apiMock.listSignals.mockResolvedValue({ signals });
  apiMock.discoverDataSource.mockResolvedValue({
    metrics: ["rabbitmq_queue_messages_ready", "up"],
    truncated: false,
    recipes: [{ recipe_id: "rabbitmq-prometheus", matched_metric: "rabbitmq_queue_messages_ready" }],
  });
});

describe("DataSourcesPage", () => {
  it("explains what a data source is when there is none", async () => {
    apiMock.listDataSources.mockResolvedValue({ data_sources: [] });
    renderPage();
    expect(await screen.findByText("Connect where your metrics already live")).toBeInTheDocument();
    expect(screen.getByText(/crash and error trackers \(Google Play vitals, App Store Connect, Sentry\), web analytics/)).toBeInTheDocument();
    await waitFor(() => expect(screen.getByRole("button", { name: "Add data source" })).toBeEnabled());
  });

  it("offers a connected Google Cloud account no source reads yet, opening the add dialog filled in", async () => {
    apiMock.listDataSourceKinds.mockResolvedValue({ kinds: [prometheusKind, gcpMonitoringKind] });
    apiMock.listCloudAccounts.mockResolvedValue({ accounts: [cloudAccount()] });
    renderPage();
    expect(await screen.findByText("Use your connected Google Cloud account for metrics")).toBeInTheDocument();
    expect(screen.getByText(/Acme production is connected, but no data source reads its metrics yet/)).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Add Google Cloud Monitoring" }));
    const dialog = await screen.findByRole("dialog", { name: "Add a data source" });
    expect(within(dialog).getByText("Kind: Google Cloud Monitoring")).toBeInTheDocument();
    expect(within(dialog).getByLabelText("Google Cloud account")).toHaveTextContent("Acme production");
  });

  it("offers nothing once a source reads through the account", async () => {
    apiMock.listDataSourceKinds.mockResolvedValue({ kinds: [prometheusKind, gcpMonitoringKind] });
    apiMock.listCloudAccounts.mockResolvedValue({ accounts: [cloudAccount()] });
    apiMock.listDataSources.mockResolvedValue({
      data_sources: [healthy, dataSource({ id: "ds-gcp", kind: "gcp_monitoring", name: "Cloud Monitoring", config: { cloud_account_id: "acc-gcp" } })],
    });
    renderPage();
    const table = await screen.findByRole("table", { name: "Data sources" });
    await waitFor(() => expect(within(table).getByText("Acme production")).toBeInTheDocument());
    expect(screen.queryByText("Use your connected Google Cloud account for metrics")).not.toBeInTheDocument();
  });

  it("lists each source with its address, kind, capabilities and status", async () => {
    renderPage();
    const table = await screen.findByRole("table", { name: "Data sources" });
    const rows = within(table).getAllByRole("row").slice(1);
    expect(rows).toHaveLength(3);

    expect(within(rows[0]).getByText("https://prom.example.com")).toBeInTheDocument();
    expect(within(rows[0]).getByText("Prometheus compatible")).toBeInTheDocument();
    expect(within(rows[0]).getByText("Metrics")).toBeInTheDocument();
    expect(within(rows[0]).getByText("Connected")).toBeInTheDocument();
    expect(within(rows[0]).getByText("Version 2.53.0")).toBeInTheDocument();

    expect(within(rows[1]).getByText("Error")).toBeInTheDocument();
    expect(within(rows[1]).getByText("401 Unauthorized")).toBeInTheDocument();
    expect(within(rows[2]).getByText("Not checked")).toBeInTheDocument();
  });

  it("verifies a source and shows the new status", async () => {
    apiMock.verifyDataSource.mockResolvedValue({ ...broken, status: "ok", status_detail: "", info: { version: "2.9.0", detail: "" } });
    renderPage();
    const table = await screen.findByRole("table", { name: "Data sources" });
    const row = within(table).getAllByRole("row")[2];
    fireEvent.click(within(row).getByRole("button", { name: "Verify Staging Mimir" }));
    await waitFor(() => expect(apiMock.verifyDataSource).toHaveBeenCalledWith("ds-2"));
    expect(await within(row).findByText("Version 2.9.0")).toBeInTheDocument();
  });

  it("keeps each running check's spinner, and takes a source deleted elsewhere off the list", async () => {
    let finish: (source: unknown) => void = () => {};
    apiMock.verifyDataSource.mockImplementation((id: string) =>
      id === "ds-1"
        ? new Promise((resolve) => {
            finish = resolve;
          })
        : Promise.reject(new ApiError("data source not found", 404)),
    );
    renderPage();
    const table = await screen.findByRole("table", { name: "Data sources" });
    fireEvent.click(within(table).getByRole("button", { name: "Verify Prod Prometheus" }));
    fireEvent.click(within(table).getByRole("button", { name: "Verify Local" }));
    await waitFor(() => expect(within(table).queryByRole("button", { name: "Open Local" })).not.toBeInTheDocument());
    expect(within(table).getByRole("button", { name: "Checking Prod Prometheus…" })).toBeDisabled();
    expect(within(table).getAllByRole("row")).toHaveLength(3);
    await act(async () => finish(healthy));
    expect(await within(table).findByRole("button", { name: "Verify Prod Prometheus" })).toBeEnabled();
  });

  it("opens a source, runs a query and draws every series", async () => {
    apiMock.queryDataSource.mockResolvedValue({
      series: [
        { name: "up", labels: { job: "api" }, points: [[1_000, 1], [2_000, 1]] },
        { name: "up", labels: { job: "web" }, points: [[1_000, 0], [2_000, null]] },
      ],
      warnings: ["showing the first 500 of 900 series"],
    });
    renderPage();
    fireEvent.click(await screen.findByRole("button", { name: "Open Prod Prometheus" }));
    const drawer = await screen.findByRole("dialog");
    expect(within(drawer).getByRole("heading", { name: "Prod Prometheus" })).toBeInTheDocument();

    expect(await within(drawer).findByText("rabbitmq-prometheus")).toBeInTheDocument();
    expect(apiMock.discoverDataSource).toHaveBeenCalledWith("ds-1", { match: "", limit: 200 });
    fireEvent.click(within(drawer).getByRole("button", { name: "up" }));
    expect(within(drawer).getByLabelText("Query")).toHaveValue("up");

    fireEvent.click(within(drawer).getByRole("button", { name: "Run" }));
    await waitFor(() => expect(apiMock.queryDataSource).toHaveBeenCalledTimes(1));
    const [id, body] = apiMock.queryDataSource.mock.calls[0];
    expect(id).toBe("ds-1");
    expect(body).toMatchObject({ query: "up", step_seconds: 0 });
    expect(Date.parse(body.to) - Date.parse(body.from)).toBe(3_600_000);

    expect(await within(drawer).findByText("showing the first 500 of 900 series")).toBeInTheDocument();
    expect(within(drawer).getByRole("img", { name: "Query result" })).toBeInTheDocument();
    await waitFor(() => expect(plots.length).toBeGreaterThan(0));
    const plot = plots.at(-1)!;
    expect(plot.opts.series.slice(1).map((s) => s.label)).toEqual(['up{job="api"}', 'up{job="web"}']);
    expect(plot.data).toEqual([[1_000, 2_000], [1, 1], [0, null]]);
  });

  it("shows the source's refusal of a query", async () => {
    const { ApiError } = await import("@/api");
    apiMock.queryDataSource.mockRejectedValue(new ApiError("parse error at char 3", 400, "bad_query"));
    renderPage();
    fireEvent.click(await screen.findByRole("button", { name: "Open Prod Prometheus" }));
    const drawer = await screen.findByRole("dialog");
    fireEvent.change(within(drawer).getByLabelText("Query"), { target: { value: "up{" } });
    fireEvent.click(within(drawer).getByRole("button", { name: "Run" }));
    expect(await within(drawer).findByText("The source refused this query")).toBeInTheDocument();
    expect(within(drawer).getByText("parse error at char 3")).toBeInTheDocument();
  });

  it("deletes a source after confirming", async () => {
    apiMock.deleteDataSource.mockResolvedValue(undefined);
    renderPage();
    fireEvent.click(await screen.findByRole("button", { name: "Delete Staging Mimir" }));
    const confirm = await screen.findByRole("dialog");
    expect(within(confirm).getByText("Delete Staging Mimir?")).toBeInTheDocument();
    fireEvent.click(within(confirm).getByRole("button", { name: "Delete" }));
    await waitFor(() => expect(apiMock.deleteDataSource).toHaveBeenCalledWith("ds-2"));
    await waitFor(() => expect(screen.queryByText("Staging Mimir")).not.toBeInTheDocument());
  });

  it("lists the recipe catalog with the signals each one provides", async () => {
    renderPage("/operations/sources?tab=recipes");
    const table = await screen.findByRole("table", { name: "Recipes" });
    const rows = within(table).getAllByRole("row").slice(1);
    expect(rows.map((r) => within(r).getAllByRole("cell")[0].textContent)).toEqual(["otel-http", "rabbitmq-prometheus"]);
    expect(within(rows[1]).getByText("rabbitmq")).toBeInTheDocument();
    expect(within(rows[1]).getByText("queue.backlog")).toHaveAttribute("title", "Unit: messages");
    expect(within(rows[1]).getByText("rabbitmq_queue_messages_ready")).toBeInTheDocument();
    expect(screen.getByText(/catalog\/observability\/recipes\//)).toBeInTheDocument();
  });
});
