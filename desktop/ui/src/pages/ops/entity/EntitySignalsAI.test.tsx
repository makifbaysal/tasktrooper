import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter, useLocation } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { ApiError, type AIJob, type ComponentSignal, type ComponentSignals, type SignalBinding } from "@/api";
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
  getComponentSeries: vi.fn(),
  listDataSourceKinds: vi.fn(),
  listDataSources: vi.fn(),
  listRecipes: vi.fn(),
  listSignals: vi.fn(),
  listStoreCredentials: vi.fn(),
  suggestComponentSignalsAI: vi.fn(),
  getAIJob: vi.fn(),
  getSession: vi.fn(),
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
    signal: "http.latency",
    data_source_id: "ds-1",
    data_source_name: "Prod Prometheus",
    data_source_kind: "prometheus",
    recipe_id: "otel-http",
    params: { job: "api" },
    query: "",
    status: "suggested",
    source: "ai",
    rendered_query: "",
    needs_params: [],
    created_at: "",
    updated_at: "",
    ...overrides,
  };
}

function signal(overrides: Partial<ComponentSignal>): ComponentSignal {
  return { signal: "http.latency", kind: "metric", unit: "ms", golden: true, status: "unbound", binding: null, suggestions: [], ...overrides };
}

const signals: ComponentSignals = {
  component_id: "c-api",
  role: "backend",
  environment: "production",
  signals: [
    signal({ status: "suggested", suggestions: [binding()] }),
    signal({ signal: "http.requests", unit: "req/s", status: "bound", binding: binding({ id: "b-2", signal: "http.requests", status: "confirmed", source: "ai" }) }),
  ],
};

function job(overrides: Partial<AIJob> = {}): AIJob {
  return {
    id: "j-map",
    kind: "map_signals",
    repository_id: "r-shop",
    component_id: "c-api",
    environment: "production",
    status: "running",
    session_id: "s-1",
    result_summary: "",
    error: "",
    created: [],
    created_at: new Date().toISOString(),
    updated_at: new Date().toISOString(),
    finished_at: null,
    ...overrides,
  };
}

function Where() {
  const { pathname, search } = useLocation();
  return <p data-testid="where">{`${pathname}${search}`}</p>;
}

function renderPage() {
  return render(
    <MemoryRouter initialEntries={["/operations/entity/c-api/signals"]}>
      <I18nProvider>
        <OpsProvider>
          <EntitySignals entity={entity} env="production" />
        </OpsProvider>
      </I18nProvider>
      <Where />
    </MemoryRouter>,
  );
}

beforeEach(() => {
  sessionStorage.clear();
  for (const key of ["kinds", "list", "recipes", "signals"]) writeCache(`operations.sources.${key}`, undefined);
  writeCache("operations.signals.c-api.production", undefined);
  writeCache("operations.aiJob.mapSignals.c-api.production", null);
  Object.values(apiMock).forEach((fn) => fn.mockReset());
  Object.values(toast).forEach((fn) => fn.mockReset());
  apiMock.getComponentSignals.mockResolvedValue(signals);
  apiMock.getComponentSeries.mockResolvedValue({ from: "", to: "", step_seconds: 15, signals: [] });
  apiMock.listDataSourceKinds.mockResolvedValue({ kinds: [prometheusKind] });
  apiMock.listDataSources.mockResolvedValue({ data_sources: [dataSource()] });
  apiMock.listRecipes.mockResolvedValue({ recipes: [] });
  apiMock.listSignals.mockResolvedValue({ signals: [] });
  apiMock.listStoreCredentials.mockResolvedValue([]);
});

describe("EntitySignals and the AI", () => {
  it("badges what the release engineer proposed", async () => {
    renderPage();
    const table = await screen.findByRole("table", { name: "Signals" });
    const badges = within(table).getAllByText("AI");
    expect(badges.length).toBe(2);
    expect(badges[0].closest("[title]")).toHaveAttribute("title", "Proposed by the release engineer from the data sources' metrics");
  });

  it("asks the AI to map signals, follows the job and links what it stored", async () => {
    apiMock.suggestComponentSignalsAI.mockResolvedValue({ job_id: "j-map", job: job() });
    apiMock.getAIJob.mockResolvedValue(
      job({
        status: "done",
        created: [
          { kind: "signal_binding", id: "b-9", name: "http.latency", rationale: "histogram_quantile over http_server_duration" },
          { kind: "alert_rule", id: "r-1", name: "Latency p95 above 800 ms" },
        ],
      }),
    );
    renderPage();
    await screen.findByRole("table", { name: "Signals" });
    const reads = apiMock.getComponentSignals.mock.calls.length;
    fireEvent.click(await screen.findByRole("button", { name: "Ask AI to map signals" }));
    await waitFor(() => expect(apiMock.suggestComponentSignalsAI).toHaveBeenCalledWith("c-api", "production"));

    expect(await screen.findByRole("link", { name: "Latency p95 above 800 ms" })).toHaveAttribute("href", "/operations/alerts?rule=r-1");
    expect(screen.getByRole("link", { name: "http.latency" })).toHaveAttribute("href", "/operations/entity/c-api/signals");
    expect(screen.getByText("Alert rule (off)")).toBeInTheDocument();
    expect(screen.getByText(/Its bindings wait below as suggestions/)).toBeInTheDocument();
    await waitFor(() => expect(apiMock.getComponentSignals.mock.calls.length).toBeGreaterThan(reads));
  });

  it("opens the chat of a job that stopped on a question", async () => {
    apiMock.suggestComponentSignalsAI.mockResolvedValue({ job_id: "j-map", job: job() });
    apiMock.getAIJob.mockResolvedValue(job({ status: "failed", error: "the release engineer stopped on a question; answer it in the chat" }));
    apiMock.getSession.mockResolvedValue({ session: { id: "s-1", agent_id: "a-release" }, messages: [] });
    renderPage();
    fireEvent.click(await screen.findByRole("button", { name: "Ask AI to map signals" }));
    expect(await screen.findByText("the release engineer stopped on a question; answer it in the chat")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Open the chat" }));
    await waitFor(() => expect(screen.getByTestId("where").textContent).toBe("/agents/a-release/chat/s-1"));
  });

  it("says how to turn the release engineer on when there is none", async () => {
    apiMock.suggestComponentSignalsAI.mockRejectedValue(new ApiError("no release engineer", 409, "no_release_engineer"));
    renderPage();
    fireEvent.click(await screen.findByRole("button", { name: "Ask AI to map signals" }));
    expect(await screen.findByText("No release engineer is on for this component")).toBeInTheDocument();
    expect(apiMock.getAIJob).not.toHaveBeenCalled();
  });
});
