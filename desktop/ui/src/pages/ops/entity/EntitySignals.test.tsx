import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { ApiError, type ComponentSignal, type ComponentSignals, type SignalBinding } from "@/api";
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

const toast = vi.hoisted(() => ({ success: vi.fn(), info: vi.fn(), error: vi.fn() }));
vi.mock("sonner", () => ({ toast }));

const apiMock = vi.hoisted(() => ({
  getComponentSignals: vi.fn(),
  suggestComponentSignals: vi.fn(),
  confirmSignalBinding: vi.fn(),
  dismissSignalBinding: vi.fn(),
  deleteSignalBinding: vi.fn(),
  getComponentSeries: vi.fn(),
  listDataSourceKinds: vi.fn(),
  listDataSources: vi.fn(),
  listRecipes: vi.fn(),
  listSignals: vi.fn(),
  listStoreCredentials: vi.fn(),
  createDataSource: vi.fn(),
  bindComponentSignal: vi.fn(),
}));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return { ...actual, api: { ...actual.api, ...apiMock } };
});

import EntitySignals from "@/pages/ops/entity/EntitySignals";

const entity: OpsEntity = {
  id: "c-api",
  name: "api",
  role: "backend",
  repositoryId: "r-shop",
  repositoryName: "shop",
  projectIds: [],
  environments: [],
};

function binding(overrides: Partial<SignalBinding> = {}): SignalBinding {
  return {
    id: "b-1",
    component_id: "c-api",
    environment: "production",
    signal: "http.requests",
    data_source_id: "ds-1",
    data_source_name: "Prod Prometheus",
    data_source_kind: "prometheus",
    recipe_id: "otel-http",
    params: { job: "api" },
    query: "",
    status: "confirmed",
    source: "human",
    rendered_query: "sum(rate(x[2m]))",
    needs_params: [],
    created_at: "",
    updated_at: "",
    ...overrides,
  };
}

function signal(overrides: Partial<ComponentSignal>): ComponentSignal {
  return { signal: "http.requests", kind: "metric", unit: "req/s", golden: true, status: "unbound", binding: null, suggestions: [], ...overrides };
}

const ready = signal({ signal: "http.latency", unit: "ms", status: "suggested", suggestions: [binding({ id: "s-lat", signal: "http.latency", status: "suggested", source: "detected" })] });
const partial = signal({
  signal: "http.error_ratio",
  unit: "%",
  status: "suggested",
  suggestions: [binding({ id: "s-err", signal: "http.error_ratio", status: "suggested", source: "detected", params: { job: "" }, needs_params: ["job"] })],
});
const bound = signal({ signal: "http.requests", status: "bound", binding: binding() });
const cpu = signal({ signal: "infra.cpu", unit: "cores", golden: false });

function answer(signals: ComponentSignal[]): ComponentSignals {
  return { component_id: "c-api", role: "backend", environment: "production", signals };
}

function renderPage() {
  return render(
    <MemoryRouter initialEntries={["/operations/entity/c-api/signals"]}>
      <I18nProvider>
        <OpsProvider>
          <EntitySignals entity={entity} env="production" />
        </OpsProvider>
      </I18nProvider>
    </MemoryRouter>,
  );
}

function row(name: string): HTMLElement {
  const table = screen.getByRole("table", { name: "Signals" });
  return within(table).getByText(name, { selector: "span" }).closest("tr") as HTMLElement;
}

beforeEach(() => {
  sessionStorage.clear();
  for (const key of ["kinds", "list", "recipes", "signals"]) writeCache(`operations.sources.${key}`, undefined);
  writeCache("operations.signals.c-api.production", undefined);
  Object.values(apiMock).forEach((fn) => fn.mockReset());
  Object.values(toast).forEach((fn) => fn.mockReset());
  apiMock.getComponentSignals.mockResolvedValue(answer([ready, partial, bound, cpu]));
  apiMock.getComponentSeries.mockResolvedValue({
    from: "",
    to: "",
    step_seconds: 15,
    signals: [{ signal: "http.requests", unit: "req/s", bound: true, series: [{ name: "", labels: {}, points: [[1, 3], [2, 5]] }], previous: [], error: null }],
  });
  apiMock.listDataSourceKinds.mockResolvedValue({ kinds: [prometheusKind] });
  apiMock.listDataSources.mockResolvedValue({ data_sources: [dataSource()] });
  apiMock.listRecipes.mockResolvedValue({
    recipes: [
      {
        id: "otel-http",
        source_kind: "prometheus",
        applies_to: { technology: "", messaging_system: "", resource_kind: "", resource_params: {} },
        detect: { metric: "" },
        params: ["job"],
        signals: { "http.requests": "q", "http.error_ratio": "q", "http.latency": "q" },
        origin: "catalog",
      },
    ],
  });
  apiMock.listSignals.mockResolvedValue({ signals: [] });
  apiMock.listStoreCredentials.mockResolvedValue([{ provider: "google_play", configured: true, updated_at: "" }]);
});

describe("EntitySignals", () => {
  it("lists the role's signals with their status, source, recipe, params and a sparkline of the bound ones", async () => {
    renderPage();
    await screen.findByRole("table", { name: "Signals" });
    const requests = row("Throughput");
    expect(within(requests).getByText("Bound")).toBeInTheDocument();
    expect(within(requests).getByText("Prod Prometheus")).toBeInTheDocument();
    expect(within(requests).getByText("otel-http")).toBeInTheDocument();
    expect(within(requests).getByText("job=api")).toBeInTheDocument();
    await waitFor(() => expect(within(requests).getByRole("img", { name: "Throughput in this range" })).toBeInTheDocument());
    expect(within(row("CPU")).getByText("Not bound")).toBeInTheDocument();
    expect(within(row("Response time")).getByText("Suggested", { selector: "div" })).toBeInTheDocument();
    expect(apiMock.getComponentSeries).toHaveBeenCalledWith("c-api", expect.objectContaining({ env: "production", signals: ["http.requests"] }));
  });

  it("accepts a complete suggestion in one click and dismisses another", async () => {
    const confirmed = binding({ id: "s-lat", signal: "http.latency", status: "confirmed", source: "detected" });
    apiMock.confirmSignalBinding.mockImplementation(async () => {
      apiMock.getComponentSignals.mockResolvedValue(
        answer([{ ...ready, status: "bound", binding: confirmed, suggestions: [] }, partial, bound, cpu]),
      );
      return confirmed;
    });
    apiMock.dismissSignalBinding.mockResolvedValue(undefined);
    renderPage();
    await screen.findByRole("table", { name: "Signals" });

    fireEvent.click(screen.getByRole("button", { name: "Accept Prod Prometheus for Response time" }));
    await waitFor(() => expect(apiMock.confirmSignalBinding).toHaveBeenCalledWith("s-lat"));
    await waitFor(() => expect(within(row("Response time")).getByText("Bound")).toBeInTheDocument());

    fireEvent.click(screen.getByRole("button", { name: "Dismiss Prod Prometheus for Error rate" }));
    await waitFor(() => expect(apiMock.dismissSignalBinding).toHaveBeenCalledWith("s-err"));
    await waitFor(() =>
      expect(screen.queryByRole("button", { name: "Dismiss Prod Prometheus for Error rate" })).not.toBeInTheDocument(),
    );
    expect(within(row("Error rate")).getByText("Not bound")).toBeInTheDocument();
  });

  it("opens the editor, filled from the suggestion, for one that still needs params", async () => {
    renderPage();
    await screen.findByRole("table", { name: "Signals" });
    fireEvent.click(screen.getByRole("button", { name: "Review and accept Prod Prometheus for Error rate" }));
    const dialog = await screen.findByRole("dialog", { name: "Bind Error rate" });
    expect(within(dialog).getByLabelText("Recipe")).toHaveTextContent("otel-http");
    expect(within(dialog).getByLabelText("job")).toHaveValue("");
    expect(apiMock.confirmSignalBinding).not.toHaveBeenCalled();
  });

  it("finds suggestions on request and removes a binding after asking", async () => {
    apiMock.suggestComponentSignals.mockResolvedValue(
      answer([ready, partial, bound, signal({ signal: "infra.cpu", unit: "cores", golden: false, status: "suggested", suggestions: [binding({ id: "s-cpu", signal: "infra.cpu", status: "suggested", recipe_id: "cadvisor", params: { container: "api" } })] })]),
    );
    apiMock.deleteSignalBinding.mockResolvedValue(undefined);
    renderPage();
    await screen.findByRole("table", { name: "Signals" });

    fireEvent.click(screen.getByRole("button", { name: "Find suggestions" }));
    await waitFor(() => expect(apiMock.suggestComponentSignals).toHaveBeenCalledWith("c-api", "production"));
    expect(await screen.findByRole("button", { name: "Accept Prod Prometheus for CPU" })).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Remove the binding of Throughput" }));
    const confirm = await screen.findByRole("dialog", { name: "Remove the binding of Throughput?" });
    fireEvent.click(within(confirm).getByRole("button", { name: "Remove" }));
    await waitFor(() => expect(apiMock.deleteSignalBinding).toHaveBeenCalledWith("b-1"));
    await waitFor(() => expect(within(row("Throughput")).getByText("Not bound")).toBeInTheDocument());
  });

  it("words a dismiss the server refused because the suggestion was accepted meanwhile, and reads the list again", async () => {
    apiMock.dismissSignalBinding.mockRejectedValue(new ApiError("the binding is confirmed; delete it instead", 409, "confirmed"));
    renderPage();
    await screen.findByRole("table", { name: "Signals" });
    const reads = apiMock.getComponentSignals.mock.calls.length;
    fireEvent.click(screen.getByRole("button", { name: "Dismiss Prod Prometheus for Error rate" }));
    await waitFor(() => expect(toast.error).toHaveBeenCalledWith("This suggestion was accepted meanwhile. Remove its binding instead."));
    await waitFor(() => expect(apiMock.getComponentSignals.mock.calls.length).toBeGreaterThan(reads));
  });

  it("reads the window once for a first binding: the new series key does it", async () => {
    const confirmed = binding({ id: "s-lat", signal: "http.latency", status: "confirmed", source: "detected" });
    apiMock.confirmSignalBinding.mockResolvedValue(confirmed);
    apiMock.getComponentSignals.mockImplementation(async () => answer([ready, partial, bound, cpu]));
    renderPage();
    await screen.findByRole("table", { name: "Signals" });
    await waitFor(() => expect(apiMock.getComponentSeries).toHaveBeenCalledTimes(1));
    apiMock.getComponentSignals.mockResolvedValue(answer([{ ...ready, status: "bound", binding: confirmed, suggestions: [] }, partial, bound, cpu]));
    fireEvent.click(screen.getByRole("button", { name: "Accept Prod Prometheus for Response time" }));
    await waitFor(() =>
      expect(apiMock.getComponentSeries).toHaveBeenLastCalledWith("c-api", expect.objectContaining({ signals: ["http.latency", "http.requests"] })),
    );
    await new Promise((r) => setTimeout(r, 50));
    expect(apiMock.getComponentSeries).toHaveBeenCalledTimes(2);
  });

  it("points to Data sources when no source can feed a signal yet, and still looks for sources to connect", async () => {
    apiMock.listDataSources.mockResolvedValue({ data_sources: [dataSource({ id: "ds-rum", capabilities: ["rum"] })] });
    renderPage();
    expect(await screen.findByText("Connect a metrics data source first")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Go to Data sources" })).toHaveAttribute("href", "/operations/sources");
    expect(screen.getByRole("button", { name: "Find suggestions" })).toBeInTheDocument();
  });
});

describe("EntitySignals source offers", () => {
  const playKind = {
    kind: "google_play_vitals",
    label: "Google Play vitals",
    description: "Android vitals from Google Play.",
    capabilities: ["metrics", "crashes"],
    fields: [
      { key: "store_credential", label: "Google Play credential", help: "", type: "store_credential", required: true, options: [], placeholder: "", show_when: {}, secret: false, provider: "google_play", default: "google_play" },
      { key: "package_name", label: "Package name", help: "", type: "text", required: false, options: [], placeholder: "", show_when: {}, secret: false },
    ],
  };
  const offer = {
    kind: "google_play_vitals",
    config: { store_credential: "google_play", package_name: "com.acme.app" },
    recipe_id: "play-vitals",
    params: { package: "com.acme.app" },
    signals: ["mobile.crash_free_users", "mobile.anr_rate"],
    store_credential: "google_play",
  };

  beforeEach(() => {
    apiMock.listDataSourceKinds.mockResolvedValue({ kinds: [prometheusKind, playKind] });
    apiMock.suggestComponentSignals.mockResolvedValue({ ...answer([ready, partial, bound, cpu]), source_offers: [offer] });
    apiMock.createDataSource.mockResolvedValue(dataSource({ id: "ds-play", kind: "google_play_vitals", name: "Google Play vitals", capabilities: ["metrics", "crashes"] }));
    apiMock.bindComponentSignal.mockImplementation((_c: string, signal: string) => Promise.resolve(binding({ id: `b-${signal}`, signal, data_source_id: "ds-play" })));
  });

  it("offers to connect Google Play vitals, creates the source prefilled and binds the offered recipe", async () => {
    renderPage();
    await screen.findByRole("table", { name: "Signals" });
    fireEvent.click(screen.getByRole("button", { name: "Find suggestions" }));
    expect(await screen.findByText("Connect Google Play vitals for this app")).toBeInTheDocument();
    expect(screen.getByText("package=com.acme.app")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Connect" }));
    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByLabelText(/^Package name/)).toHaveValue("com.acme.app");
    expect(within(dialog).getByText("Google Play service account")).toBeInTheDocument();
    fireEvent.click(within(dialog).getByRole("button", { name: "Save and check" }));

    await waitFor(() =>
      expect(apiMock.createDataSource).toHaveBeenCalledWith({
        kind: "google_play_vitals",
        config: { store_credential: "google_play", package_name: "com.acme.app" },
        secrets: {},
      }),
    );
    await waitFor(() => expect(apiMock.bindComponentSignal).toHaveBeenCalledTimes(2));
    for (const signal of offer.signals) {
      expect(apiMock.bindComponentSignal).toHaveBeenCalledWith("c-api", signal, "production", {
        data_source_id: "ds-play",
        recipe_id: "play-vitals",
        params: { package: "com.acme.app" },
      });
    }
    await waitFor(() => expect(toast.success).toHaveBeenCalledWith("Bound 2 signals through Google Play vitals"));
    expect(screen.queryByText("Connect Google Play vitals for this app")).not.toBeInTheDocument();
  });

  it("asks for the store credential first when it isn't saved", async () => {
    apiMock.listStoreCredentials.mockResolvedValue([{ provider: "google_play", configured: false, updated_at: "" }]);
    renderPage();
    await screen.findByRole("table", { name: "Signals" });
    fireEvent.click(screen.getByRole("button", { name: "Find suggestions" }));
    expect(await screen.findByText("Save the Google Play credential first.")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Settings → Integrations" })).toHaveAttribute("href", "/settings/integrations");
    expect(screen.getByRole("button", { name: "Connect" })).toBeDisabled();
  });
});
