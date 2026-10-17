import { describe, expect, it } from "vitest";
import type { TableColumn } from "@/api";
import {
  CRASH_MEASURES,
  ENDPOINT_THROUGHPUT,
  WEB_MEASURES,
  barShare,
  columnMax,
  crashTypeKey,
  headlineColumn,
  isCountUnit,
  safeLink,
  tableWindow,
  tablesNamed,
  worstEndpoints,
} from "@/components/operations/tables/componentTables";

const ENDPOINTS: TableColumn[] = [
  { key: "http_route", label: "Endpoint", unit: "" },
  { key: "rpm", label: "Requests", unit: "req/min" },
  { key: "error_pct", label: "Errors", unit: "%" },
  { key: "p95_ms", label: "p95", unit: "ms" },
];

describe("componentTables", () => {
  it("picks the headline measure by name, else the first measure", () => {
    expect(headlineColumn(ENDPOINTS, ENDPOINT_THROUGHPUT)?.key).toBe("rpm");
    const pages: TableColumn[] = [
      { key: "path", label: "Path", unit: "" },
      { key: "visitors", label: "Visitors", unit: "visitors" },
      { key: "views", label: "Views", unit: "views" },
    ];
    expect(headlineColumn(pages, WEB_MEASURES)?.key).toBe("views");
    expect(headlineColumn([{ key: "users", label: "Users", unit: "" }], CRASH_MEASURES)).toBeUndefined();
    expect(headlineColumn([{ key: "x", label: "X", unit: "count" }], CRASH_MEASURES)?.key).toBe("x");
  });

  it("draws bars as a share of the column's largest value", () => {
    const rpm = ENDPOINTS[1];
    const rows = [{ rpm: 50 }, { rpm: 200 }, { rpm: null }, { rpm: "100" }];
    expect(columnMax(rows, rpm)).toBe(200);
    expect(barShare(50, 200)).toBe(0.25);
    expect(barShare("100", 200)).toBe(0.5);
    expect(barShare(null, 200)).toBe(0);
    expect(barShare(5, 0)).toBe(0);
  });

  it("calls out the most failing and the slowest endpoint, only among several", () => {
    const a = { http_route: "/a", rpm: 100, error_pct: 0, p95_ms: 900 };
    const b = { http_route: "/b", rpm: 10, error_pct: 4.2, p95_ms: 80 };
    const c = { http_route: "/c", rpm: 50, error_pct: 4.2, p95_ms: 120 };
    const flags = worstEndpoints([a, b, c], ENDPOINTS);
    expect(flags.get(a)).toEqual(["slowest"]);
    expect(flags.get(b)).toEqual(["errors"]);
    expect(flags.get(c)).toEqual(["errors"]);
    expect(worstEndpoints([a], ENDPOINTS).size).toBe(0);
    const healthy = worstEndpoints([{ ...a, p95_ms: 0 }, { ...b, error_pct: 0, p95_ms: 0 }], ENDPOINTS);
    expect(healthy.size).toBe(0);
  });

  it("reads crash types, links and count units", () => {
    expect(crashTypeKey("NON_FATAL")).toBe("non_fatal");
    expect(crashTypeKey("disk writes")).toBe("disk_writes");
    expect(crashTypeKey(3)).toBe("");
    expect(safeLink("https://play.google.com/console/x")).toBe("https://play.google.com/console/x");
    expect(safeLink("javascript:alert(1)")).toBeUndefined();
    expect(safeLink(null)).toBeUndefined();
    expect(isCountUnit("views")).toBe(true);
    expect(isCountUnit("req/min")).toBe(false);
  });

  it("keeps the tables of one name in the server's order", () => {
    const t = (table: string, recipe_id: string) => ({
      table,
      recipe_id,
      data_source_id: "ds",
      data_source_name: "s",
      columns: [],
      rows: [],
      truncated: false,
      error: null,
    });
    expect(tablesNamed([t("crashes", "a"), t("signatures", "b"), t("crashes", "c")], "crashes").map((x) => x.recipe_id)).toEqual(["a", "c"]);
    expect(tablesNamed(undefined, "crashes")).toEqual([]);
  });
});

describe("tableWindow", () => {
  it("knows each table recipe's window", () => {
    expect(tableWindow({ recipe_id: "ga4-web" })).toEqual({ amount: 7, unit: "days" });
    expect(tableWindow({ recipe_id: "newrelic-browser" })).toEqual({ amount: 7, unit: "days" });
    expect(tableWindow({ recipe_id: "play-vitals" })).toEqual({ amount: 7, unit: "days" });
    expect(tableWindow({ recipe_id: "sentry-mobile" })).toEqual({ amount: 14, unit: "days" });
    expect(tableWindow({ recipe_id: "datadog-apm" })).toEqual({ amount: 10, unit: "minutes" });
    expect(tableWindow({ recipe_id: "newrelic-apm" })).toEqual({ amount: 1, unit: "hours" });
    expect(tableWindow({ recipe_id: "otel-http" })).toBeUndefined();
    expect(tableWindow({ recipe_id: "app-store-metrics" })).toBeUndefined();
  });

  it("reads a custom query's window from its source kind and the query's own days or SINCE", () => {
    expect(tableWindow({ recipe_id: "" }, { data_source_kind: "posthog", query: '{"table": "pages"}' })).toEqual({ amount: 7, unit: "days" });
    expect(tableWindow({ recipe_id: "" }, { data_source_kind: "sentry", query: '{"project": "ios", "days": 30}' })).toEqual({ amount: 30, unit: "days" });
    expect(tableWindow({ recipe_id: "" }, { data_source_kind: "sentry", query: "{}" })).toEqual({ amount: 14, unit: "days" });
    expect(tableWindow({ recipe_id: "" }, { data_source_kind: "google_play_vitals", query: '{"days": "3"}' })).toEqual({ amount: 3, unit: "days" });
    expect(tableWindow({ recipe_id: "" }, { data_source_kind: "datadog", query: "avg:x{*} by {host}" })).toEqual({ amount: 10, unit: "minutes" });
    expect(tableWindow({ recipe_id: "" }, { data_source_kind: "newrelic", query: "SELECT count(*) FROM PageView FACET pageUrl" })).toEqual({ amount: 1, unit: "hours" });
    expect(tableWindow({ recipe_id: "" }, { data_source_kind: "newrelic", query: "SELECT count(*) FROM X SINCE 2 weeks ago" })).toEqual({ amount: 14, unit: "days" });
    expect(tableWindow({ recipe_id: "" }, { data_source_kind: "newrelic", query: "SELECT count(*) FROM X SINCE 30 minutes ago" })).toEqual({ amount: 30, unit: "minutes" });
    expect(tableWindow({ recipe_id: "" }, { data_source_kind: "newrelic", query: "SELECT count(*) FROM X SINCE yesterday" })).toBeUndefined();
    expect(tableWindow({ recipe_id: "" }, { data_source_kind: "prometheus", query: "up" })).toBeUndefined();
    expect(tableWindow({ recipe_id: "" })).toBeUndefined();
  });
});
