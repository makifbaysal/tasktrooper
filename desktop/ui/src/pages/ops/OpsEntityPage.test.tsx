import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter, Route, Routes, useLocation } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { Incident, ProjectsOverview } from "@/api";
import { I18nProvider } from "@/hooks/useI18n";
import { writeCache } from "@/lib/uiCache";
import { dataSource, prometheusKind } from "@/test/dataSourceFixtures";
import { component, env, incident, opsIncidents, opsOverview, opsReleases, repo } from "@/test/opsConsoleFixtures";

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

const { getProjectsOverview, listIncidents, listAllReleases, getComponentSeries, getOpsGolden, editorApi } = vi.hoisted(() => ({
  getProjectsOverview: vi.fn(),
  listIncidents: vi.fn(),
  listAllReleases: vi.fn(),
  getComponentSeries: vi.fn(),
  getOpsGolden: vi.fn(),
  editorApi: {
    getComponentSignals: vi.fn(),
    listDataSourceKinds: vi.fn(),
    listDataSources: vi.fn(),
    listRecipes: vi.fn(),
    listSignals: vi.fn(),
  },
}));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return {
    ...actual,
    api: { ...actual.api, getProjectsOverview, listIncidents, listAllReleases, getComponentSeries, getOpsGolden, ...editorApi },
  };
});

import { OpsShell } from "@/components/operations/shell/OpsShell";
import OpsEntityPage from "@/pages/ops/OpsEntityPage";
import EntityChanges from "@/pages/ops/entity/EntityChanges";
import EntityErrors from "@/pages/ops/entity/EntityErrors";
import EntityIssues from "@/pages/ops/entity/EntityIssues";
import EntityLogs from "@/pages/ops/entity/EntityLogs";
import EntityQueues from "@/pages/ops/entity/EntityQueues";
import EntityReleases from "@/pages/ops/entity/EntityReleases";
import EntitySummary from "@/pages/ops/entity/EntitySummary";
import { EntitySubRoute } from "@/pages/ops/entity/EntitySubRoute";
import { EntityIndexRedirect } from "@/pages/ops/opsRedirects";

const appsRepo = repo(
  "r-apps",
  "apps",
  [component("c-ios", "ios", "mobile", "ios"), component("c-queue", "mailer", "worker", "mailer")],
  [env("e-ios", "c-ios", { provider: "aws", resource_name: "ios-store" })],
);

const overview = {
  ...opsOverview,
  unassigned: [...(opsOverview as unknown as { unassigned: unknown[] }).unassigned, appsRepo],
} as unknown as ProjectsOverview;

const incidents: Incident[] = [...opsIncidents, incident("i-preview", "r-shop", "medium", { env: "preview", title: "Preview is down" })];

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
            <Route index element={<p>all entities</p>} />
            <Route path="entity/:componentId" element={<OpsEntityPage />}>
              <Route index element={<EntityIndexRedirect />} />
              <Route path="summary" element={<EntitySubRoute tab="summary" page={EntitySummary} />} />
              <Route path="queues" element={<EntitySubRoute tab="queues" page={EntityQueues} />} />
              <Route path="releases" element={<EntitySubRoute tab="releases" page={EntityReleases} />} />
              <Route path="errors" element={<EntitySubRoute tab="errors" page={EntityErrors} />} />
              <Route path="logs" element={<EntitySubRoute tab="logs" page={EntityLogs} />} />
              <Route path="changes" element={<EntitySubRoute tab="changes" page={EntityChanges} />} />
              <Route path="issues" element={<EntitySubRoute tab="issues" page={EntityIssues} />} />
              <Route path="*" element={<EntityIndexRedirect />} />
            </Route>
          </Route>
        </Routes>
        <Where />
      </I18nProvider>
    </MemoryRouter>,
  );
}

beforeEach(() => {
  sessionStorage.clear();
  localStorage.clear();
  for (const key of ["operations.entities.overview", "operations.console.openIncidents", "operations.console.shippedReleases"]) {
    writeCache(key, undefined);
  }
  getProjectsOverview.mockReset().mockResolvedValue(overview);
  listIncidents.mockReset().mockImplementation((params?: { repositoryId?: string }) => {
    const list = params?.repositoryId
      ? incidents.filter((i) => i.repository_id === params.repositoryId)
      : incidents.filter((i) => !["resolved", "ignored"].includes(i.status));
    return Promise.resolve({ incidents: list, count: list.length });
  });
  listAllReleases.mockReset().mockResolvedValue({ releases: opsReleases });
  getComponentSeries.mockReset().mockImplementation((_id: string, q: { signals: string[] }) =>
    Promise.resolve({
      from: "",
      to: "",
      step_seconds: 60,
      signals: q.signals.map((signal) => ({ signal, unit: "ms", bound: false, series: [], previous: [], error: null })),
    }),
  );
  getOpsGolden.mockReset().mockResolvedValue({ generated_at: "", components: [] });
  for (const key of ["kinds", "list", "recipes", "signals"]) writeCache(`operations.sources.${key}`, undefined);
  writeCache("operations.signals.c-api.production", undefined);
  editorApi.listDataSourceKinds.mockReset().mockResolvedValue({ kinds: [prometheusKind] });
  editorApi.listDataSources.mockReset().mockResolvedValue({ data_sources: [dataSource()] });
  editorApi.listRecipes.mockReset().mockResolvedValue({
    recipes: [
      {
        id: "otel-http",
        source_kind: "prometheus",
        applies_to: { technology: "", messaging_system: "", resource_kind: "", resource_params: {} },
        detect: { metric: "" },
        params: ["job"],
        signals: { "http.latency": "q", "http.requests": "q" },
        origin: "catalog",
      },
    ],
  });
  editorApi.listSignals.mockReset().mockResolvedValue({ signals: [{ id: "http.error_ratio", kind: "metric", unit: "%", roles: ["backend"], aggregation: "ratio" }] });
  editorApi.getComponentSignals.mockReset().mockResolvedValue({ component_id: "c-api", role: "backend", environment: "production", signals: [] });
});

async function sections(name: string) {
  const nav = (await screen.findAllByRole("navigation", { name: `${name} sections` }))[0];
  return within(nav)
    .getAllByRole("link")
    .map((a) => a.textContent);
}

const DAY = Date.parse("2026-10-15T00:00:00Z");
const ON_THE_15TH = `range=custom&from=${DAY}&to=${DAY + 86_400_000}`;
// A window of its own per test: answers are cached per window for the tab.
const onDay = (n: number) => `range=custom&from=${DAY - n * 86_400_000}&to=${DAY - (n - 1) * 86_400_000}`;

describe("OpsEntityPage", () => {
  it("opens on Summary, keeping the console's params", async () => {
    renderAt("/operations/entity/c-api?env=staging");
    await waitFor(() => expect(screen.getByTestId("where")).toHaveTextContent("/operations/entity/c-api/summary?env=staging"));
    expect(await screen.findByRole("heading", { level: 1, name: "api" })).toBeInTheDocument();
  });

  it("gives each role its own sections", async () => {
    renderAt("/operations/entity/c-api/summary");
    expect(await sections("api")).toEqual(["Summary", "Endpoints", "Errors", "Logs", "Traces", "Changes", "Issues", "Alerts", "Signals"]);
  });

  it("adds Queues for a worker, and Crashes and Releases for a mobile app", async () => {
    const worker = renderAt("/operations/entity/c-queue/summary");
    expect(await sections("mailer")).toEqual(["Summary", "Queues", "Errors", "Logs", "Traces", "Changes", "Issues", "Alerts", "Signals"]);
    worker.unmount();
    const mobile = renderAt("/operations/entity/c-ios/summary");
    expect(await sections("ios")).toEqual(["Summary", "Crashes", "Releases", "Errors", "Logs", "Changes", "Issues", "Alerts", "Signals"]);
    mobile.unmount();
    renderAt("/operations/entity/c-lib/summary");
    expect(await sections("ui-kit")).toEqual(["Summary", "Changes", "Issues", "Alerts"]);
  });

  it("sends a section the role does not have back to Summary, its query kept", async () => {
    renderAt("/operations/entity/c-api/queues?range=6h&rule=ar-1");
    await waitFor(() => expect(screen.getByTestId("where")).toHaveTextContent("/operations/entity/c-api/summary?range=6h&rule=ar-1"));
  });

  it("renders the worker's Queues as the way to connect a data source", async () => {
    renderAt("/operations/entity/c-queue/queues");
    expect(await screen.findByText("Connect a data source to see queues")).toBeInTheDocument();
  });

  it("keeps the environment picked even where the entity is not connected, with that environment's issues", async () => {
    renderAt("/operations/entity/c-api/summary?env=preview");
    expect(await screen.findByText("Not reporting in Preview")).toBeInTheDocument();
    expect(screen.getAllByText("Not connected in Preview").length).toBeGreaterThan(0);
    expect(screen.getByTestId("where")).toHaveTextContent("env=preview");
    const tile = screen.getByText("Open issues", { selector: "p" }).closest("div") as HTMLElement;
    await waitFor(() => expect(within(tile).getByText("1")).toBeInTheDocument());
    expect(screen.getByRole("heading", { level: 1, name: "api" }).parentElement).toHaveTextContent("Warning");
  });

  it("shows golden metrics blank until bound, the window's deploys and its activity", async () => {
    renderAt(`/operations/entity/c-api/summary?${ON_THE_15TH}`);
    expect(await screen.findByRole("heading", { level: 3, name: /^Response time/ })).toBeInTheDocument();
    await waitFor(() => expect(screen.getAllByTitle("No data source bound").length).toBeGreaterThan(0));
    expect(screen.getAllByRole("button", { name: /^Bind a data source to / }).length).toBe(4);
    expect(getComponentSeries).toHaveBeenCalledWith(
      "c-api",
      expect.objectContaining({ env: "production", signals: ["http.latency", "http.requests", "http.error_ratio", "infra.cpu"] }),
    );
    expect(screen.getByRole("button", { name: /^Time range/ })).toBeInTheDocument();
    expect(screen.getByText("Compare with previous period")).toBeInTheDocument();

    const lane = await screen.findByRole("list", { name: "Deploys in this range" });
    const marker = await within(lane).findByRole("link", { name: /^Deploy 1\.4\.0/ });
    expect(marker).toHaveAttribute("href", expect.stringContaining("/operations/entity/c-api/changes?release=1.4.0"));
    expect(within(lane).queryByRole("link", { name: /pre-1|1\.5\.0-rc/ })).not.toBeInTheDocument();

    const activity = screen.getByRole("list", { name: "Activity" });
    await waitFor(() => expect(within(activity).getByText("Incident i-shop")).toBeInTheDocument());
    expect(within(activity).getByText("1.4.0")).toBeInTheDocument();
    expect(within(activity).queryByText("Incident i-pre")).not.toBeInTheDocument();
    expect(listAllReleases).toHaveBeenCalledWith(expect.objectContaining({ componentId: "c-api", limit: 100 }));
  });

  it("draws bound signals live: quantiles, the previous period dashed with compare on, a failed read with Retry", async () => {
    const pts = (v: number): Array<[number, number | null]> => [[DAY + 3_600_000, v], [DAY + 7_200_000, v + 10]];
    getComponentSeries.mockResolvedValue({
      from: "",
      to: "",
      step_seconds: 600,
      signals: [
        {
          signal: "http.latency",
          unit: "ms",
          bound: true,
          series: [{ name: "p95", labels: {}, points: pts(120) }, { name: "p50", labels: {}, points: pts(40) }, { name: "p99", labels: {}, points: pts(300) }],
          previous: [{ name: "p95", labels: {}, points: pts(100) }],
          error: null,
        },
        { signal: "http.requests", unit: "req/s", bound: true, series: [], previous: [], error: { code: "upstream", message: "prometheus: 502 Bad Gateway" } },
        { signal: "http.error_ratio", unit: "%", bound: false, series: [], previous: [], error: null },
        { signal: "infra.cpu", unit: "cores", bound: true, series: [], previous: [], error: null },
      ],
    });
    getOpsGolden.mockResolvedValue({
      generated_at: "",
      components: [
        {
          component_id: "c-api",
          signals: [
            { signal: "http.latency", unit: "ms", value: 120, previous: 100, spark: pts(110) },
            { signal: "http.requests", unit: "req/s", value: null, previous: null, spark: [] },
            { signal: "http.error_ratio", unit: "%", value: null, previous: null, spark: [] },
          ],
        },
      ],
    });
    renderAt(`/operations/entity/c-api/summary?${ON_THE_15TH}&compare=1`);

    const latency = (await screen.findByRole("heading", { level: 3, name: "Response time" })).parentElement!.parentElement!;
    const legend = await within(latency).findByRole("list", { name: "Series" });
    expect(within(legend).getAllByRole("listitem").map((li) => li.textContent)).toEqual(["p50", "p95", "p99", "p95 · previous period"]);
    await waitFor(() =>
      expect(getComponentSeries).toHaveBeenCalledWith("c-api", expect.objectContaining({ compare: true, stepSeconds: 600 })),
    );

    const throughput = screen.getByRole("heading", { level: 3, name: "Throughput" }).parentElement!.parentElement!;
    expect(within(throughput).getByText("The data source didn't answer")).toBeInTheDocument();
    expect(within(throughput).getByText("prometheus: 502 Bad Gateway")).toBeInTheDocument();
    const calls = getComponentSeries.mock.calls.length;
    fireEvent.click(within(throughput).getByRole("button", { name: "Retry" }));
    await waitFor(() => expect(getComponentSeries.mock.calls.length).toBe(calls + 1));

    const errorRate = screen.getByRole("heading", { level: 3, name: "Error rate" }).parentElement!.parentElement!;
    expect(within(errorRate).getByText("No data source bound")).toBeInTheDocument();
    expect(within(errorRate).getByRole("link", { name: "Manage signals" })).toHaveAttribute(
      "href",
      expect.stringContaining("/operations/entity/c-api/signals"),
    );
    const cpu = screen.getByRole("heading", { level: 3, name: "CPU" }).parentElement!.parentElement!;
    expect(within(cpu).getByText("No data in this range")).toBeInTheDocument();

    const tile = screen.getByText("Response time", { selector: "p" }).closest("div") as HTMLElement;
    expect(within(tile).getByText("120")).toBeInTheDocument();
    expect(within(tile).getByText("ms")).toBeInTheDocument();
    expect(within(tile).getByText("+20%")).toHaveClass("text-destructive");
    expect(within(tile).getByText("worse", { exact: false })).toHaveClass("sr-only");
    expect(within(tile).getByText("vs previous period")).toBeInTheDocument();
    expect(within(tile).getByText("Previous period: 100 ms")).toBeInTheDocument();
    const rateTile = screen.getByText("Error rate", { selector: "p" }).closest("div") as HTMLElement;
    expect(within(rateTile).getByRole("link", { name: "Bind a data source" })).toBeInTheDocument();
  });

  it("never turns a bound signal into Bind when the series read fails: the failure with Retry instead", async () => {
    getComponentSeries.mockRejectedValue(new Error("The server did not answer in time. Please try again."));
    getOpsGolden.mockResolvedValue({
      generated_at: "",
      components: [
        {
          component_id: "c-api",
          signals: [
            { signal: "http.latency", unit: "ms", value: null, previous: null, spark: [] },
            { signal: "http.requests", unit: "req/s", value: null, previous: null, spark: [] },
            { signal: "http.error_ratio", unit: "%", value: null, previous: null, spark: [] },
          ],
        },
      ],
    });
    renderAt(`/operations/entity/c-api/summary?${onDay(2)}`);
    const tile = (await screen.findByText("Response time", { selector: "p" })).closest("div") as HTMLElement;
    await waitFor(() => expect(within(tile).getByText("Couldn't read")).toBeInTheDocument());
    expect(within(tile).getByRole("button", { name: "Retry" })).toBeInTheDocument();
    expect(screen.queryByRole("link", { name: "Bind a data source" })).not.toBeInTheDocument();
    const latency = screen.getByRole("heading", { level: 3, name: "Response time" }).parentElement!.parentElement!;
    expect(within(latency).getByText("Couldn't read this signal")).toBeInTheDocument();
    expect(within(latency).queryByRole("button", { name: /^Bind a data source to / })).not.toBeInTheDocument();
  });

  it("keeps a tile in flight from reading as unbound or empty", async () => {
    getComponentSeries.mockImplementation(() => new Promise(() => {}));
    getOpsGolden.mockImplementation(() => new Promise(() => {}));
    renderAt(`/operations/entity/c-api/summary?${onDay(3)}`);
    const tile = (await screen.findByText("Response time", { selector: "p" })).closest("div") as HTMLElement;
    expect(within(tile).queryByText("Bind a data source")).not.toBeInTheDocument();
    expect(within(tile).queryByText("No data in this range")).not.toBeInTheDocument();
  });

  it("answers each series error code with what helps, and notes a previous period it could not read", async () => {
    const pts: Array<[number, number | null]> = [[DAY + 3_600_000, 1], [DAY + 7_200_000, 2]];
    getComponentSeries.mockResolvedValue({
      from: "",
      to: "",
      step_seconds: 600,
      signals: [
        { signal: "http.latency", unit: "ms", bound: true, series: [], previous: [], error: { code: "needs_params", message: "no value for job" } },
        { signal: "http.requests", unit: "req/s", bound: true, series: [], previous: [], error: { code: "unsupported", message: "x is a log signal" } },
        { signal: "http.error_ratio", unit: "%", bound: true, series: [], previous: [], error: { code: "timeout", message: "no answer within 20s" } },
        {
          signal: "infra.cpu",
          unit: "cores",
          bound: true,
          series: [{ name: "", labels: {}, points: pts }],
          previous: [],
          error: null,
          previous_error: { code: "timeout", message: "no answer within 20s" },
        },
      ],
    });
    editorApi.getComponentSignals.mockResolvedValue({
      component_id: "c-api",
      role: "backend",
      environment: "production",
      signals: [
        {
          signal: "http.latency",
          kind: "metric",
          unit: "ms",
          golden: true,
          status: "bound",
          binding: {
            id: "b-lat",
            component_id: "c-api",
            environment: "production",
            signal: "http.latency",
            data_source_id: "ds-1",
            data_source_name: "Prod Prometheus",
            data_source_kind: "prometheus",
            recipe_id: "otel-http",
            params: {},
            query: "",
            status: "confirmed",
            source: "human",
            rendered_query: "",
            needs_params: ["job"],
            created_at: "",
            updated_at: "",
          },
          suggestions: [],
        },
      ],
    });
    renderAt(`/operations/entity/c-api/summary?${onDay(4)}&compare=1`);
    const slot = async (name: string) => (await screen.findByRole("heading", { level: 3, name })).parentElement!.parentElement!;

    const latency = await slot("Response time");
    await waitFor(() => expect(within(latency).getByText("The binding is missing a parameter")).toBeInTheDocument());
    expect(within(latency).queryByRole("button", { name: "Retry" })).not.toBeInTheDocument();
    const requests = await slot("Throughput");
    expect(within(requests).getByText("This signal can't be charted")).toBeInTheDocument();
    expect(within(requests).queryByRole("button", { name: "Retry" })).not.toBeInTheDocument();
    const errorRate = await slot("Error rate");
    expect(within(errorRate).getByText("The data source didn't answer in time")).toBeInTheDocument();
    expect(within(errorRate).getByRole("button", { name: "Retry" })).toBeInTheDocument();
    const cpu = await slot("CPU");
    expect(within(cpu).getByText("Previous period not shown: The data source didn't answer in time")).toBeInTheDocument();
    expect(within(cpu).queryByText("Couldn't read this signal")).not.toBeInTheDocument();

    fireEvent.click(within(latency).getByRole("button", { name: "Fill in parameters" }));
    const dialog = () => screen.getByRole("dialog", { name: "Bind Response time" });
    await waitFor(() => expect(within(dialog()).getByLabelText("Recipe")).toHaveTextContent("otel-http"));
    expect(within(dialog()).getByLabelText("Data source")).toHaveTextContent("Prod Prometheus");
    expect(within(dialog()).getByLabelText("job")).toHaveValue("");
    expect(editorApi.getComponentSignals).toHaveBeenCalledWith("c-api", "production");
  });

  it("asks for at most 30 days of golden metrics and says so", async () => {
    const to = DAY + 86_400_000;
    renderAt(`/operations/entity/c-api/summary?range=custom&from=${to - 45 * 86_400_000}&to=${to}`);
    expect(await screen.findByText(/^Golden metrics cover the last 30 days of this range/)).toBeInTheDocument();
    await waitFor(() => expect(getOpsGolden).toHaveBeenCalled());
    const query = getOpsGolden.mock.calls[0][0];
    expect(query.to - query.from).toBe(30 * 86_400_000);
    expect(query.componentIds).toEqual(["c-api"]);
  });

  it("reads a sole component's releases by repository", async () => {
    renderAt(`/operations/entity/c-site/summary?${ON_THE_15TH}`);
    await screen.findByRole("heading", { level: 1, name: "site" });
    await waitFor(() => expect(listAllReleases).toHaveBeenCalledWith(expect.objectContaining({ repositoryId: "r-site" })));
  });

  it("says when the entity does not exist", async () => {
    renderAt("/operations/entity/nope/summary");
    expect(await screen.findByText("Entity not found")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Back to all entities" })).toHaveAttribute("href", "/operations");
  });

  it("keeps the shell up for an address that does not decode", async () => {
    renderAt("/operations/entity/%E0%A4/summary");
    expect(await screen.findByText("Entity not found")).toBeInTheDocument();
  });

  it("says a failed first read is a failure, not a missing entity, and tries again", async () => {
    getProjectsOverview.mockReset().mockRejectedValueOnce(new Error("server is restarting")).mockResolvedValue(overview);
    renderAt("/operations/entity/c-api/summary");
    expect(await screen.findByText("Couldn't load this entity")).toBeInTheDocument();
    expect(screen.getByText("server is restarting")).toBeInTheDocument();
    expect(screen.queryByText("Entity not found")).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(await screen.findByRole("heading", { level: 1, name: "api" })).toBeInTheDocument();
  });
});
