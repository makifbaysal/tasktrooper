import type { ComponentTable, TableColumn } from "@/api";
import { cellNumber, isValueColumn, type QueueRow, type TableSort } from "@/components/operations/queues/queueTables";

export type TableRow = QueueRow;

/** The first column of `preferred` the table has, else its first measure:
 * the one its bars are drawn on and its rows are first sorted by. */
export function headlineColumn(columns: TableColumn[], preferred: readonly string[]): TableColumn | undefined {
  for (const key of preferred) {
    const found = columns.find((c) => c.key === key);
    if (found && isValueColumn(found)) return found;
  }
  return columns.find(isValueColumn);
}

export function sortBy(column: TableColumn | undefined): TableSort | undefined {
  return column ? { key: column.key, direction: "desc" } : undefined;
}

/** The largest value of a column, for its bars; 0 when it has none above 0. */
export function columnMax(rows: TableRow[], column: TableColumn | undefined): number {
  if (!column) return 0;
  let max = 0;
  for (const row of rows) {
    const v = cellNumber(row[column.key]);
    if (v !== undefined && v > max) max = v;
  }
  return max;
}

/** A row's bar as a share of the column's largest value, 0..1. */
export function barShare(value: TableRow[string] | undefined, max: number): number {
  const v = cellNumber(value);
  if (v === undefined || max <= 0 || v <= 0) return 0;
  return Math.min(1, v / max);
}

// The columns an endpoints table names its measures by: the server's
// (`rpm`, `error_pct`, `p95_ms`), then the names other recipes give them.
export const ENDPOINT_THROUGHPUT = ["rpm", "throughput", "requests", "rps"] as const;
export const ENDPOINT_ERRORS = ["error_pct", "error_rate", "errors_pct"] as const;
export const ENDPOINT_LATENCY = ["p95_ms", "p95", "latency_p95", "duration_p95"] as const;

export type EndpointFlag = "errors" | "slowest";

/** The endpoints to call out, New Relic's way: the highest error rate (only
 * above zero) and the slowest p95 — each only when there is more than one
 * endpoint to compare with. A row can carry both. */
export function worstEndpoints(rows: TableRow[], columns: TableColumn[]): Map<TableRow, EndpointFlag[]> {
  const out = new Map<TableRow, EndpointFlag[]>();
  if (rows.length < 2) return out;
  const flag = (column: TableColumn | undefined, reason: EndpointFlag) => {
    if (!column) return;
    let max = 0;
    for (const row of rows) max = Math.max(max, cellNumber(row[column.key]) ?? 0);
    if (max <= 0) return;
    for (const row of rows) {
      if (cellNumber(row[column.key]) === max) out.set(row, [...(out.get(row) ?? []), reason]);
    }
  };
  flag(headlineColumn(columns, ENDPOINT_ERRORS), "errors");
  flag(headlineColumn(columns, ENDPOINT_LATENCY), "slowest");
  return out;
}

/** A value column's key that names an error share, so it reads in the destructive tone. */
export function isErrorColumn(column: TableColumn): boolean {
  return (ENDPOINT_ERRORS as readonly string[]).includes(column.key);
}

// What a web table's bars measure, by the names the web recipes give it.
export const WEB_MEASURES = ["views", "pageviews", "page_views", "visitors", "visits", "sessions", "users", "count"] as const;

// A crash issue's measures, users first: New Relic and the stores rank by who it hit.
export const CRASH_MEASURES = ["users", "reports", "events", "count"] as const;
export const SIGNATURE_MEASURES = ["weight", "reports", "count"] as const;

/** The tables of one answer that have this name, in the server's order. */
export function tablesNamed(tables: ComponentTable[] | null | undefined, name: string): ComponentTable[] {
  return (tables ?? []).filter((t) => t.table === name);
}

// Units that only count what the column already names (Views, Visitors,
// Users, Reports): the header says them once in its label.
const COUNT_UNITS = new Set(["views", "visitors", "users", "sessions", "events", "reports", "issues", "count"]);

export function isCountUnit(unit: string | undefined): boolean {
  return COUNT_UNITS.has(unit ?? "");
}

export function tableKey(table: Pick<ComponentTable, "data_source_id" | "recipe_id" | "table">, index: number): string {
  return `${table.table}:${table.data_source_id}:${table.recipe_id}:${index}`;
}

/** A crash table's `type` value as a word key (`crash`, `anr`, `non_fatal`,
 * `hangs`, `launches`, `disk_writes`, Sentry's levels), "" for none. */
export function crashTypeKey(value: TableRow[string] | undefined): string {
  if (typeof value !== "string") return "";
  return value.trim().toLowerCase().replace(/[^a-z0-9]+/g, "_");
}

/** An http(s) link from a table cell; anything else is not one. */
export function safeLink(value: TableRow[string] | undefined): string | undefined {
  if (typeof value !== "string") return undefined;
  try {
    const u = new URL(value);
    return u.protocol === "https:" || u.protocol === "http:" ? u.toString() : undefined;
  } catch {
    return undefined;
  }
}

export type TableWindowUnit = "minutes" | "hours" | "days";

/** The span of time a recipe table covers, before the instant it is read. */
export interface TableWindow {
  amount: number;
  unit: TableWindowUnit;
}

/** Where the console's docs list each source's table window. */
export const TABLE_WINDOWS_DOCS_URL = "https://tasktrooper.ai/docs/data-sources#table-windows";

const DAYS = (amount: number): TableWindow => ({ amount, unit: "days" });

// Mirrors the catalog's table recipes and what each kind's Table reads
// (server .ai/api-spec.md): a web analytics table and Play's issues over the
// last 7 days, Sentry's over 14, Datadog's query over the 10 minutes before
// the instant, New Relic's the hour before unless its NRQL says SINCE.
const RECIPE_TABLE_WINDOWS: Record<string, TableWindow> = {
  "ga4-web": DAYS(7),
  "plausible-web": DAYS(7),
  "vercel-web": DAYS(7),
  "posthog-web": DAYS(7),
  "newrelic-browser": DAYS(7),
  "play-vitals": DAYS(7),
  "sentry-mobile": DAYS(14),
  "datadog-apm": { amount: 10, unit: "minutes" },
  "newrelic-apm": { amount: 1, unit: "hours" },
};

const DAYS_PARAM_DEFAULTS: Record<string, number> = { ga4: 7, plausible: 7, vercel: 7, posthog: 7, google_play_vitals: 7, sentry: 14 };

function jsonDays(query: string): number | undefined {
  try {
    const parsed: unknown = JSON.parse(query);
    const days = parsed && typeof parsed === "object" ? (parsed as Record<string, unknown>).days : undefined;
    const n = typeof days === "string" ? Number(days) : days;
    return typeof n === "number" && Number.isInteger(n) && n > 0 ? n : undefined;
  } catch {
    return undefined;
  }
}

const NRQL_SINCE = /\bSINCE\s+(\d+)\s+(minute|hour|day|week)s?\s+ago\b/i;

function nrqlWindow(query: string): TableWindow | undefined | null {
  if (!/\bSINCE\b/i.test(query)) return null;
  const m = NRQL_SINCE.exec(query);
  if (!m) return undefined;
  const amount = Number(m[1]);
  const unit = m[2].toLowerCase();
  if (unit === "week") return DAYS(amount * 7);
  return { amount, unit: unit === "minute" ? "minutes" : unit === "hour" ? "hours" : "days" };
}

/** The window a table covers: its recipe's, or for a custom query its
 * source kind's (the query's own `days`, or NRQL's SINCE, when it sets one);
 * undefined when it cannot be told — the source's own recent window. */
export function tableWindow(
  table: Pick<ComponentTable, "recipe_id">,
  binding?: { data_source_kind?: string; query?: string } | null,
): TableWindow | undefined {
  if (table.recipe_id) return RECIPE_TABLE_WINDOWS[table.recipe_id];
  const kind = binding?.data_source_kind ?? "";
  const query = binding?.query ?? "";
  if (kind === "datadog") return { amount: 10, unit: "minutes" };
  if (kind === "newrelic") {
    const since = nrqlWindow(query);
    return since === null ? { amount: 1, unit: "hours" } : since;
  }
  const fallback = DAYS_PARAM_DEFAULTS[kind];
  return fallback === undefined ? undefined : DAYS(jsonDays(query) ?? fallback);
}
