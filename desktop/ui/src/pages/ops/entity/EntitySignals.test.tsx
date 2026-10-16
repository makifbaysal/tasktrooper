import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { ComponentSignal, ComponentSignals, SignalBinding } from "@/api";
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
  suggestComponentSignals: vi.fn(),
  confirmSignalBinding: vi.fn(),
  dismissSignalBinding: vi.fn(),
  deleteSignalBinding: vi.fn(),
  getComponentSeries: vi.fn(),
  listDataSourceKinds: vi.fn(),
  listDataSources: vi.fn(),
  listRecipes: vi.fn(),
  listSignals: vi.fn(),
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

  it("points to Data sources when no metrics source exists yet", async () => {
    apiMock.listDataSources.mockResolvedValue({ data_sources: [dataSource({ id: "ds-logs", capabilities: ["logs"] })] });
    renderPage();
    expect(await screen.findByText("Connect a metrics data source first")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Go to Data sources" })).toHaveAttribute("href", "/operations/sources");
    expect(screen.queryByRole("button", { name: "Find suggestions" })).not.toBeInTheDocument();
  });
});
