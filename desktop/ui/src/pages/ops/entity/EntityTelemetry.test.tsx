import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import type { ReactNode } from "react";
import { MemoryRouter, useLocation } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { ApiError, type ComponentTable, type ComponentTables, type TraceSpan } from "@/api";
import { OpsProvider } from "@/components/operations/shell/OpsContext";
import type { OpsEntity } from "@/components/operations/shell/useOpsEntities";
import { I18nProvider } from "@/hooks/useI18n";
import { writeCache } from "@/lib/uiCache";
import { opsOverview } from "@/test/opsConsoleFixtures";

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
  getComponentEndpoints: vi.fn(),
  getComponentWeb: vi.fn(),
  getComponentCrashes: vi.fn(),
  getComponentTraces: vi.fn(),
  getOperationsTrace: vi.fn(),
  getProjectsOverview: vi.fn(),
  getComponentSignals: vi.fn(),
  listDataSourceKinds: vi.fn(),
  listDataSources: vi.fn(),
  listRecipes: vi.fn(),
  listSignals: vi.fn(),
}));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return { ...actual, api: { ...actual.api, ...apiMock } };
});
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn(), info: vi.fn() } }));

import EntityCrashes from "@/pages/ops/entity/EntityCrashes";
import EntityEndpoints from "@/pages/ops/entity/EntityEndpoints";
import EntityPages from "@/pages/ops/entity/EntityPages";
import EntityTraces from "@/pages/ops/entity/EntityTraces";

function entity(id: string, name: string, role: OpsEntity["role"]): OpsEntity {
  return { id, name, role, repositoryId: "r-shop", repositoryName: "shop", projectIds: [], environments: [] };
}

const api = entity("c-api", "api", "backend");
const web = entity("c-web", "web", "frontend");
const ios = entity("c-ios", "ios", "mobile");

function Where() {
  const { search } = useLocation();
  return <p data-testid="where">{search}</p>;
}

function renderAt(path: string, page: ReactNode) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <I18nProvider>
        <OpsProvider>
          {page}
          <Where />
        </OpsProvider>
      </I18nProvider>
    </MemoryRouter>,
  );
}

function table(partial: Partial<ComponentTable>): ComponentTable {
  return {
    signal_binding_id: "b-1",
    data_source_id: "ds-1",
    data_source_name: "Prod Prometheus",
    recipe_id: "otel-http",
    table: "endpoints",
    params: {},
    columns: [],
    rows: [],
    truncated: false,
    error: null,
    ...partial,
  };
}

function tables(componentId: string, list: ComponentTable[]): ComponentTables {
  return { component_id: componentId, environment: "production", tables: list };
}

function bodyRows(name: string) {
  const grid = screen.getByRole("table", { name });
  return within(grid).getAllByRole("row").slice(1);
}

beforeEach(() => {
  sessionStorage.clear();
  localStorage.clear();
  for (const key of [
    "operations.endpoints.c-api.production",
    "operations.web.c-web.production.pages",
    "operations.web.c-web.production.referrers",
    "operations.crashes.c-ios.production",
    "operations.entities.overview",
  ]) {
    writeCache(key, undefined);
  }
  Object.values(apiMock).forEach((fn) => fn.mockReset());
  apiMock.getProjectsOverview.mockResolvedValue(opsOverview);
  apiMock.getComponentSignals.mockResolvedValue({ component_id: "c-api", role: "backend", environment: "production", signals: [] });
  apiMock.listDataSourceKinds.mockResolvedValue({ kinds: [] });
  apiMock.listDataSources.mockResolvedValue({ data_sources: [] });
  apiMock.listRecipes.mockResolvedValue({ recipes: [] });
  apiMock.listSignals.mockResolvedValue({ signals: [] });
});

describe("EntityEndpoints", () => {
  const columns = [
    { key: "http_route", label: "Endpoint", unit: "" },
    { key: "rpm", label: "Requests", unit: "req/min" },
    { key: "error_pct", label: "Errors", unit: "%" },
    { key: "p95_ms", label: "p95", unit: "ms" },
  ];

  it("lists the endpoints busiest first, sortable, the most failing and the slowest called out", async () => {
    apiMock.getComponentEndpoints.mockResolvedValue(
      tables("c-api", [
        table({
          columns,
          rows: [
            { http_route: "/health", rpm: 20, error_pct: 0, p95_ms: 3 },
            { http_route: "/orders", rpm: 900, error_pct: 0.4, p95_ms: 180 },
            { http_route: "/checkout", rpm: 120, error_pct: 6.5, p95_ms: 1400 },
          ],
        }),
      ]),
    );
    renderAt("/operations/entity/c-api/endpoints", <EntityEndpoints entity={api} env="production" />);
    await screen.findByRole("table", { name: "Prod Prometheus" });
    expect(apiMock.getComponentEndpoints).toHaveBeenCalledWith("c-api", "production");
    const rows = bodyRows("Prod Prometheus");
    expect(rows.map((r) => within(r).getAllByRole("cell")[1].textContent)).toEqual(["/orders", "/checkout", "/health"]);
    expect(within(rows[1]).getByText("Highest error rate, Slowest p95")).toBeInTheDocument();
    expect(screen.getByText("Highest error rate", { selector: "span" })).toBeInTheDocument();
    expect(within(screen.getByRole("columnheader", { name: /Requests/ })).getByText("(req/min)")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Sort by p95" }));
    expect(bodyRows("Prod Prometheus").map((r) => within(r).getAllByRole("cell")[1].textContent)).toEqual(["/checkout", "/orders", "/health"]);
    expect(screen.getByRole("columnheader", { name: /p95/ })).toHaveAttribute("aria-sort", "descending");
  });

  it("says where endpoints come from when no binding reads a table", async () => {
    apiMock.getComponentEndpoints.mockResolvedValue(tables("c-api", []));
    renderAt("/operations/entity/c-api/endpoints", <EntityEndpoints entity={api} env="production" />);
    expect(await screen.findByText("No endpoint table yet")).toBeInTheDocument();
    expect(screen.getByText(/OpenTelemetry metrics in Prometheus, New Relic or Datadog/)).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Open Signals" })).toHaveAttribute("href", "/operations/entity/c-api/signals");
  });

  it("says when the tables were read, the window each covers, and names rows by their key columns", async () => {
    const readAt = new Date(Date.now() - 2 * 60_000).toISOString();
    apiMock.getComponentEndpoints.mockResolvedValue({
      ...tables("c-api", [
        table({ recipe_id: "datadog-apm", data_source_name: "Datadog", key_columns: ["http_route"], columns, rows: [{ http_route: "/orders", rpm: 9, error_pct: 0, p95_ms: 3 }] }),
        table({ recipe_id: "otel-http", data_source_name: "Prom", columns, rows: [{ http_route: "/x", rpm: 1, error_pct: 0, p95_ms: 3 }] }),
      ]),
      read_at: readAt,
    });
    renderAt("/operations/entity/c-api/endpoints", <EntityEndpoints entity={api} env="production" />);
    const datadog = (await screen.findByRole("table", { name: "Datadog" })).closest(".rounded-xl") as HTMLElement;
    expect(within(datadog).getByText("Covers the last 10 minutes")).toBeInTheDocument();
    const read = within(datadog).getByText("Read 2 minutes ago");
    expect(read.closest("[title]")).toHaveAttribute("title", expect.stringContaining("30 seconds"));
    expect(within(bodyRows("Datadog")[0]).getByText("/orders")).toHaveClass("font-medium");
    expect(within(bodyRows("Prom")[0]).getByText("/x")).not.toHaveClass("font-medium");
    const prom = screen.getByRole("table", { name: "Prom" }).closest(".rounded-xl") as HTMLElement;
    expect(within(prom).getByText("Covers the source's own recent window")).toBeInTheDocument();
    expect(within(prom).getByRole("link", { name: "Table windows" })).toHaveAttribute("href", expect.stringContaining("data-sources#table-windows"));
  });

  it("opens the binding editor on the table's signal for an error its binding fixes", async () => {
    apiMock.getComponentEndpoints.mockResolvedValue(
      tables("c-api", [table({ signal: "http.requests", columns, error: { code: "needs_params", message: "no value for job" } })]),
    );
    renderAt("/operations/entity/c-api/endpoints", <EntityEndpoints entity={api} env="production" />);
    fireEvent.click(await screen.findByRole("button", { name: "Fill in parameters" }));
    await waitFor(() => expect(screen.getByRole("dialog")).toHaveTextContent(/Bind/));
    await waitFor(() => expect(apiMock.getComponentSignals).toHaveBeenCalledWith("c-api", "production"));
    expect(screen.queryByRole("link", { name: "Open Signals" })).toBeNull();
  });

  it("explains a table that could not be read, with Retry", async () => {
    apiMock.getComponentEndpoints.mockResolvedValue(
      tables("c-api", [table({ columns, error: { code: "upstream", message: "502 from prometheus" } })]),
    );
    renderAt("/operations/entity/c-api/endpoints", <EntityEndpoints entity={api} env="production" />);
    expect(await screen.findByText("The source didn't answer")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    await waitFor(() => expect(apiMock.getComponentEndpoints).toHaveBeenCalledTimes(2));
  });
});

describe("EntityPages", () => {
  const pageColumns = [
    { key: "path", label: "Path", unit: "" },
    { key: "views", label: "Views", unit: "views" },
    { key: "visitors", label: "Visitors", unit: "visitors" },
  ];

  it("reads one web table per tab, the most viewed first with bars", async () => {
    apiMock.getComponentWeb.mockImplementation((_id: string, _env: string, name: string) =>
      Promise.resolve(
        tables("c-web", [
          name === "pages"
            ? table({ table: "pages", recipe_id: "posthog-web", data_source_name: "PostHog", columns: pageColumns, rows: [{ path: "/", views: 40, visitors: 12 }, { path: "/pricing", views: 90, visitors: 30 }] })
            : table({ table: "referrers", recipe_id: "posthog-web", data_source_name: "PostHog", columns: [{ key: "referrer", label: "Referrer", unit: "" }, { key: "views", label: "Views", unit: "views" }], rows: [{ referrer: "google.com", views: 70 }] }),
        ]),
      ),
    );
    renderAt("/operations/entity/c-web/pages", <EntityPages entity={web} env="production" />);
    await screen.findByRole("table", { name: "PostHog" });
    expect(apiMock.getComponentWeb).toHaveBeenCalledWith("c-web", "production", "pages");
    expect(bodyRows("PostHog").map((r) => within(r).getAllByRole("cell")[0].textContent)).toEqual(["/pricing", "/"]);
    expect(screen.getByRole("columnheader", { name: /Views/ })).not.toHaveTextContent("(views)");

    fireEvent.click(screen.getByRole("tab", { name: "Referrers" }));
    await waitFor(() => expect(apiMock.getComponentWeb).toHaveBeenCalledWith("c-web", "production", "referrers"));
    expect(await screen.findByText("google.com")).toBeInTheDocument();
    expect(screen.getByTestId("where")).toHaveTextContent("table=referrers");
  });

  it("names the tab's table when none is bound", async () => {
    apiMock.getComponentWeb.mockResolvedValue(tables("c-web", []));
    renderAt("/operations/entity/c-web/pages?table=countries", <EntityPages entity={web} env="production" />);
    expect(await screen.findByText("No Countries table yet")).toBeInTheDocument();
    expect(screen.getByText(/PostHog, Google Analytics 4 or Plausible/)).toBeInTheDocument();
  });
});

describe("EntityCrashes", () => {
  it("lists crash and ANR issues by users hit, linked to their console, then an iOS build's signatures", async () => {
    apiMock.getComponentCrashes.mockResolvedValue(
      tables("c-ios", [
        table({
          table: "crashes",
          recipe_id: "play-vitals",
          data_source_name: "Google Play vitals",
          columns: [
            { key: "issue", label: "Issue", unit: "" },
            { key: "type", label: "Type", unit: "" },
            { key: "cause", label: "Cause", unit: "" },
            { key: "users", label: "Users", unit: "users" },
            { key: "reports", label: "Reports", unit: "reports" },
            { key: "last_seen", label: "Last seen", unit: "" },
            { key: "link", label: "Link", unit: "" },
          ],
          rows: [
            { issue: "i-1", type: "anr", cause: "Input dispatching timed out", users: 12, reports: 40, last_seen: new Date().toISOString(), link: "https://play.google.com/console/i-1" },
            { issue: "i-2", type: "crash", cause: "NullPointerException", users: 85, reports: 120, last_seen: null, link: "javascript:alert(1)" },
          ],
        }),
        table({
          table: "signatures",
          recipe_id: "app-store-metrics",
          data_source_name: "App Store Connect",
          columns: [
            { key: "issue", label: "Signature id", unit: "" },
            { key: "type", label: "Type", unit: "" },
            { key: "cause", label: "Signature", unit: "" },
            { key: "weight", label: "Share", unit: "%" },
            { key: "build", label: "Build", unit: "" },
          ],
          rows: [{ issue: "sig-1", type: "hangs", cause: "main thread blocked", weight: 42.5, build: "812" }],
        }),
      ]),
    );
    renderAt("/operations/entity/c-ios/crashes", <EntityCrashes entity={ios} env="production" />);
    expect(await screen.findByText("Crash and ANR issues")).toBeInTheDocument();
    const rows = bodyRows("Google Play vitals");
    expect(within(rows[0]).getByText("i-2")).toBeInTheDocument();
    expect(within(rows[0]).getByText("Crash")).toBeInTheDocument();
    expect(within(rows[0]).queryByRole("link")).not.toBeInTheDocument();
    expect(within(rows[1]).getByRole("link", { name: /i-1/ })).toHaveAttribute("href", "https://play.google.com/console/i-1");
    expect(within(rows[1]).getByText("ANR")).toBeInTheDocument();
    expect(screen.queryByRole("columnheader", { name: /Link/ })).not.toBeInTheDocument();
    expect(screen.getByText("Diagnostic signatures")).toBeInTheDocument();
    expect(within(bodyRows("App Store Connect")[0]).getByText("Hang")).toBeInTheDocument();
  });

  it("points to Signals when nothing lists crashes", async () => {
    apiMock.getComponentCrashes.mockResolvedValue(tables("c-ios", []));
    renderAt("/operations/entity/c-ios/crashes", <EntityCrashes entity={ios} env="production" />);
    expect(await screen.findByText("No crash data yet")).toBeInTheDocument();
  });
});

describe("EntityTraces", () => {
  const T = Date.parse("2026-10-17T10:00:00Z");
  const at = (ms: number) => new Date(T + ms).toISOString();
  const spans: TraceSpan[] = [
    { id: "s-root", service: "web", name: "GET /checkout", start: at(0), duration_ms: 400, status: "ok", attributes: { "http.route": "/checkout", "resource.service.name": "web" } },
    { id: "s-api", parent_id: "s-root", service: "api", name: "POST /orders", start: at(20), duration_ms: 300, status: "error", status_message: "500", attributes: {} },
    { id: "s-db", parent_id: "s-api", service: "postgres", name: "INSERT orders", start: at(40), duration_ms: 120, status: "ok", attributes: {} },
  ];

  function traces(bound = true) {
    return {
      component_id: "c-api",
      environment: "production",
      bound,
      data_source_id: bound ? "ds-tempo" : null,
      traces: bound
        ? [
            { id: "aaaa1111", root_service: "web", root_name: "GET /checkout", start: at(0), duration_ms: 400, span_count: 3, error: true },
            { id: "bbbb2222", root_service: "web", root_name: "GET /", start: at(-60_000), duration_ms: 1200, span_count: 5, error: false },
          ]
        : [],
      error: null,
    };
  }

  it("searches the range with a minimum duration and opens a trace's waterfall", async () => {
    apiMock.getComponentTraces.mockResolvedValue(traces());
    apiMock.getOperationsTrace.mockResolvedValue({ trace: { id: "aaaa1111", spans, truncated: false }, component_id: "c-api", data_source_id: "ds-tempo" });
    renderAt("/operations/entity/c-api/traces", <EntityTraces entity={api} env="production" />);
    const list = await screen.findByRole("table", { name: "Traces" });
    expect(within(list).getAllByRole("row")).toHaveLength(3);
    expect(within(list).getByText("Error")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Slowest first" }));
    expect(within(within(list).getAllByRole("row")[1]).getByText("GET /")).toBeInTheDocument();

    fireEvent.change(screen.getByLabelText("Minimum duration (ms)"), { target: { value: "250" } });
    fireEvent.keyDown(screen.getByLabelText("Minimum duration (ms)"), { key: "Enter" });
    await waitFor(() => expect(apiMock.getComponentTraces).toHaveBeenLastCalledWith("c-api", expect.objectContaining({ minDurationMs: 250, env: "production" })));

    fireEvent.click(await screen.findByRole("link", { name: "Open trace GET /checkout" }));
    const waterfall = await screen.findByRole("list", { name: "Spans" });
    expect(apiMock.getOperationsTrace).toHaveBeenCalledWith("aaaa1111", { env: "production", componentId: "c-api" });
    const items = within(waterfall).getAllByRole("listitem");
    expect(items).toHaveLength(3);
    expect(within(items[1]).getByRole("button", { name: /api POST \/orders, starts at \+20 ms, takes 300 ms · failed/ })).toBeInTheDocument();

    fireEvent.click(within(items[0]).getByRole("button", { name: "Hide the spans under GET /checkout" }));
    expect(within(waterfall).getAllByRole("listitem")).toHaveLength(1);
    fireEvent.click(within(waterfall).getByRole("button", { name: "Show the spans under GET /checkout" }));

    fireEvent.click(within(waterfall).getByRole("button", { name: /GET \/checkout, starts at/ }));
    const panel = screen.getByRole("region", { name: "GET /checkout" });
    expect(within(panel).getByText("http.route")).toBeInTheDocument();
    expect(within(panel).getByText("service.name")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("tab", { name: "Table" }));
    const asTable = screen.getByRole("table", { name: "Spans as a table" });
    expect(within(asTable).getAllByRole("row")).toHaveLength(4);
    expect(within(asTable).getByText("INSERT orders")).toBeInTheDocument();
  });

  it("looks a trace up in every traces source when the component has no binding of its own", async () => {
    apiMock.getOperationsTrace
      .mockRejectedValueOnce(new ApiError("no traces binding", 422, "unbound"))
      .mockResolvedValueOnce({ trace: { id: "aaaa1111", spans, truncated: true }, component_id: "c-web", data_source_id: "ds-tempo" });
    renderAt("/operations/entity/c-api/traces?trace=AAAA1111", <EntityTraces entity={api} env="production" />);
    expect(await screen.findByRole("list", { name: "Spans" })).toBeInTheDocument();
    expect(apiMock.getOperationsTrace).toHaveBeenNthCalledWith(1, "aaaa1111", { env: "production", componentId: "c-api" });
    expect(apiMock.getOperationsTrace).toHaveBeenNthCalledWith(2, "aaaa1111", { env: "production" });
    expect(screen.getByText("Not every span is shown")).toBeInTheDocument();
    expect(await screen.findByRole("link", { name: "Open it on web" })).toHaveAttribute("href", "/operations/entity/c-web/traces?trace=aaaa1111");
    expect(apiMock.getComponentTraces).not.toHaveBeenCalled();
  });

  it("offers to bind traces when the component has no traces source", async () => {
    apiMock.getComponentTraces.mockResolvedValue(traces(false));
    renderAt("/operations/entity/c-api/traces", <EntityTraces entity={api} env="production" />);
    expect(await screen.findByText("Connect a traces source")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Bind traces" })).toBeInTheDocument();
  });

  it("says a trace is not held, with the way back", async () => {
    apiMock.getOperationsTrace.mockRejectedValue(new ApiError("not found", 404, "not_found"));
    renderAt("/operations/entity/c-api/traces?trace=abc", <EntityTraces entity={api} env="production" />);
    expect(await screen.findByText("Trace not found")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Back to traces" })).toHaveAttribute("href", "/operations/entity/c-api/traces");
  });
});
