import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter, Route, Routes, useLocation } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { ApiError } from "@/api";
import { I18nProvider } from "@/hooks/useI18n";

const { telemetryGet } = vi.hoisted(() => ({ telemetryGet: vi.fn() }));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return { ...actual, api: { ...actual.api, telemetryGet } };
});
vi.mock("@/components/operations/charts/TimeSeriesChart", () => ({
  TimeSeriesChart: ({ ariaLabel }: { ariaLabel: string }) => <div role="img" aria-label={ariaLabel} />,
}));

import { OpsProvider } from "@/components/operations/shell/OpsContext";
import TelemetryDeploysPage from "@/pages/ops/telemetry/TelemetryDeploysPage";
import TelemetryEndpointPage from "@/pages/ops/telemetry/TelemetryEndpointPage";
import TelemetryErrorsPage from "@/pages/ops/telemetry/TelemetryErrorsPage";
import TelemetryIncidentsPage from "@/pages/ops/telemetry/TelemetryIncidentsPage";
import TelemetryLogsPage from "@/pages/ops/telemetry/TelemetryLogsPage";
import TelemetryMonitorsPage from "@/pages/ops/telemetry/TelemetryMonitorsPage";
import TelemetryServicePage from "@/pages/ops/telemetry/TelemetryServicePage";
import TelemetryServicesPage from "@/pages/ops/telemetry/TelemetryServicesPage";
import TelemetrySloPage from "@/pages/ops/telemetry/TelemetrySloPage";
import TelemetryTracePage from "@/pages/ops/telemetry/TelemetryTracePage";
import TelemetryTracesPage from "@/pages/ops/telemetry/TelemetryTracesPage";

const NOW = Date.now();

const services = [
  { id: "s-ok", name: "billing", type: "http", language: "go", version: "v1", rpm: 120, errorRate: 0.001, p95: 80, prevRpm: 100, prevErrorRate: 0.001, prevP95: 70, status: "ok", instances: 2, spark: [1, 2, 3] },
  { id: "s-bad", name: "checkout", type: "http", language: "ts", version: "v9", rpm: 40, errorRate: 0.12, p95: 2400, prevRpm: 40, prevErrorRate: 0.01, prevP95: 300, status: "critical", instances: 3, spark: [3, 2, 1] },
  { id: "s-w", name: "mailer", type: "worker", language: "py", version: "", rpm: 5, errorRate: 0, p95: 10, prevRpm: 5, prevErrorRate: 0, prevP95: 10, status: "warning", instances: 1 },
];

const responses: Record<string, unknown> = {};

function Where() {
  const { pathname, search } = useLocation();
  return <p data-testid="where">{`${pathname}${search}`}</p>;
}

function renderAt(path: string, routePath: string, element: React.ReactNode) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <I18nProvider>
        <OpsProvider>
          <Routes>
            <Route path={routePath} element={element} />
            <Route path="*" element={<p>elsewhere</p>} />
          </Routes>
          <Where />
        </OpsProvider>
      </I18nProvider>
    </MemoryRouter>,
  );
}

function ok(value: unknown) {
  return Promise.resolve(value);
}

beforeEach(() => {
  for (const k of Object.keys(responses)) delete responses[k];
  telemetryGet.mockReset();
  telemetryGet.mockImplementation((path: string) => {
    if (path in responses) return ok(responses[path]);
    if (path === "environments") return ok(["production", "staging"]);
    if (path === "services") return ok(services);
    return Promise.reject(new Error(`unexpected ${path}`));
  });
});

describe("services", () => {
  it("lists the worst service first and filters by name", async () => {
    renderAt("/operations/telemetry/services", "/operations/telemetry/services", <TelemetryServicesPage />);
    const table = await screen.findByRole("table", { name: "Services" });
    const rows = within(table).getAllByRole("row").slice(1);
    expect(rows.map((r) => within(r).getAllByRole("cell")[1].textContent)).toEqual([
      expect.stringContaining("checkout"),
      expect.stringContaining("mailer"),
      expect.stringContaining("billing"),
    ]);
    expect(within(rows[0]).getByText("12%")).toBeInTheDocument();
    expect(within(rows[0]).getByText("2.4 s")).toBeInTheDocument();
    fireEvent.change(screen.getByLabelText("Search services"), { target: { value: "bill" } });
    await waitFor(() => expect(screen.getByTestId("where")).toHaveTextContent("q=bill"));
    expect(within(screen.getByRole("table", { name: "Services" })).getAllByRole("row")).toHaveLength(2);
  });

  it("asks the product for the window the console picked and the chosen environment", async () => {
    renderAt("/operations/telemetry/services?range=6h&tenv=staging", "/operations/telemetry/services", <TelemetryServicesPage />);
    await screen.findByRole("table", { name: "Services" });
    expect(telemetryGet).toHaveBeenCalledWith("services", { range: "6h", env: "staging" });
  });

  it("explains an empty answer and a failed one", async () => {
    responses.services = [];
    const first = renderAt("/operations/telemetry/services", "/operations/telemetry/services", <TelemetryServicesPage />);
    expect(await screen.findByText("No services reporting yet")).toBeInTheDocument();
    first.unmount();
    delete responses.services;
    telemetryGet.mockImplementation((path: string) => (path === "services" ? Promise.reject(new Error("upstream 502")) : ok([])));
    renderAt("/operations/telemetry/services", "/operations/telemetry/services", <TelemetryServicesPage />);
    expect(await screen.findByText("Could not load telemetry data")).toBeInTheDocument();
    expect(screen.getByText("upstream 502")).toBeInTheDocument();
  });

  it("shows the connect guidance when the server lost its configuration", async () => {
    telemetryGet.mockImplementation((path: string) =>
      path === "services" ? Promise.reject(new ApiError("not configured", 409, "telemetry_not_configured")) : ok([]),
    );
    renderAt("/operations/telemetry/services", "/operations/telemetry/services", <TelemetryServicesPage />);
    expect(await screen.findByText("Connect TaskTrooper Telemetry")).toBeInTheDocument();
  });
});

describe("service detail", () => {
  it("reads the service, its endpoints and deploys, and links an endpoint through", async () => {
    responses["services/checkout"] = services[1];
    responses["services/checkout/endpoints"] = [
      { id: "post-pay", serviceId: "checkout", method: "POST", route: "/pay", rpm: 10, errorRate: 0.2, p50: 100, p95: 2000, p99: 3000, monitored: true, criticality: "critical" },
    ];
    responses["services/checkout/deploys"] = [{ service: "checkout", env: "production", version: "v9", ts: NOW - 1000, source: "ci" }];
    responses["services/checkout/series"] = { timestamps: [NOW - 60_000, NOW], series: [{ id: "rpm", label: "rpm", data: [1, 2] }] };
    renderAt("/operations/telemetry/services/checkout", "/operations/telemetry/services/:name", <TelemetryServicePage />);
    expect(await screen.findByRole("heading", { name: "checkout" })).toBeInTheDocument();
    const link = await screen.findByRole("link", { name: "POST /pay" });
    expect(link).toHaveAttribute("href", "/operations/telemetry/endpoints/checkout/post-pay");
    expect(screen.getByRole("img", { name: "Throughput" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Traces" })).toHaveAttribute("href", expect.stringContaining("service=checkout"));
  });
});

describe("endpoint detail", () => {
  it("explains a regression by dependency", async () => {
    const base = "services/checkout/endpoints/post-pay";
    responses[base] = { id: "post-pay", serviceId: "checkout", method: "POST", route: "/pay", rpm: 10, errorRate: 0.2, p50: 100, p95: 2000, p99: 3000, monitored: false, criticality: "low" };
    responses[`${base}/series`] = { timestamps: [], series: [] };
    responses[`${base}/rca`] = {
      service: "checkout",
      endpoint: "POST /pay",
      current: { requests: 100, rpm: 10, avgMs: 900, p50: 100, p95: 2000, errorRate: 0.2 },
      baseline: { requests: 90, rpm: 9, avgMs: 400, p50: 90, p95: 500, errorRate: 0.01 },
      ownMs: [100, 90],
      deltaAvgMs: 500,
      children: [{ dep: "payments-db", kind: "db", deltaMsPerReq: 480, share: 0.96, current: {}, baseline: {} }],
    };
    responses["services/checkout/deploys"] = [];
    renderAt("/operations/telemetry/endpoints/checkout/post-pay", "/operations/telemetry/endpoints/:service/:endpoint", <TelemetryEndpointPage />);
    expect(await screen.findByText("Requests take 500 ms longer on average than in the baseline.")).toBeInTheDocument();
    expect(screen.getByText("payments-db")).toBeInTheDocument();
    expect(telemetryGet).toHaveBeenCalledWith(`${base}/rca`, expect.objectContaining({ baseline: "prev" }));
  });
});

describe("traces", () => {
  it("filters the search and opens a waterfall", async () => {
    responses.traces = [{ id: "abc123", ts: NOW, dur: 1500, status: 500, service: "checkout", method: "POST", route: "/pay", isError: true }];
    responses["traces/abc123"] = {
      id: "abc123",
      ts: NOW,
      dur: 1500,
      errors: 1,
      services: ["checkout", "payments-db"],
      spans: [
        { spanId: "a", service: "checkout", name: "POST /pay", kind: "server", start: 0, dur: 1500, status: 500, failed: true },
        { spanId: "b", parentSpanId: "a", service: "payments-db", name: "INSERT payments", kind: "client", start: 100, dur: 1200, status: 0, failed: false },
      ],
    };
    const first = renderAt("/operations/telemetry/traces?service=checkout&errors=1&sort=slowest", "/operations/telemetry/traces", <TelemetryTracesPage />);
    const link = await screen.findByRole("link", { name: "POST /pay" });
    expect(link).toHaveAttribute("href", "/operations/telemetry/traces/abc123");
    expect(telemetryGet).toHaveBeenCalledWith("traces", expect.objectContaining({ service: "checkout", errors: true, sort: "slowest", limit: 100 }));
    first.unmount();
    renderAt("/operations/telemetry/traces/abc123", "/operations/telemetry/traces/:traceId", <TelemetryTracePage />);
    expect(await screen.findByText("INSERT payments")).toBeInTheDocument();
    expect(screen.getAllByText("payments-db").length).toBeGreaterThan(0);
  });
});

describe("logs", () => {
  it("shows lines with a link to their trace and pages back through the cursor", async () => {
    const line = (i: number) => ({ id: `l${i}`, ts: NOW - i * 1000, level: i === 0 ? "error" : "info", service: "checkout", message: `message ${i}`, traceId: i === 0 ? "abc123" : null });
    const first = Array.from({ length: 200 }, (_, i) => line(i));
    telemetryGet.mockImplementation((path: string, query?: Record<string, unknown>) => {
      if (path === "environments") return ok([]);
      if (path === "services") return ok(services);
      if (path === "logs/series") return ok({ timestamps: [NOW - 60_000, NOW], series: [{ id: "error", label: "error", data: [1, 0] }] });
      if (path === "logs") {
        if (query?.before) return ok({ lines: [line(500)], nextBefore: 0, levels: {}, services: {} });
        return ok({ lines: first, nextBefore: 777, levels: { error: 1, info: 199 }, services: { checkout: 200 } });
      }
      return Promise.reject(new Error(`unexpected ${path}`));
    });
    renderAt("/operations/telemetry/logs", "/operations/telemetry/logs", <TelemetryLogsPage />);
    expect(await screen.findByText("message 0")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Load older lines" }));
    expect(await screen.findByText("message 500")).toBeInTheDocument();
    expect(telemetryGet).toHaveBeenCalledWith("logs", expect.objectContaining({ before: 777 }));
    await waitFor(() => expect(screen.queryByRole("button", { name: "Load older lines" })).not.toBeInTheDocument());
  });
});

describe("errors", () => {
  it("opens the stack of an error issue", async () => {
    responses.errors = [{ fingerprint: "fp1", service: "checkout", type: "TypeError", message: "x is undefined", route: "/pay", firstSeen: NOW - 5000, lastSeen: NOW, count: 42, status: "open", sampleTraceId: "abc123" }];
    responses["errors/fp1"] = { ...(responses.errors as object[])[0], release: "v9", platform: "node", stack: "", symbolicated: true, frames: [{ fn: "charge", file: "pay.ts", line: 12, col: 3, inApp: true }] };
    renderAt("/operations/telemetry/errors", "/operations/telemetry/errors", <TelemetryErrorsPage />);
    fireEvent.click(await screen.findByText("TypeError"));
    expect(await screen.findByText("charge")).toBeInTheDocument();
    expect(screen.getByText("pay.ts:12:3")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Open a trace of the latest occurrence" })).toHaveAttribute("href", "/operations/telemetry/traces/abc123");
  });
});

describe("incidents", () => {
  it("asks for open incidents by default and shows the timeline of one", async () => {
    responses.incidents = [{ id: "i1", monitorId: "m1", title: "p95 above 2s", severity: "critical", state: "firing", startedAt: NOW - 60_000, value: "2400 ms", service: "checkout", endpoint: "POST /pay" }];
    responses["incidents/i1/events"] = [{ id: "e1", ts: NOW - 60_000, kind: "fired", data: { value: 2400 } }];
    renderAt("/operations/telemetry/incidents", "/operations/telemetry/incidents", <TelemetryIncidentsPage />);
    fireEvent.click(await screen.findByText("p95 above 2s"));
    expect(telemetryGet).toHaveBeenCalledWith("incidents", { state: "open" });
    expect(await screen.findByText("fired")).toBeInTheDocument();
    expect(screen.getByText(/"value": 2400/)).toBeInTheDocument();
  });
});

describe("monitors, deploys and SLOs", () => {
  it("lists monitors firing first", async () => {
    responses.monitors = [
      { id: "m1", name: "Quiet monitor", kind: "latency", target: "billing", condition: "p95 > 1s", severity: "info", state: "ok", source: "manual", lastChange: NOW, channels: [], status: "active" },
      { id: "m2", name: "Loud monitor", kind: "error_new", target: "checkout", condition: "new error", severity: "critical", state: "firing", source: "code", lastChange: NOW, channels: [], status: "active" },
    ];
    renderAt("/operations/telemetry/monitors", "/operations/telemetry/monitors", <TelemetryMonitorsPage />);
    const table = await screen.findByRole("table", { name: "Monitors" });
    const names = within(table).getAllByRole("row").slice(1).map((r) => within(r).getAllByRole("cell")[1].textContent);
    expect(names[0]).toContain("Loud monitor");
    expect(names[1]).toContain("Quiet monitor");
  });

  it("lists deploys newest first and only links web addresses", async () => {
    responses.deploys = [
      { service: "billing", env: "production", version: "v1", ts: NOW - 5000, source: "ci", url: "javascript:alert(1)" },
      { service: "checkout", env: "production", version: "v9", ts: NOW, source: "vercel", url: "https://ci.example.com/run/9" },
    ];
    renderAt("/operations/telemetry/deploys", "/operations/telemetry/deploys", <TelemetryDeploysPage />);
    const table = await screen.findByRole("table", { name: "Deploys" });
    const rows = within(table).getAllByRole("row").slice(1);
    expect(rows[0]).toHaveTextContent("v9");
    expect(within(rows[0]).getByRole("link")).toHaveAttribute("href", "https://ci.example.com/run/9");
    expect(within(rows[1]).queryByRole("link")).not.toBeInTheDocument();
  });

  it("shows error budget and burn rate", async () => {
    responses.kpis = [{ id: "k1", name: "Checkout succeeds", description: "", owner: "", target: { kind: "endpoint", serviceId: "checkout", ref: "POST /pay" }, goodWhen: "status < 500", objective: 0.99, window: "28d", current: 0.98, windowGood: 0.985, budgetRemaining: 0.25, burnRate1h: 4.5, burnRate6h: 2, status: "warning" }];
    renderAt("/operations/telemetry/slo", "/operations/telemetry/slo", <TelemetrySloPage />);
    expect(await screen.findByText("Checkout succeeds")).toBeInTheDocument();
    expect(screen.getByRole("img", { name: "25% of the error budget left" })).toBeInTheDocument();
    expect(screen.getByText("4.5× (1h) · 2× (6h)")).toBeInTheDocument();
  });
});
