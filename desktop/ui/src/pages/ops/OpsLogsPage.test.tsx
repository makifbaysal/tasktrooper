import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter, Route, Routes, useLocation } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { ApiError, type OpsLogEntry, type OpsLogs } from "@/api";
import { I18nProvider } from "@/hooks/useI18n";
import { OPS_LOGS_TAIL_OVERLAP_MS } from "@/hooks/useOpsLogs";
import { writeCache } from "@/lib/uiCache";
import { opsOverview } from "@/test/opsConsoleFixtures";

const { getProjectsOverview, listIncidents, listAllReleases, listOpsLogs } = vi.hoisted(() => ({
  getProjectsOverview: vi.fn(),
  listIncidents: vi.fn(),
  listAllReleases: vi.fn(),
  listOpsLogs: vi.fn(),
}));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return { ...actual, api: { ...actual.api, getProjectsOverview, listIncidents, listAllReleases, listOpsLogs } };
});

import { OpsShell } from "@/components/operations/shell/OpsShell";
import OpsEntityPage from "@/pages/ops/OpsEntityPage";
import OpsLogsPage from "@/pages/ops/OpsLogsPage";
import EntityLogs from "@/pages/ops/entity/EntityLogs";
import { EntitySubRoute } from "@/pages/ops/entity/EntitySubRoute";

const MINUTE = 60_000;

// A provider's nanosecond stamp: the milliseconds Date keeps plus three more digits.
function stamp(msAgo: number, nanos = "000"): string {
  return new Date(Date.now() - msAgo).toISOString().replace("Z", `${nanos}Z`);
}

function entry(timestamp: string, message: string, overrides: Partial<OpsLogEntry> = {}): OpsLogEntry {
  return { timestamp, severity: "info", message, component_id: "c-api", environment_id: "e-api", ...overrides };
}

const t3 = stamp(10 * MINUTE);
const t2 = stamp(20 * MINUTE);
const t1 = stamp(30 * MINUTE, "500");
const t0 = stamp(40 * MINUTE);

const firstPage: OpsLogs = {
  entries: [
    entry(t3, "GET /orders 500", { severity: "error", method: "GET", path: "/orders", status_code: 500, trace_id: "trace-abc", fields: { region: "eu" } }),
    entry(t2, "slow query", { severity: "warning", component_id: "c-site", environment_id: "e-site" }),
    entry(t1, "boundary line", { severity: "critical" }),
  ],
  unavailable: [{ component_id: "c-web", environment_id: "e-web", code: "timeout", message: "" }],
  truncated: true,
};

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
            <Route path="logs" element={<OpsLogsPage />} />
            <Route path="entity/:componentId" element={<OpsEntityPage />}>
              <Route path="logs" element={<EntitySubRoute tab="logs" page={EntityLogs} />} />
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
  getProjectsOverview.mockReset().mockResolvedValue(opsOverview);
  listIncidents.mockReset().mockResolvedValue({ incidents: [], count: 0 });
  listAllReleases.mockReset().mockResolvedValue({ releases: [] });
  listOpsLogs.mockReset().mockResolvedValue(firstPage);
});

async function logRows() {
  const table = await screen.findByRole("table", { name: "Log entries" });
  return within(table)
    .getAllByRole("row")
    .slice(1)
    .filter((r) => within(r).queryByRole("button", { name: /details$/ }));
}

function lastCell(row: HTMLElement): string | null {
  const cells = (row as HTMLTableRowElement).cells;
  return cells[cells.length - 1].textContent;
}

const messages = async () => (await logRows()).map(lastCell);

describe("OpsLogsPage", () => {
  it("reads the window with the query's filters and lists entries newest first", async () => {
    renderAt("/operations/logs?q=orders&severity=warning");
    const rows = await logRows();
    const [query] = listOpsLogs.mock.calls[0];
    expect(query).toMatchObject({ env: "production", text: "orders", minSeverity: "warning", limit: 200 });
    expect(query.componentIds).toBeUndefined();
    expect(query.until).toBeUndefined();
    expect(Math.abs(Date.now() - 60 * MINUTE - Date.parse(query.since))).toBeLessThan(5_000);
    expect(rows).toHaveLength(3);
    expect(within(rows[0]).getByText("Error")).toBeInTheDocument();
    expect(within(rows[0]).getByRole("link", { name: "api" })).toHaveAttribute("href", "/operations/entity/c-api/logs");
    expect(within(rows[1]).getByRole("link", { name: "site" })).toBeInTheDocument();
    expect(screen.getByText("No answer within 20 seconds. Refresh to try again.")).toBeInTheDocument();
    expect(screen.getByText("The range holds more entries than are shown")).toBeInTheDocument();
  });

  it("opens an entry to show its request, trace id and fields", async () => {
    renderAt("/operations/logs");
    const [first] = await logRows();
    fireEvent.click(within(first).getByRole("button", { name: "Show details" }));
    expect(await screen.findByText("trace-abc")).toBeInTheDocument();
    expect(screen.getByText("GET /orders")).toBeInTheDocument();
    expect(screen.getByText("region")).toBeInTheDocument();
    expect(screen.getByText(t3)).toBeInTheDocument();
  });

  it("draws the loaded entries' volume by severity, with a table view", async () => {
    renderAt("/operations/logs");
    await logRows();
    expect(screen.getByText("Loaded entries by severity")).toBeInTheDocument();
    expect(screen.getByText("Counts the 3 entries loaded below, not the provider's totals")).toBeInTheDocument();
    expect(screen.getByRole("img", { name: "Volume of the 3 loaded log entries over time, by severity" })).toBeInTheDocument();
    expect(screen.getAllByTestId("histogram-bar").length).toBeGreaterThan(10);
    fireEvent.click(screen.getByRole("button", { name: "Show as table" }));
    const table = screen.getByRole("table", { name: "Volume of the 3 loaded log entries over time, by severity" });
    const totals = within(table)
      .getAllByRole("row")
      .slice(1)
      .map(lastCell);
    expect(totals.reduce((sum, v) => sum + Number(v), 0)).toBe(3);
  });

  it("loads older entries up to the oldest shown and drops the repeats at that instant", async () => {
    renderAt("/operations/logs");
    await logRows();
    listOpsLogs.mockResolvedValueOnce({
      entries: [entry(t1, "boundary line", { severity: "critical" }), entry(t0, "oldest line")],
      unavailable: [],
      truncated: false,
    });
    fireEvent.click(screen.getByRole("button", { name: "Load older entries" }));
    await waitFor(() => expect(listOpsLogs).toHaveBeenCalledTimes(2));
    const [older] = listOpsLogs.mock.calls[1];
    expect(older.until).toBe(t1);
    expect(older.since).toBe(listOpsLogs.mock.calls[0][0].since);
    await waitFor(async () => expect(await messages()).toHaveLength(4));
    expect((await messages())[3]).toBe("oldest line");
    expect(screen.getByText("Start of the range: nothing older in this window.")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Load older entries" })).not.toBeInTheDocument();
  });

  it("tails entries newer than the newest shown while live", async () => {
    renderAt("/operations/logs");
    await logRows();
    listOpsLogs.mockResolvedValue({
      entries: [entry(stamp(MINUTE), "fresh line"), entry(t3, "GET /orders 500", firstPage.entries[0])],
      unavailable: [],
      truncated: false,
    });
    fireEvent.click(screen.getByRole("switch", { name: "Live tail" }));
    await waitFor(() => expect(listOpsLogs).toHaveBeenCalledTimes(2));
    const [tail] = listOpsLogs.mock.calls[1];
    expect(tail.since).toBe(new Date(Date.parse(t3) - OPS_LOGS_TAIL_OVERLAP_MS).toISOString());
    expect(tail.until).toBeUndefined();
    await waitFor(async () => expect((await messages())[0]).toBe("fresh line"));
    expect(await messages()).toHaveLength(4);
  });

  it("offers no live tail for a custom range that ended in the past", async () => {
    const to = Date.now() - 2 * 60 * MINUTE;
    renderAt(`/operations/logs?range=custom&from=${to - 60 * MINUTE}&to=${to}`);
    await logRows();
    expect(screen.getByRole("switch", { name: "Live tail" })).toBeDisabled();
    expect(screen.getByText("Live tail follows new entries; this range ended in the past.")).toBeInTheDocument();
  });

  it("asks for entities when too many environments match, with none picked", async () => {
    listOpsLogs.mockRejectedValue(
      new ApiError("too_many_environments", 400, "too_many_environments", undefined, {
        error: "too_many_environments",
        code: "too_many_environments",
        matched: 14,
        max_environments: 10,
      }),
    );
    renderAt("/operations/logs");
    expect(await screen.findByText("Too many environments to read at once")).toBeInTheDocument();
    expect(
      screen.getByText("14 environments match in Production; logs are read from up to 10 at a time. Pick the entities to read."),
    ).toBeInTheDocument();
    const pickers = screen.getAllByRole("button", { name: "Entities: Pick entities" });
    expect(pickers.length).toBeGreaterThan(0);
    expect(screen.queryByRole("table", { name: "Log entries" })).not.toBeInTheDocument();

    listOpsLogs.mockReset().mockResolvedValue(firstPage);
    fireEvent.keyDown(pickers[pickers.length - 1], { key: "Enter" });
    fireEvent.click(await screen.findByRole("menuitemcheckbox", { name: /^api/ }));
    await waitFor(() => expect(screen.getByTestId("where")).toHaveTextContent("entity=c-api"));
    await waitFor(() => expect(listOpsLogs).toHaveBeenCalled());
    expect(listOpsLogs.mock.calls[0][0].componentIds).toEqual(["c-api"]);
  });
});

describe("OpsLogsPage with nothing connected", () => {
  it("still reads, lists what a logs binding feeds, and points to connecting when nothing answers", async () => {
    getProjectsOverview.mockResolvedValue({ projects: [], unassigned: [] });
    listOpsLogs.mockResolvedValue({ entries: [entry(t3, "bound only line", { environment_id: "00000000-0000-0000-0000-000000000000", data_source_id: "ds-loki" })], unavailable: [], truncated: false });
    const first = renderAt("/operations/logs");
    expect(await messages()).toEqual([expect.stringContaining("bound only line")]);
    first.unmount();

    listOpsLogs.mockResolvedValue({ entries: [], unavailable: [], truncated: false });
    renderAt("/operations/logs?env=staging");
    expect(await screen.findByText("Nothing is connected in Staging")).toBeInTheDocument();
    expect(listOpsLogs).toHaveBeenCalledWith(expect.objectContaining({ env: "staging" }));
  });
});

describe("EntityLogs", () => {
  it("reads only the entity's component, without the entity column", async () => {
    renderAt("/operations/entity/c-api/logs");
    await logRows();
    expect(listOpsLogs.mock.calls[0][0].componentIds).toEqual(["c-api"]);
    expect(screen.queryByRole("columnheader", { name: "Entity" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /^Entities:/ })).not.toBeInTheDocument();
  });

  it("says the entity is not reporting where it has no environment", async () => {
    renderAt("/operations/entity/c-worker/logs");
    expect(await screen.findByText("Not reporting in Production")).toBeInTheDocument();
    expect(screen.getByText("jobs has no environment connected in Production, so there are no logs to read.")).toBeInTheDocument();
    expect(listOpsLogs).not.toHaveBeenCalled();
  });
});
