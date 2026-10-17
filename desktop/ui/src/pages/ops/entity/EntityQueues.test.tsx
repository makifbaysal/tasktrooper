import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { ComponentQueues, ComponentSignals, ProjectsOverview, QueueTable, SignalSeries } from "@/api";
import { I18nProvider } from "@/hooks/useI18n";
import { writeCache } from "@/lib/uiCache";
import { dataSource, prometheusKind } from "@/test/dataSourceFixtures";
import { component, env, opsOverview, repo } from "@/test/opsConsoleFixtures";

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

const mocks = vi.hoisted(() => ({
  getProjectsOverview: vi.fn(),
  listIncidents: vi.fn(),
  listAllReleases: vi.fn(),
  getComponentSeries: vi.fn(),
  getOpsGolden: vi.fn(),
  getComponentSignals: vi.fn(),
  getComponentQueues: vi.fn(),
  suggestComponentSignals: vi.fn(),
  listAlertRules: vi.fn(),
  listDataSourceKinds: vi.fn(),
  listDataSources: vi.fn(),
  listRecipes: vi.fn(),
  listSignals: vi.fn(),
}));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return { ...actual, api: { ...actual.api, ...mocks } };
});

import { OpsShell } from "@/components/operations/shell/OpsShell";
import OpsEntityPage from "@/pages/ops/OpsEntityPage";
import EntityQueues from "@/pages/ops/entity/EntityQueues";
import { EntitySubRoute } from "@/pages/ops/entity/EntitySubRoute";

const workerRepo = repo("r-jobs", "jobs", [component("c-queue", "mailer", "worker", "mailer")], [env("e-queue", "c-queue")]);
const overview = {
  ...opsOverview,
  unassigned: [...(opsOverview as unknown as { unassigned: unknown[] }).unassigned, workerRepo],
} as unknown as ProjectsOverview;

const DAY = Date.parse("2026-10-15T00:00:00Z");
const onDay = (n: number) => `range=custom&from=${DAY - n * 86_400_000}&to=${DAY - (n - 1) * 86_400_000}`;

function renderQueues(query: string) {
  return render(
    <MemoryRouter initialEntries={[`/operations/entity/c-queue/queues?${query}`]}>
      <I18nProvider>
        <Routes>
          <Route path="/operations" element={<OpsShell />}>
            <Route path="entity/:componentId" element={<OpsEntityPage />}>
              <Route path="queues" element={<EntitySubRoute tab="queues" page={EntityQueues} />} />
              <Route path="*" element={<p>elsewhere</p>} />
            </Route>
          </Route>
        </Routes>
      </I18nProvider>
    </MemoryRouter>,
  );
}

function binding(id: string, signal: string) {
  return {
    id,
    component_id: "c-queue",
    environment: "production",
    signal,
    data_source_id: "ds-kafka",
    data_source_name: "kafka-prod",
    data_source_kind: "kafka",
    recipe_id: "kafka-direct",
    params: { group: "billing" },
    query: "",
    status: "confirmed",
    source: "human",
    rendered_query: '{"group":"billing","metric":"lag"}',
    needs_params: [],
    created_at: "",
    updated_at: "",
  };
}

function signals(bound: boolean, suggestions = 0): ComponentSignals {
  return {
    component_id: "c-queue",
    role: "worker",
    environment: "production",
    signals: [
      {
        signal: "queue.backlog",
        kind: "metric",
        unit: "messages",
        golden: true,
        status: bound ? "bound" : suggestions ? "suggested" : "unbound",
        binding: bound ? binding("b-backlog", "queue.backlog") : null,
        suggestions: Array.from({ length: suggestions }, (_, i) => ({ ...binding(`s-${i}`, "queue.backlog"), status: "suggested" })),
      },
      { signal: "queue.consume_rate", kind: "metric", unit: "msg/s", golden: true, status: "unbound", binding: null, suggestions: [] },
    ],
  } as unknown as ComponentSignals;
}

function unbound(signal: string, unit: string): SignalSeries {
  return { signal, unit, bound: false, series: [], previous: [], error: null };
}

function series(to: number, backlog: SignalSeries) {
  return {
    from: new Date(to - 86_400_000).toISOString(),
    to: new Date(to).toISOString(),
    step_seconds: 600,
    signals: [
      backlog,
      unbound("queue.backlog_age", "s"),
      unbound("queue.consume_rate", "msg/s"),
      unbound("queue.publish_rate", "msg/s"),
      unbound("queue.dead_letter", "messages"),
    ],
  };
}

const KAFKA_COLUMNS = [
  { key: "topic", label: "Topic", unit: "" },
  { key: "partition", label: "Partition", unit: "" },
  { key: "committed_offset", label: "Committed offset", unit: "" },
  { key: "end_offset", label: "End offset", unit: "" },
  { key: "lag", label: "Lag", unit: "messages" },
  { key: "member_id", label: "Member", unit: "" },
];

function table(partial: Partial<QueueTable> = {}): QueueTable {
  return {
    signal_binding_id: "b-backlog",
    data_source_id: "ds-kafka",
    data_source_name: "kafka-prod",
    recipe_id: "kafka-direct",
    messaging_system: "kafka",
    params: { group: "billing" },
    columns: KAFKA_COLUMNS,
    rows: [
      { topic: "invoices", partition: 0, committed_offset: 100, end_offset: 104, lag: 4, member_id: "m-1" },
      { topic: "invoices", partition: 1, committed_offset: 50, end_offset: 80, lag: 30, member_id: "m-2" },
      { topic: "invoices", partition: 2, committed_offset: 9, end_offset: 17, lag: 8, member_id: null },
    ],
    truncated: false,
    error: null,
    ...partial,
  };
}

function queuesAnswer(queues: QueueTable[]): ComponentQueues {
  return { component_id: "c-queue", environment: "production", queues };
}

function bodyRows(card: HTMLElement) {
  return within(within(card).getByRole("table")).getAllByRole("row").slice(1);
}

beforeEach(() => {
  sessionStorage.clear();
  localStorage.clear();
  for (const key of [
    "operations.entities.overview",
    "operations.console.openIncidents",
    "operations.console.shippedReleases",
    "operations.signals.c-queue.production",
    "operations.queues.c-queue.production",
    "operations.alertRules.c-queue",
    "operations.sources.kinds",
    "operations.sources.list",
    "operations.sources.recipes",
    "operations.sources.signals",
  ]) {
    writeCache(key, undefined);
  }
  mocks.getProjectsOverview.mockReset().mockResolvedValue(overview);
  mocks.listIncidents.mockReset().mockResolvedValue({ incidents: [], count: 0 });
  mocks.listAllReleases.mockReset().mockResolvedValue({ releases: [] });
  mocks.getOpsGolden.mockReset().mockResolvedValue({ generated_at: "", components: [] });
  mocks.getComponentSignals.mockReset().mockResolvedValue(signals(true));
  mocks.getComponentSeries.mockReset().mockResolvedValue(series(DAY, unbound("queue.backlog", "messages")));
  mocks.getComponentQueues.mockReset().mockResolvedValue(queuesAnswer([table()]));
  mocks.suggestComponentSignals.mockReset();
  mocks.listAlertRules.mockReset().mockResolvedValue({ rules: [] });
  mocks.listDataSourceKinds.mockReset().mockResolvedValue({ kinds: [prometheusKind] });
  mocks.listDataSources.mockReset().mockResolvedValue({ data_sources: [dataSource()] });
  mocks.listRecipes.mockReset().mockResolvedValue({ recipes: [] });
  mocks.listSignals.mockReset().mockResolvedValue({ signals: [] });
});

describe("EntityQueues", () => {
  it("lists each queue source as a table sorted by the largest lag, units in the header, the largest flagged", async () => {
    mocks.getComponentQueues.mockResolvedValue({
      ...queuesAnswer([table({ truncated: true, key_columns: ["topic", "partition"] })]),
      read_at: new Date(Date.now() - 45_000).toISOString(),
    });
    renderQueues(onDay(1));

    const heading = await screen.findByRole("heading", { level: 3, name: /kafka-prod/ });
    const card = heading.closest(".rounded-xl") as HTMLElement;
    expect(within(card).getByText("Kafka")).toBeInTheDocument();
    expect(within(card).getByText("Recipe kafka-direct")).toBeInTheDocument();
    expect(within(card).getByText("group=billing")).toBeInTheDocument();
    expect(within(card).getByRole("button", { name: "Sort by Lag" })).toHaveTextContent("Lag(messages)");
    expect(within(card).getByRole("columnheader", { name: /Lag/ })).toHaveAttribute("aria-sort", "descending");

    expect(within(card).getByText(/^Read /).closest("[title]")).toHaveAttribute("title", expect.stringContaining("30 seconds"));
    let rows = bodyRows(card);
    expect(rows.map((r) => within(r).getAllByRole("cell")[2].textContent)).toEqual(["1", "2", "0"]);
    expect(within(rows[0]).getAllByRole("cell")[1]).toHaveClass("font-medium");
    expect(within(rows[0]).getAllByRole("cell")[3]).not.toHaveClass("font-medium");
    expect(within(rows[0]).getByText("Largest backlog")).toBeInTheDocument();
    expect(within(rows[1]).getByText("No value")).toBeInTheDocument();
    expect(within(card).getByText("The highlighted row holds the largest backlog.")).toBeInTheDocument();
    expect(within(card).getByText("The source holds more rows than it sent: the first 3 are shown.")).toBeInTheDocument();

    fireEvent.click(within(card).getByRole("button", { name: "Sort by Lag" }));
    rows = bodyRows(card);
    expect(rows.map((r) => within(r).getAllByRole("cell")[2].textContent)).toEqual(["0", "2", "1"]);
    fireEvent.click(within(card).getByRole("button", { name: "Sort by Partition" }));
    rows = bodyRows(card);
    expect(rows.map((r) => within(r).getAllByRole("cell")[2].textContent)).toEqual(["0", "1", "2"]);
  });

  it("flags the partitions above the backlog alert's threshold", async () => {
    mocks.listAlertRules.mockResolvedValue({
      rules: [
        {
          id: "ar-1",
          name: "Backlog",
          component_id: "c-queue",
          environment: "production",
          signal: "queue.backlog",
          condition: { op: "gt", threshold: 5, for_seconds: 0, window_seconds: 300 },
          severity: "high",
          channels: ["desktop"],
          open_incident: true,
          enabled: true,
          state: "firing",
          state_since: "",
          created_at: "",
          updated_at: "",
        },
      ],
    });
    renderQueues(onDay(2));
    const heading = await screen.findByRole("heading", { level: 3, name: /kafka-prod/ });
    const card = heading.closest(".rounded-xl") as HTMLElement;
    await waitFor(() => expect(within(card).getAllByText("Above the alert threshold of 5")).toHaveLength(2));
    expect(within(card).getByText("Highlighted rows are above the backlog alert's threshold (5).")).toBeInTheDocument();
  });

  it("answers each table's error by its code: fill in the binding, retry, or explain", async () => {
    mocks.getComponentQueues.mockResolvedValue(
      queuesAnswer([
        table({ signal: "queue.consumers", data_source_name: "needs", rows: [], columns: [], error: { code: "needs_params", message: "no value for group" } }),
        table({ signal_binding_id: "b-2", data_source_name: "slow", rows: [], columns: [], error: { code: "timeout", message: "" } }),
        table({ signal_binding_id: "b-3", data_source_name: "plain", rows: [], columns: [], error: { code: "unsupported", message: "" } }),
      ]),
    );
    renderQueues(onDay(3));

    const card = (name: string) => (screen.getByRole("heading", { level: 3, name: new RegExp(name) }).closest(".rounded-xl") as HTMLElement);
    await screen.findByRole("heading", { level: 3, name: /needs/ });
    expect(within(card("needs")).getByText("The binding is missing a parameter")).toBeInTheDocument();
    expect(within(card("needs")).getByText("no value for group")).toBeInTheDocument();
    expect(within(card("slow")).getByText("The source didn't answer in time")).toBeInTheDocument();
    expect(within(card("plain")).getByText("This source has no queues table")).toBeInTheDocument();
    expect(within(card("plain")).queryByRole("button")).not.toBeInTheDocument();

    const before = mocks.getComponentQueues.mock.calls.length;
    fireEvent.click(within(card("slow")).getByRole("button", { name: "Retry" }));
    await waitFor(() => expect(mocks.getComponentQueues.mock.calls.length).toBeGreaterThan(before));

    fireEvent.click(within(card("needs")).getByRole("button", { name: "Fill in parameters" }));
    await waitFor(() => expect(screen.getByRole("dialog", { name: "Bind Consumers" })).toBeInTheDocument());
  });

  it("shows a direct broker's live value with the way to history instead of an empty chart", async () => {
    const to = DAY - 3 * 86_400_000;
    mocks.getComponentSeries.mockResolvedValue(
      series(to, {
        signal: "queue.backlog",
        unit: "messages",
        bound: true,
        live: true,
        series: [{ name: "lag", labels: { group: "billing" }, points: [[to, 42]] }],
        previous: [],
        error: null,
      }),
    );
    renderQueues(onDay(4));

    const chart = (await screen.findByRole("heading", { level: 3, name: "Backlog" })).closest(".rounded-xl") as HTMLElement;
    await waitFor(() => expect(within(chart).getByRole("group", { name: "Backlog" })).toBeInTheDocument());
    expect(within(chart).getByText("42 messages")).toBeInTheDocument();
    expect(within(chart).getByText(/Live value — connect an exporter-backed source for history\./)).toBeInTheDocument();
    expect(within(chart).getByRole("link", { name: "Data sources" })).toHaveAttribute("href", expect.stringContaining("/operations/sources"));
    expect(within(chart).queryByRole("img")).not.toBeInTheDocument();

    const tile = screen.getByText("Total backlog").closest("div") as HTMLElement;
    expect(within(tile).getByText("42")).toBeInTheDocument();
    expect(within(tile).getByText("Live value, no history")).toBeInTheDocument();
    expect(within(tile).queryByRole("img")).not.toBeInTheDocument();
  });

  it("without a queue binding, explains and offers suggestions, the backlog's binding and a broker source", async () => {
    mocks.getComponentSignals.mockResolvedValue(signals(false));
    mocks.suggestComponentSignals.mockResolvedValue(signals(false, 2));
    renderQueues(onDay(5));

    expect(await screen.findByText("Connect a data source to see queues")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Add a broker data source" })).toHaveAttribute("href", expect.stringContaining("/operations/sources"));
    expect(mocks.getComponentQueues).not.toHaveBeenCalled();
    expect(mocks.getComponentSeries).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole("button", { name: "Find suggestions" }));
    await waitFor(() => expect(mocks.suggestComponentSignals).toHaveBeenCalledWith("c-queue", "production"));
    expect(await screen.findByText("2 suggestions found for mailer's queues")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Review them on Signals" })).toHaveAttribute(
      "href",
      expect.stringContaining("/operations/entity/c-queue/signals"),
    );

    fireEvent.click(screen.getByRole("button", { name: "Bind backlog" }));
    await waitFor(() => expect(screen.getByRole("dialog", { name: "Bind Backlog" })).toBeInTheDocument());
  });

  it("says so when a search finds nothing for the queues", async () => {
    mocks.getComponentSignals.mockResolvedValue(signals(false));
    mocks.suggestComponentSignals.mockResolvedValue(signals(false));
    renderQueues(onDay(6));
    fireEvent.click(await screen.findByRole("button", { name: "Find suggestions" }));
    expect(await screen.findByText("No suggestions yet")).toBeInTheDocument();
  });
});
