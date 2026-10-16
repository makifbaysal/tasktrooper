import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { ApiError, type DataSourceKindInfo, type ObservabilityRecipe, type SignalBinding } from "@/api";
import { I18nProvider } from "@/hooks/useI18n";
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

const { previewComponentSignal, bindComponentSignal } = vi.hoisted(() => ({
  previewComponentSignal: vi.fn(),
  bindComponentSignal: vi.fn(),
}));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return { ...actual, api: { ...actual.api, previewComponentSignal, bindComponentSignal } };
});

import { SignalBindingDialog } from "@/components/operations/signals/SignalBindingDialog";

const gcpKind: DataSourceKindInfo = {
  kind: "gcp_monitoring",
  label: "Google Cloud Monitoring",
  description: "",
  capabilities: ["metrics"],
  fields: [],
};

const prom = dataSource();
const gcp = dataSource({ id: "ds-gcp", kind: "gcp_monitoring", name: "Cloud Monitoring", config: {}, secrets_set: [] });
const loki = dataSource({ id: "ds-loki", kind: "loki", name: "Logs", capabilities: ["logs"] });

function recipe(id: string, sourceKind: string, signals: Record<string, string>, params: string[] | null = null): ObservabilityRecipe {
  return {
    id,
    source_kind: sourceKind,
    applies_to: { technology: "", messaging_system: "", resource_kind: "", resource_params: {} },
    detect: { metric: "" },
    params,
    signals,
    origin: "catalog",
  };
}

const recipes = [
  recipe("otel-http", "prometheus", { "http.latency": 'histogram_quantile($__quantile, rate(x{job="$job"}[$__rate]))' }, ["job"]),
  recipe("cadvisor", "prometheus", { "infra.cpu": "sum(rate(c[$__rate]))" }, ["container"]),
  recipe("cloud-run", "gcp_monitoring", { "http.latency": "run_latency", "http.requests": "run_requests" }, ["service"]),
];

const WINDOW = { from: Date.parse("2026-10-16T10:00:00Z"), to: Date.parse("2026-10-16T11:00:00Z") };

function binding(overrides: Partial<SignalBinding> = {}): SignalBinding {
  return {
    id: "b-1",
    component_id: "c-api",
    environment: "production",
    signal: "http.latency",
    data_source_id: "ds-1",
    data_source_name: "Prod Prometheus",
    data_source_kind: "prometheus",
    recipe_id: "otel-http",
    params: { job: "api" },
    query: "",
    status: "confirmed",
    source: "human",
    rendered_query: "",
    needs_params: [],
    created_at: "",
    updated_at: "",
    ...overrides,
  };
}

function renderDialog(props: Partial<React.ComponentProps<typeof SignalBindingDialog>> = {}) {
  const onSaved = vi.fn();
  const onOpenChange = vi.fn();
  render(
    <MemoryRouter>
      <I18nProvider>
        <SignalBindingDialog
          open
          onOpenChange={onOpenChange}
          componentId="c-api"
          env="production"
          signal="http.latency"
          unit="ms"
          sources={[prom, gcp, loki]}
          kinds={[prometheusKind, gcpKind]}
          recipes={recipes}
          previewWindow={WINDOW}
          onSaved={onSaved}
          {...props}
        />
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
  previewComponentSignal.mockReset();
  bindComponentSignal.mockReset();
});

describe("SignalBindingDialog", () => {
  it("offers only metrics sources, and only the recipes for the source's kind that provide the signal", () => {
    renderDialog();
    fireEvent.click(screen.getByLabelText("Data source"));
    const sources = screen.getAllByRole("option").map((o) => o.textContent);
    expect(sources).toEqual([expect.stringContaining("Prod Prometheus"), expect.stringContaining("Cloud Monitoring")]);
    fireEvent.click(screen.getByRole("option", { name: /Prod Prometheus/ }));

    fireEvent.click(screen.getByLabelText("Recipe"));
    expect(screen.getAllByRole("option").map((o) => o.textContent)).toEqual(["otel-http"]);
    fireEvent.click(screen.getByRole("option", { name: "otel-http" }));
    expect(screen.getByLabelText("job")).toBeInTheDocument();

    pick(/Data source/, /Cloud Monitoring/);
    fireEvent.click(screen.getByLabelText("Recipe"));
    expect(screen.getAllByRole("option").map((o) => o.textContent)).toEqual(["cloud-run"]);
  });

  it("asks for the recipe's params, then previews over the console's range", async () => {
    previewComponentSignal.mockResolvedValue({
      rendered_query: 'histogram_quantile(0.95, rate(x{job="api"}[2m]))',
      series: [
        { name: "p50", labels: {}, points: [[WINDOW.from, 40]] },
        { name: "p95", labels: {}, points: [[WINDOW.from, 120]] },
      ],
      warnings: ["2 series dropped"],
    });
    renderDialog();
    pick(/Data source/, /Prod Prometheus/);
    const preview = screen.getByRole("button", { name: "Preview" });
    expect(preview).toBeDisabled();
    fireEvent.change(screen.getByLabelText("job"), { target: { value: "api" } });
    expect(preview).toBeEnabled();
    fireEvent.click(preview);

    await waitFor(() =>
      expect(previewComponentSignal).toHaveBeenCalledWith("c-api", {
        signal: "http.latency",
        data_source_id: "ds-1",
        recipe_id: "otel-http",
        params: { job: "api" },
        from: "2026-10-16T10:00:00.000Z",
        to: "2026-10-16T11:00:00.000Z",
        step_seconds: 15,
      }),
    );
    expect(await screen.findByText('histogram_quantile(0.95, rate(x{job="api"}[2m]))')).toBeInTheDocument();
    expect(screen.getByText("2 series dropped")).toBeInTheDocument();
    const legend = screen.getByRole("list", { name: "Series" });
    expect(within(legend).getByText("p50")).toBeInTheDocument();
    expect(within(legend).getByText("p95")).toBeInTheDocument();
  });

  it("saves a custom query instead of a recipe", async () => {
    bindComponentSignal.mockResolvedValue(binding({ recipe_id: "", params: {}, query: "my_latency" }));
    const { onSaved, onOpenChange } = renderDialog();
    pick(/Data source/, /Prod Prometheus/);
    fireEvent.click(screen.getByRole("tab", { name: "Custom query" }));
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    expect(await screen.findByText("Required.")).toBeInTheDocument();
    expect(bindComponentSignal).not.toHaveBeenCalled();

    const query = screen.getByLabelText("Query");
    expect(query).toHaveClass("font-mono");
    fireEvent.change(query, { target: { value: "  my_latency  " } });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() =>
      expect(bindComponentSignal).toHaveBeenCalledWith("c-api", "http.latency", "production", { data_source_id: "ds-1", query: "my_latency" }),
    );
    expect(onSaved).toHaveBeenCalledWith(expect.objectContaining({ query: "my_latency" }));
    expect(onOpenChange).toHaveBeenCalledWith(false);
  });

  it("starts from a suggestion, sends exactly the recipe's params and shows the server's field errors in place", async () => {
    bindComponentSignal.mockRejectedValueOnce(new ApiError("invalid", 400, "", { "params.job": "too_long" }));
    renderDialog({ prefill: binding({ params: { job: "", stray: "x" } }) });
    expect(screen.getByLabelText("Data source")).toHaveTextContent("Prod Prometheus");
    expect(screen.getByLabelText("Recipe")).toHaveTextContent("otel-http");
    fireEvent.change(screen.getByLabelText("job"), { target: { value: "api" } });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() =>
      expect(bindComponentSignal).toHaveBeenCalledWith("c-api", "http.latency", "production", {
        data_source_id: "ds-1",
        recipe_id: "otel-http",
        params: { job: "api" },
      }),
    );
    expect(await screen.findByText("Too long.")).toBeInTheDocument();
    expect(screen.getByLabelText("job")).toHaveAttribute("aria-invalid", "true");
  });

  it("explains the custom query in the source kind's own language", () => {
    const cw = dataSource({ id: "ds-cw", kind: "aws_cloudwatch", name: "CloudWatch", config: {}, secrets_set: [] });
    renderDialog({ sources: [cw], signal: "web.lcp" });
    expect(screen.getByText(/^CloudWatch JSON: \{"namespace", "metric", "stat", "dimensions"\}/)).toBeInTheDocument();
    expect(screen.getByLabelText("Query")).toHaveClass("font-mono");
  });

  it("calls a Prometheus or Cloud Monitoring custom query PromQL", () => {
    renderDialog({ sources: [gcp], signal: "web.lcp" });
    expect(screen.getByText(/^PromQL\. \$__rate, \$__interval and \$__quantile/)).toBeInTheDocument();
  });

  it("falls back to a custom query for a source no recipe fits", () => {
    renderDialog({ signal: "web.lcp", sources: [prom] });
    expect(screen.getByRole("tab", { name: "Custom query" })).toHaveAttribute("aria-selected", "true");
    expect(screen.getByLabelText("Query")).toBeInTheDocument();
  });

  it("points to Data sources when no metrics source exists", () => {
    renderDialog({ sources: [loki] });
    expect(screen.getByText("No metrics data source yet")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Go to Data sources" })).toHaveAttribute("href", "/operations/sources");
  });
});
