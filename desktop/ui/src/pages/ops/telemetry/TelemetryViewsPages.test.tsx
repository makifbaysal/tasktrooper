import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter, Route, Routes, useLocation } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
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
import TelemetryAppPage from "@/pages/ops/telemetry/TelemetryAppPage";
import TelemetryAppsPage from "@/pages/ops/telemetry/TelemetryAppsPage";
import TelemetryDatabasesPage from "@/pages/ops/telemetry/TelemetryDatabasesPage";
import TelemetryFunctionPage from "@/pages/ops/telemetry/TelemetryFunctionPage";
import TelemetryHostPage from "@/pages/ops/telemetry/TelemetryHostPage";
import TelemetryHostsPage from "@/pages/ops/telemetry/TelemetryHostsPage";
import TelemetryKubernetesPage from "@/pages/ops/telemetry/TelemetryKubernetesPage";
import TelemetryLlmPage from "@/pages/ops/telemetry/TelemetryLlmPage";
import TelemetryLoadTestRunPage from "@/pages/ops/telemetry/TelemetryLoadTestRunPage";
import TelemetryLoadTestsPage from "@/pages/ops/telemetry/TelemetryLoadTestsPage";
import TelemetryOverviewPage from "@/pages/ops/telemetry/TelemetryOverviewPage";
import TelemetryPodPage from "@/pages/ops/telemetry/TelemetryPodPage";
import TelemetryProfilingPage from "@/pages/ops/telemetry/TelemetryProfilingPage";
import TelemetryQueuesPage from "@/pages/ops/telemetry/TelemetryQueuesPage";
import TelemetryReplayPage from "@/pages/ops/telemetry/TelemetryReplayPage";
import TelemetryReplaysPage from "@/pages/ops/telemetry/TelemetryReplaysPage";
import TelemetryServerlessPage from "@/pages/ops/telemetry/TelemetryServerlessPage";
import TelemetryServiceMapPage from "@/pages/ops/telemetry/TelemetryServiceMapPage";

const NOW = Date.now();

const services = [
  { id: "s-ok", name: "billing", type: "http", language: "go", version: "v1", rpm: 120, errorRate: 0.001, p95: 80, prevRpm: 100, prevErrorRate: 0.001, prevP95: 70, status: "ok", instances: 2 },
  { id: "s-bad", name: "checkout", type: "http", language: "ts", version: "v9", rpm: 40, errorRate: 0.12, p95: 2400, prevRpm: 40, prevErrorRate: 0.01, prevP95: 300, status: "critical", instances: 3 },
  { id: "s-web", name: "shop-web", type: "web", language: "js", version: "1.2.0", rpm: 300, errorRate: 0.02, p95: 0, prevRpm: 0, prevErrorRate: 0, prevP95: 0, status: "warning", instances: 0 },
  { id: "s-ios", name: "shop-ios", type: "mobile", language: "swift", version: "3.1", rpm: 50, errorRate: 0.001, p95: 0, prevRpm: 0, prevErrorRate: 0, prevP95: 0, status: "ok", instances: 0 },
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

const set = (ids: string[]) => ({ timestamps: [NOW - 60_000, NOW], series: ids.map((id) => ({ id, label: id, data: [1, 2] })) });

beforeEach(() => {
  for (const k of Object.keys(responses)) delete responses[k];
  telemetryGet.mockReset();
  telemetryGet.mockImplementation((path: string) => {
    if (path in responses) return Promise.resolve(responses[path]);
    if (path === "environments") return Promise.resolve(["production"]);
    if (path === "services") return Promise.resolve(services);
    return Promise.reject(new Error(`unexpected ${path}`));
  });
});

describe("overview", () => {
  it("sums the project up and points at what hurts", async () => {
    responses.incidents = [{ id: "i1", monitorId: "m1", title: "p95 above 2s", severity: "critical", state: "firing", startedAt: NOW - 60_000, value: "2.4 s", service: "checkout" }];
    responses.kpis = [{ id: "k1", name: "Checkout succeeds", description: "", owner: "", target: { kind: "endpoint", serviceId: "checkout", ref: "POST /pay" }, goodWhen: "", objective: 0.99, window: "28d", current: 0.9, windowGood: 0.95, budgetRemaining: 0.1, burnRate1h: 9, burnRate6h: 4, status: "critical" }];
    responses.deploys = [{ service: "checkout", env: "production", version: "v9", ts: NOW - 5000, source: "ci" }];
    responses["overview/series"] = set(["rpm", "errorRate", "p95"]);
    renderAt("/operations/telemetry/overview?range=1h", "/operations/telemetry/overview", <TelemetryOverviewPage />);
    expect(await screen.findByText("p95 above 2s")).toBeInTheDocument();
    expect(await screen.findByRole("link", { name: "checkout" })).toHaveAttribute("href", "/operations/telemetry/services/checkout?range=1h");
    expect(screen.queryByRole("link", { name: "billing" })).not.toBeInTheDocument();
    expect(await screen.findByText("Checkout succeeds")).toBeInTheDocument();
    expect(await screen.findByText("v9")).toBeInTheDocument();
    expect(telemetryGet).toHaveBeenCalledWith("overview/series", { range: "1h", env: undefined });
    expect(telemetryGet).toHaveBeenCalledWith("incidents", { state: "open" });
  });
});

describe("service map", () => {
  it("draws callers, services and stores, links a service and lists its calls", async () => {
    responses["service-map"] = {
      nodes: [
        { id: "shop-web", name: "shop-web", tier: "client", kind: "web", status: "ok", rpm: 10, errorRate: 0, p95: 0 },
        { id: "checkout", name: "checkout", tier: "service", kind: "http", status: "critical", rpm: 40, errorRate: 0.12, p95: 2400 },
        { id: "dep:postgres · payments", name: "payments", tier: "data", kind: "db", system: "postgres", status: "ok", rpm: 80, errorRate: 0, p95: 4 },
      ],
      edges: [
        { id: "e1", from: "shop-web", to: "checkout", callsPerMin: 10, errorRate: 0.1, p95: 2400, status: "critical" },
        { id: "e2", from: "checkout", to: "dep:postgres · payments", callsPerMin: 80, errorRate: 0, p95: 4, status: "ok" },
      ],
    };
    renderAt("/operations/telemetry/map", "/operations/telemetry/map", <TelemetryServiceMapPage />);
    expect(await screen.findByRole("link", { name: /checkout/ })).toHaveAttribute("href", "/operations/telemetry/services/checkout");
    expect(screen.getByText("Data stores")).toBeInTheDocument();
    const table = screen.getByRole("table", { name: "Calls between services" });
    const rows = within(table).getAllByRole("row").slice(1);
    expect(rows[0]).toHaveTextContent("payments");
    expect(rows[1]).toHaveTextContent("shop-web");
  });
});

describe("hosts", () => {
  const host = { id: "web-1", hostId: "h1", os: "linux", arch: "amd64", cluster: "", node: "", cpu: 0.92, cpuCores: 4, memory: 0.5, memoryBytes: 4 * 1024 ** 3, disk: 0.3, netIn: 1024, netOut: 2048, load1: 1.5, uptimeS: 7200, containers: 2, lastSeen: NOW, status: "critical", spark: { cpu: [1, 2], memory: [1, 2] }, gpus: [] };

  it("lists hosts with their usage and opens one", async () => {
    responses["infra/hosts"] = [{ ...host, id: "idle-1", cpu: 0.1, status: "ok" }, host];
    renderAt("/operations/telemetry/infra/hosts", "/operations/telemetry/infra/hosts", <TelemetryHostsPage />);
    const table = await screen.findByRole("table", { name: "Hosts" });
    const rows = within(table).getAllByRole("row").slice(1);
    expect(within(rows[0]).getByRole("link", { name: "web-1" })).toHaveAttribute("href", "/operations/telemetry/infra/hosts/web-1");
    expect(within(rows[0]).getByText("92% of 4 cores")).toBeInTheDocument();
  });

  it("shows a host's charts, containers and processes", async () => {
    responses["infra/hosts/web-1"] = {
      ...host,
      series: { cpu: set(["cpu"]), memory: set(["memory"]), disk: set(["disk"]), network: set(["in", "out"]) },
      containerList: [{ id: "c1", name: "api", image: "acme/api:1", host: "web-1", namespace: "", pod: "", podUid: "", service: "checkout", cpu: 0.5, cpuLimit: 1, memoryBytes: 1024 ** 3, memoryLimit: 2 * 1024 ** 3, restarts: 3, throttled: 0, status: "ok" }],
      processes: [{ pid: 42, exe: "node", command: "node server.js", containerId: "c1", cpu: 0.4, memoryBytes: 1024 ** 2, threads: 8, fds: 20 }],
      services: ["checkout"],
    };
    renderAt("/operations/telemetry/infra/hosts/web-1", "/operations/telemetry/infra/hosts/:host", <TelemetryHostPage />);
    expect(await screen.findByRole("heading", { name: "web-1" })).toBeInTheDocument();
    expect(screen.getByRole("img", { name: "Network" })).toBeInTheDocument();
    expect(screen.getByText("acme/api:1")).toBeInTheDocument();
    expect(screen.getByText("node server.js")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "checkout" })).toHaveAttribute("href", "/operations/telemetry/services/checkout");
  });
});

describe("kubernetes", () => {
  const pod = { uid: "u1", name: "checkout-7d9", namespace: "shop", cluster: "prod", node: "n1", phase: "Running", reason: "CrashLoopBackOff", ready: false, restarts: 9, restartsInRange: 4, cpu: 0.2, cpuRequest: 0.25, cpuLimit: 0.5, memoryBytes: 1024 ** 2, memoryRequest: 0, memoryLimit: 0, workloadKind: "Deployment", workload: "checkout", service: "checkout", status: "critical", containers: [] };

  it("lists clusters and reads pods of the picked cluster on their tab", async () => {
    responses["infra/k8s/clusters"] = [{ name: "prod", nodes: 3, nodesReady: 3, pods: 40, podsRunning: 38, podsFailing: 2, namespaces: 5, workloads: 12, cpuUsed: 6, cpuAllocatable: 12, memoryUsed: 1024 ** 3, memoryAllocatable: 8 * 1024 ** 3, warningEvents: 7, lastSeen: NOW, status: "warning" }];
    responses["infra/k8s/pods"] = [pod];
    renderAt("/operations/telemetry/infra/kubernetes?cluster=prod", "/operations/telemetry/infra/kubernetes", <TelemetryKubernetesPage />);
    expect(await screen.findByRole("link", { name: "prod" })).toHaveAttribute("href", expect.stringMatching(/^\/operations\/telemetry\/infra\/kubernetes\/clusters\/prod(\?|$)/));
    expect(screen.getByText("2 failing")).toBeInTheDocument();
    expect(telemetryGet).not.toHaveBeenCalledWith("infra/k8s/pods", expect.anything());
    fireEvent.mouseDown(screen.getByRole("tab", { name: "Pods" }));
    fireEvent.click(screen.getByRole("tab", { name: "Pods" }));
    expect(await screen.findByRole("link", { name: "checkout-7d9" })).toBeInTheDocument();
    expect(screen.getByText("CrashLoopBackOff")).toBeInTheDocument();
    expect(telemetryGet).toHaveBeenCalledWith("infra/k8s/pods", expect.objectContaining({ cluster: "prod" }));
  });

  it("shows a pod with its restarts and warning events", async () => {
    responses["infra/k8s/pods/u1"] = {
      ...pod,
      containers: [{ name: "app", image: "acme/checkout:9", cpu: 0.2, memoryBytes: 1024 ** 2, restarts: 9 }],
      series: { cpu: set(["usage", "request", "limit"]), memory: set(["usage"]), restarts: set(["restarts"]) },
      events: [{ ts: NOW, type: "Warning", reason: "BackOff", message: "Back-off restarting failed container", objectKind: "Pod", objectName: "checkout-7d9", namespace: "shop", cluster: "prod", count: 12 }],
    };
    renderAt("/operations/telemetry/infra/kubernetes/pods/u1", "/operations/telemetry/infra/kubernetes/pods/:uid", <TelemetryPodPage />);
    expect(await screen.findByRole("heading", { name: "checkout-7d9" })).toBeInTheDocument();
    expect(screen.getByText("9 in total, 4 in this window")).toBeInTheDocument();
    expect(screen.getByText("Back-off restarting failed container")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "prod" })).toHaveAttribute("href", "/operations/telemetry/infra/kubernetes/clusters/prod");
  });
});

describe("databases and queues", () => {
  it("opens a database with its top queries", async () => {
    responses["infra/databases"] = [
      { id: "pg-main", name: "pg-main", engine: "postgres", version: "16", host: "db.internal", port: 5432, env: "production", instanceClass: "", source: "agent", status: "warning", cpu: 0.7, memory: 0.5, disk: 0.4, diskGb: 100, connections: 90, maxConnections: 100, p95: 40, prevP95: 10, slowPerMin: 3, opsPerSec: 500, hitRatio: 0.99, nodes: [], problem: null },
    ];
    responses["infra/databases/pg-main/queries"] = [{ id: "q1", text: "SELECT * FROM orders WHERE id = $1", serviceId: "checkout", endpoint: "POST /pay", perCall: 2, avgMs: 12, p95: 40, rows: 1, callsPerMin: 80, share: 0.6 }];
    renderAt("/operations/telemetry/infra/databases", "/operations/telemetry/infra/databases", <TelemetryDatabasesPage />);
    fireEvent.click(await screen.findByText("pg-main"));
    expect(await screen.findByText("SELECT * FROM orders WHERE id = $1")).toBeInTheDocument();
    expect(screen.getByText("90/100")).toBeInTheDocument();
    await waitFor(() => expect(screen.getByTestId("where")).toHaveTextContent("db=pg-main"));
  });

  it("lists consumer groups by lag and opens one's lag chart", async () => {
    responses["infra/clusters"] = [{ id: "kafka-1", name: "kafka-main", kind: "kafka", version: "3.7", endpoint: "kafka:9092", source: "agent", status: "ok", totalLag: 1200, topics: 4, groups: 2, nodes: [] }];
    responses["infra/groups"] = [
      { clusterId: "kafka-1", id: "billing-consumer", members: 2, state: "Stable", rebalancesPerHour: 0, trend: "flat", reads: [{ topic: "invoices", partitionLag: [1, 2], consumePerSec: 10 }], lag: 3, health: "ok" },
      { clusterId: "kafka-1", id: "orders-consumer", serviceId: "checkout", members: 1, state: "Stable", rebalancesPerHour: 2, trend: "growing", reads: [{ topic: "orders", partitionLag: [600, 597], consumePerSec: 4 }], lag: 1197, health: "critical" },
    ];
    responses["infra/clusters/kafka-1/groups/orders-consumer/lag"] = set(["orders"]);
    renderAt("/operations/telemetry/infra/queues", "/operations/telemetry/infra/queues", <TelemetryQueuesPage />);
    const table = await screen.findByRole("table", { name: "Consumer groups" });
    const rows = within(table).getAllByRole("row").slice(1);
    expect(rows[0]).toHaveTextContent("orders-consumer");
    expect(rows[0]).toHaveTextContent("kafka-main");
    fireEvent.click(within(rows[0]).getByText("orders-consumer"));
    expect(await screen.findByRole("img", { name: "Lag" })).toBeInTheDocument();
    expect(telemetryGet).toHaveBeenCalledWith("infra/clusters/kafka-1/groups/orders-consumer/lag", expect.objectContaining({ range: "1h" }));
  });
});

describe("serverless", () => {
  const fn = { id: "aws-eu-west-1-resize", slug: "aws-eu-west-1-resize", name: "resize-image", provider: "aws", app: "", region: "eu-west-1", runtime: "nodejs20.x", memoryMb: 512, timeoutS: 30, arch: "arm64", triggers: ["s3"], service: "media", invocations: 12000, errors: 120, errorRate: 0.01, throttles: 4, p95Ms: 800, p99Ms: 1500, coldStarts: 30, coldStartRate: 0.0025, concurrencyMax: 20, costUsd: 1.25, status: "warning", spark: [1, 2, 3] };

  it("sums the functions up and filters by provider", async () => {
    responses["serverless/summary"] = { functions: 1, invocations: 12000, errors: 120, errorRate: 0.01, throttles: 4, coldStarts: 30, costUsd: 1.25, byProvider: [{ provider: "aws", functions: 1, invocations: 12000, costUsd: 1.25 }] };
    responses["serverless/functions"] = [fn];
    renderAt("/operations/telemetry/infra/serverless", "/operations/telemetry/infra/serverless", <TelemetryServerlessPage />);
    expect(await screen.findByRole("link", { name: "resize-image" })).toHaveAttribute("href", "/operations/telemetry/infra/serverless/aws-eu-west-1-resize");
    expect(await screen.findByText("$1.25", { selector: "p" })).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: /AWS Lambda/ }));
    await waitFor(() => expect(telemetryGet).toHaveBeenCalledWith("serverless/functions", expect.objectContaining({ provider: "aws" })));
  });

  it("shows a function's configuration and latest errors", async () => {
    responses["serverless/functions/aws-eu-west-1-resize"] = { ...fn, series: { invocations: set(["invocations"]) }, recentErrors: [{ ts: NOW, traceId: "t1", message: "Task timed out" }], integration: null };
    renderAt("/operations/telemetry/infra/serverless/aws-eu-west-1-resize", "/operations/telemetry/infra/serverless/:id", <TelemetryFunctionPage />);
    expect(await screen.findByRole("heading", { name: "resize-image" })).toBeInTheDocument();
    expect(screen.getByText("512 MB")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Task timed out" })).toHaveAttribute("href", "/operations/telemetry/traces/t1");
  });
});

describe("web and mobile apps", () => {
  it("lists only the client apps", async () => {
    renderAt("/operations/telemetry/apps", "/operations/telemetry/apps", <TelemetryAppsPage />);
    const table = await screen.findByRole("table", { name: "Web and mobile apps" });
    const names = within(table).getAllByRole("link").map((a) => a.textContent);
    expect(names).toEqual(["shop-web", "shop-ios"]);
  });

  it("shows a mobile app's vitals and its sessions with a replay link", async () => {
    responses["services/shop-ios"] = services[3];
    responses["apps/shop-ios/summary"] = {
      activeUsers: 120,
      prevActiveUsers: 100,
      sessions: 300,
      pageViewsPerMin: 40,
      avgSessionSec: 95,
      errorRate: 0.004,
      prevErrorRate: 0.002,
      vitals: [{ id: "cold", label: "Cold start", value: 4500, good: 2000, poor: 4000, unit: "ms" }],
    };
    responses["apps/shop-ios/series"] = set(["views", "users", "sessions", "errorRate"]);
    responses["apps/shop-ios/pages"] = [];
    responses["apps/shop-ios/sessions"] = [{ id: "sess-1", user: "ada@example.com", identified: true, country: "TR", device: "iPhone 15", startedAt: NOW, duration: 90, pages: 4, errors: 1, entry: "Home", exit: "Cart", hasReplay: true }];
    renderAt("/operations/telemetry/apps/shop-ios", "/operations/telemetry/apps/:app", <TelemetryAppPage />);
    expect(await screen.findByText("Crash rate", { selector: "p" })).toBeInTheDocument();
    expect(screen.getByText("Cold start")).toBeInTheDocument();
    expect(screen.getByText("Poor")).toBeInTheDocument();
    expect(await screen.findByRole("tab", { name: "App starts" })).toBeInTheDocument();
    fireEvent.mouseDown(screen.getByRole("tab", { name: "Sessions" }));
    fireEvent.click(screen.getByRole("tab", { name: "Sessions" }));
    expect(await screen.findByRole("link", { name: "Open" })).toHaveAttribute("href", "/operations/telemetry/replays/sess-1");
  });
});

describe("session replays", () => {
  const replay = { sessionId: "sess-1", app: "shop-web", platform: "web", format: "rrweb", startedAt: NOW - 120_000, durationMs: 95_000, segments: 2, user: { id: "ada", anonymousId: "anon-1" }, browser: "Chrome", firstView: "/", views: 5, errors: 2, clicks: 30, rageClicks: 3 };

  it("filters recorded sessions and opens one", async () => {
    responses.replays = { replays: [replay] };
    renderAt("/operations/telemetry/replays?app=shop-web&errors=1", "/operations/telemetry/replays", <TelemetryReplaysPage />);
    expect(await screen.findByText("3 rage clicks")).toBeInTheDocument();
    expect(telemetryGet).toHaveBeenCalledWith("replays", expect.objectContaining({ app: "shop-web", hasErrors: true }));
    expect(screen.getAllByRole("link")[0]).toHaveAttribute("href", expect.stringMatching(/^\/operations\/telemetry\/replays\/sess-1(\?|$)/));
  });

  it("shows a session as its timeline and says the player is not here", async () => {
    responses["replays/sess-1"] = {
      summary: replay,
      segments: [],
      timeline: [
        { ts: NOW - 120_000, kind: "view", label: "/", viewName: "Home" },
        { ts: NOW - 60_000, kind: "resource", label: "POST /api/pay", status: 500, durationMs: 2400, traceId: "abc123" },
        { ts: NOW - 59_000, kind: "error", label: "TypeError: x is undefined" },
      ],
    };
    renderAt("/operations/telemetry/replays/sess-1", "/operations/telemetry/replays/:sessionId", <TelemetryReplayPage />);
    expect(await screen.findByText("The visual player is not in this app yet")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "POST /api/pay" })).toHaveAttribute("href", "/operations/telemetry/traces/abc123");
    expect(screen.getByText("1m 0s")).toBeInTheDocument();
    expect(screen.getByText("TypeError: x is undefined")).toBeInTheDocument();
  });
});

describe("profiling", () => {
  it("asks for a service first, then shows its most expensive functions and the flame graph", async () => {
    const first = renderAt("/operations/telemetry/profiling", "/operations/telemetry/profiling", <TelemetryProfilingPage />);
    expect(await screen.findByText("Pick a service")).toBeInTheDocument();
    expect(telemetryGet).not.toHaveBeenCalledWith("profiles/types", expect.anything());
    first.unmount();
    responses["profiles/types"] = { types: [{ type: "cpu", unit: "nanoseconds", runtime: "go", hosts: [], versions: [], routes: 0 }] };
    responses["profiles/top"] = { unit: "nanoseconds", total: 10_000_000_000, functions: [{ name: "encoding/json.Marshal", self: 4_000_000_000, total: 6_000_000_000 }] };
    responses["profiles/flamegraph"] = { type: "cpu", unit: "nanoseconds", total: 10, profiles: 3, from: 0, to: 0, root: { name: "all", value: 10, self: 0, children: [{ name: "main", value: 10, self: 10, children: [] }] } };
    responses["profiles/series"] = set(["web-1"]);
    renderAt("/operations/telemetry/profiling?service=checkout", "/operations/telemetry/profiling", <TelemetryProfilingPage />);
    expect(await screen.findByText("encoding/json.Marshal")).toBeInTheDocument();
    expect(screen.getByText("4 s")).toBeInTheDocument();
    expect(screen.getByText("40%")).toBeInTheDocument();
    expect(await screen.findByRole("button", { name: /^main:/ })).toBeInTheDocument();
    expect(telemetryGet).toHaveBeenCalledWith("profiles/top", expect.objectContaining({ service: "checkout", type: "cpu" }));
  });
});

describe("LLM monitoring", () => {
  it("shows tokens, cost and the models used", async () => {
    responses["ai/overview"] = {
      totals: { calls: 1200, errors: 12, errorRate: 0.01, inputTokens: 900_000, outputTokens: 100_000, cacheReadTokens: 0, costUsd: 4.5, costPartial: true, p50Ms: 800, p95Ms: 2500 },
      series: { calls: set(["gpt"]), tokens: set(["in", "out"]), cost: set(["gpt"]), latency: set(["p95"]) },
      byModel: [{ provider: "openai", model: "gpt-4o-mini", calls: 1200, errors: 12, inputTokens: 900_000, outputTokens: 100_000, costUsd: 4.5, p95Ms: 2500, priceKnown: true }],
      byService: [],
      byEndpoint: [],
    };
    renderAt("/operations/telemetry/llm?service=checkout", "/operations/telemetry/llm", <TelemetryLlmPage />);
    expect(await screen.findByText("gpt-4o-mini")).toBeInTheDocument();
    expect(screen.getByText("Some models have no price")).toBeInTheDocument();
    expect(screen.getAllByText("900K in · 100K out").length).toBeGreaterThan(0);
    expect(telemetryGet).toHaveBeenCalledWith("ai/overview", expect.objectContaining({ service: "checkout" }));
  });
});

describe("load tests", () => {
  const run = { id: "r1", scenarioId: "sc1", state: "failed", startedAt: NOW - 600_000, finishedAt: NOW - 300_000, progress: 1, rps: 250, p95: 900, errorRate: 0.03, vus: 100, thresholds: [{ name: "p95 < 500ms", passed: false }, { name: "errors < 5%", passed: true }] };

  it("lists runs with their scenario and thresholds", async () => {
    responses["loadtests/scenarios"] = [{ id: "sc1", name: "Checkout spike", target: "https://shop.example.com", source: "agent", steps: 3, peakVus: 100, duration: 300, regions: ["eu"] }];
    responses["loadtests/runs"] = [run];
    renderAt("/operations/telemetry/load-tests", "/operations/telemetry/load-tests", <TelemetryLoadTestsPage />);
    expect(await screen.findByRole("link", { name: "Checkout spike" })).toHaveAttribute("href", "/operations/telemetry/load-tests/r1");
    expect(screen.getByText("1 failed")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /start|run now/i })).not.toBeInTheDocument();
  });

  it("shows a run's timeline, thresholds and steps", async () => {
    responses["loadtests/runs/r1"] = { ...run, steps: [{ key: "POST /pay", count: 5000, errors: 150, errorRate: 0.03, rps: 120, p50: 300, p95: 900, p99: 1500 }], statusCodes: { "200": 4850, "503": 150 } };
    responses["loadtests/runs/r1/series"] = { ts: [NOW - 600_000, NOW - 595_000], vus: [10, 20], rps: [100, 200], p95: [500, 900], errorRate: [0, 0.05], stepMs: 5000, source: "metrics" };
    responses["loadtests/scenarios"] = [{ id: "sc1", name: "Checkout spike", target: "", source: "agent", steps: 3, peakVus: 100, duration: 300, regions: [] }];
    renderAt("/operations/telemetry/load-tests/r1", "/operations/telemetry/load-tests/:id", <TelemetryLoadTestRunPage />);
    expect(await screen.findByRole("heading", { name: "Checkout spike" })).toBeInTheDocument();
    expect(screen.getByText("p95 < 500ms")).toBeInTheDocument();
    expect(screen.getByText("503 × 150")).toBeInTheDocument();
    expect(screen.getByText("POST /pay")).toBeInTheDocument();
    expect(screen.getByRole("img", { name: "Requests per second" })).toBeInTheDocument();
  });
});
