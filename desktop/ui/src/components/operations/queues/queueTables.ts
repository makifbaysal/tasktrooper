import type { AlertRule, DeployEnvironment, QueueTable, TableColumn } from "@/api";
import { joinSignalValue, signalValueParts, type SignalFormatContext } from "@/components/operations/console/signalFormat";
import { QUEUE_SIGNALS } from "@/components/operations/queues/queueSignals";

export type QueueRow = QueueTable["rows"][number];
export type QueueCell = QueueRow[string];

// The columns that hold a table's backlog, most specific first: Kafka lag
// (direct and exporters), MSK's summed lag, RabbitMQ's messages or ready,
// NATS pending, Pub/Sub undelivered, SQS visible, a Redis list's length.
const BACKLOG_KEYS = ["lag", "sum_lag", "messages", "ready", "pending", "undelivered", "visible", "length", "backlog"];

/** A column with a unit holds a measure; one without names the row (topic,
 * partition, queue, consumer) or is an identifier (an offset, a member). */
export function isValueColumn(column: TableColumn): boolean {
  return (column.unit ?? "") !== "";
}

/** The column the table's backlog is in, if it has one. */
export function backlogColumn(columns: TableColumn[]): TableColumn | undefined {
  for (const key of BACKLOG_KEYS) {
    const found = columns.find((c) => c.key === key);
    if (found) return found;
  }
  return columns.find((c) => c.unit === "messages");
}

/** A cell as a number: a number, or a value column's numeric string. */
export function cellNumber(v: QueueCell | undefined): number | undefined {
  if (typeof v === "number") return Number.isFinite(v) ? v : undefined;
  if (typeof v === "string" && v.trim() !== "") {
    const n = Number(v);
    return Number.isFinite(n) ? n : undefined;
  }
  return undefined;
}

export type SortDirection = "asc" | "desc";

export interface TableSort {
  key: string;
  direction: SortDirection;
}

/** Biggest backlog first; a table with no backlog column keeps the server's order. */
export function defaultSort(columns: TableColumn[]): TableSort | undefined {
  const column = backlogColumn(columns) ?? columns.find(isValueColumn);
  return column ? { key: column.key, direction: "desc" } : undefined;
}

/** The next sort a header click gives: a measure starts with its largest, a label with A. */
export function toggleSort(current: TableSort | undefined, column: TableColumn): TableSort {
  if (current?.key === column.key) return { key: column.key, direction: current.direction === "desc" ? "asc" : "desc" };
  return { key: column.key, direction: isValueColumn(column) ? "desc" : "asc" };
}

const collator = new Intl.Collator(undefined, { numeric: true, sensitivity: "base" });

function compareCells(a: QueueCell | undefined, b: QueueCell | undefined, numeric: boolean): number {
  if (numeric) {
    const na = cellNumber(a);
    const nb = cellNumber(b);
    if (na !== undefined && nb !== undefined) return na - nb;
  }
  if (typeof a === "number" && typeof b === "number") return a - b;
  return collator.compare(String(a), String(b));
}

function blank(v: QueueCell | undefined): boolean {
  return v === null || v === undefined || v === "";
}

/** A sorted copy: a row without a value sorts last either way, and equal
 * rows keep the server's order (it orders by the key labels). */
export function sortRows(rows: QueueRow[], sort: TableSort | undefined, columns: TableColumn[]): QueueRow[] {
  if (!sort) return rows;
  const column = columns.find((c) => c.key === sort.key);
  if (!column) return rows;
  const numeric = isValueColumn(column);
  const sign = sort.direction === "asc" ? 1 : -1;
  return rows
    .map((row, i) => ({ row, i }))
    .sort((x, y) => {
      const a = x.row[sort.key];
      const b = y.row[sort.key];
      if (blank(a) || blank(b)) return blank(a) === blank(b) ? x.i - y.i : blank(a) ? 1 : -1;
      return sign * compareCells(a, b, numeric) || x.i - y.i;
    })
    .map(({ row }) => row);
}

export type WorstReason = "threshold" | "largest";

export interface WorstRows {
  rows: Set<QueueRow>;
  reason?: WorstReason;
}

/** The rows to call out: those above the alert threshold when a rule sets
 * one, else the row(s) holding the largest backlog — only when there is more
 * than one row and something is waiting. */
export function worstRows(rows: QueueRow[], column: TableColumn | undefined, threshold?: number): WorstRows {
  if (!column) return { rows: new Set() };
  const values = rows.map((row) => ({ row, v: cellNumber(row[column.key]) }));
  if (threshold !== undefined) {
    const above = values.filter(({ v }) => v !== undefined && v > threshold).map(({ row }) => row);
    return { rows: new Set(above), reason: above.length > 0 ? "threshold" : undefined };
  }
  if (rows.length < 2) return { rows: new Set() };
  const max = Math.max(...values.map(({ v }) => v ?? Number.NEGATIVE_INFINITY));
  if (!Number.isFinite(max) || max <= 0) return { rows: new Set() };
  const largest = values.filter(({ v }) => v === max).map(({ row }) => row);
  return { rows: new Set(largest), reason: "largest" };
}

/** The lowest threshold an enabled "above" rule on the backlog sets in the
 * environment: past it, a partition's or a queue's backlog alone would fire it. */
export function backlogThreshold(rules: AlertRule[] | null | undefined, env: DeployEnvironment, signal: string = QUEUE_SIGNALS.backlog): number | undefined {
  const found = (rules ?? [])
    .filter(
      (r) =>
        r.enabled &&
        r.environment === env &&
        r.signal === signal &&
        (r.condition?.op === "gt" || r.condition?.op === "rising") &&
        Number.isFinite(r.condition.threshold),
    )
    .map((r) => r.condition.threshold);
  return found.length > 0 ? Math.min(...found) : undefined;
}

export type QueueErrorAction = "edit" | "retry" | "none";

export interface QueueErrorView {
  /** Under `operations.queues.errors`. */
  key: string;
  /** A sentence of its own under `${key}Body`, in place of the server's. */
  body: boolean;
  action: QueueErrorAction;
  /** The edit action's label, under `operations.queues.errors`. */
  actionKey?: string;
}

const QUEUE_ERRORS: Record<string, QueueErrorView> = {
  needs_params: { key: "needsParams", body: true, action: "edit", actionKey: "fillParams" },
  not_found: { key: "notFound", body: true, action: "edit", actionKey: "rebind" },
  unsupported: { key: "unsupported", body: true, action: "none" },
  bad_query: { key: "badQuery", body: false, action: "edit", actionKey: "editBinding" },
  timeout: { key: "timeout", body: true, action: "retry" },
  upstream: { key: "upstream", body: false, action: "retry" },
};

const QUEUE_ERROR_FALLBACK: QueueErrorView = { key: "failed", body: false, action: "retry" };

/** How a table's `error.code` is answered: fix the binding, try again, or
 * only explain (retrying cannot help). */
export function queueErrorView(code: string | undefined): QueueErrorView {
  return (code && QUEUE_ERRORS[code]) || QUEUE_ERROR_FALLBACK;
}

/** A column the server names as one that names a row (`key_columns`). */
export function isKeyColumn(column: TableColumn, keyColumns: readonly string[] | null | undefined): boolean {
  return (keyColumns ?? []).includes(column.key);
}

/** A stable key for a row: its key columns' values when the table names
 * them, else its label columns'. */
export function rowKey(row: QueueRow, columns: TableColumn[], index: number, keyColumns?: readonly string[] | null): string {
  const named = columns.filter((c) => isKeyColumn(c, keyColumns));
  const labels = (named.length > 0 ? named : columns.filter((c) => !isValueColumn(c))).map((c) => String(row[c.key] ?? ""));
  return labels.length > 0 ? labels.join("\u0000") : String(index);
}

/** The params a table read with, as `k=v` in key order. */
export function paramChips(params: Record<string, string> | null | undefined): string[] {
  return Object.entries(params ?? {})
    .sort(([a], [b]) => (a < b ? -1 : a > b ? 1 : 0))
    .map(([k, v]) => `${k}=${v}`);
}

// Units a value is rescaled in (ms → s, s → min/h, bytes → KiB…): each cell
// carries its own; every other unit is the same for the whole column, so it
// goes in the header once.
const SCALED_UNITS = new Set(["ms", "s", "bytes"]);

/** The unit a column's header names, "" when each cell names its own or there is none. */
export function columnUnit(column: TableColumn, ctx: SignalFormatContext): string {
  if (!isValueColumn(column) || SCALED_UNITS.has(column.unit)) return "";
  return signalValueParts(1, column.unit, ctx).unit;
}

export interface CellText {
  text: string;
  /** The exact value, where the cell shows it rounded. */
  title?: string;
}

/** A cell as the table shows it; undefined when it holds no value. */
export function cellText(v: QueueCell | undefined, column: TableColumn, ctx: SignalFormatContext): CellText | undefined {
  if (blank(v)) return undefined;
  if (!isValueColumn(column)) return { text: String(v) };
  const n = cellNumber(v);
  if (n === undefined) return { text: String(v) };
  const parts = signalValueParts(n, column.unit, ctx);
  const text = SCALED_UNITS.has(column.unit) ? joinSignalValue(parts) : parts.value;
  const exact = new Intl.NumberFormat(ctx.locale, { maximumFractionDigits: 3 }).format(n);
  return { text, title: exact === parts.value ? undefined : exact };
}
