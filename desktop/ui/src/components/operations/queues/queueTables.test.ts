import { describe, expect, it } from "vitest";
import type { AlertRule, TableColumn } from "@/api";
import {
  backlogColumn,
  backlogThreshold,
  cellText,
  columnUnit,
  defaultSort,
  isKeyColumn,
  queueErrorView,
  rowKey,
  sortRows,
  toggleSort,
  worstRows,
  type QueueRow,
} from "@/components/operations/queues/queueTables";
import { en } from "@/locales/en";

function t(key: string, params?: Record<string, string | number>): string {
  let node: unknown = en;
  for (const part of key.split(".")) node = (node as Record<string, unknown> | undefined)?.[part];
  if (typeof node !== "string") return key;
  return node.replace(/\{(\w+)\}/g, (_, k: string) => String(params?.[k] ?? `{${k}}`));
}

const ctx = { locale: "en", t };

const KAFKA: TableColumn[] = [
  { key: "topic", label: "Topic", unit: "" },
  { key: "partition", label: "Partition", unit: "" },
  { key: "committed_offset", label: "Committed offset", unit: "" },
  { key: "lag", label: "Lag", unit: "messages" },
  { key: "member_id", label: "Member", unit: "" },
];

const rows: QueueRow[] = [
  { topic: "invoices", partition: 0, committed_offset: 10, lag: 4, member_id: "m-1" },
  { topic: "invoices", partition: 10, committed_offset: 7, lag: null, member_id: null },
  { topic: "invoices", partition: 2, committed_offset: 9, lag: 120, member_id: "m-2" },
  { topic: "invoices", partition: 1, committed_offset: 3, lag: 120, member_id: "m-1" },
];

function rule(partial: Partial<AlertRule> & Pick<AlertRule, "condition">): AlertRule {
  return {
    id: "r",
    name: "r",
    component_id: "c",
    environment: "production",
    signal: "queue.backlog",
    severity: "high",
    channels: ["desktop"],
    open_incident: true,
    enabled: true,
    state: "ok",
    state_since: "",
    created_at: "",
    updated_at: "",
    ...partial,
  };
}

describe("queue tables", () => {
  it("finds the backlog column by key, else by the messages unit", () => {
    expect(backlogColumn(KAFKA)?.key).toBe("lag");
    expect(backlogColumn([{ key: "queue", label: "Queue", unit: "" }, { key: "depth", label: "Depth", unit: "messages" }])?.key).toBe("depth");
    expect(backlogColumn([{ key: "queue", label: "Queue", unit: "" }])).toBeUndefined();
  });

  it("sorts by the largest backlog first, rows without a value last, ties in the server's order", () => {
    const sort = defaultSort(KAFKA);
    expect(sort).toEqual({ key: "lag", direction: "desc" });
    expect(sortRows(rows, sort, KAFKA).map((r) => r.partition)).toEqual([2, 1, 0, 10]);
    expect(sortRows(rows, { key: "lag", direction: "asc" }, KAFKA).map((r) => r.partition)).toEqual([0, 2, 1, 10]);
  });

  it("sorts a label column naturally, partition 2 before 10", () => {
    expect(sortRows(rows, { key: "partition", direction: "asc" }, KAFKA).map((r) => r.partition)).toEqual([0, 1, 2, 10]);
    expect(sortRows(rows, { key: "member_id", direction: "asc" }, KAFKA).map((r) => r.member_id)).toEqual(["m-1", "m-1", "m-2", null]);
  });

  it("starts a measure at its largest and a label at A, and flips on a second click", () => {
    expect(toggleSort(undefined, KAFKA[3])).toEqual({ key: "lag", direction: "desc" });
    expect(toggleSort(undefined, KAFKA[0])).toEqual({ key: "topic", direction: "asc" });
    expect(toggleSort({ key: "lag", direction: "desc" }, KAFKA[3])).toEqual({ key: "lag", direction: "asc" });
  });

  it("calls out the rows above the alert threshold, else the largest backlog", () => {
    const above = worstRows(rows, KAFKA[3], 100);
    expect(above.reason).toBe("threshold");
    expect([...above.rows].map((r) => r.partition).sort()).toEqual([1, 2]);
    expect(worstRows(rows, KAFKA[3], 500)).toEqual({ rows: new Set(), reason: undefined });
    const largest = worstRows(rows, KAFKA[3]);
    expect(largest.reason).toBe("largest");
    expect(largest.rows.size).toBe(2);
    expect(worstRows([rows[0]], KAFKA[3]).rows.size).toBe(0);
    expect(worstRows([{ lag: 0 }, { lag: 0 }], KAFKA[3]).rows.size).toBe(0);
  });

  it("takes the lowest threshold of the environment's enabled above-rules on the backlog", () => {
    const rules = [
      rule({ condition: { op: "gt", threshold: 500, for_seconds: 0, window_seconds: 300 } }),
      rule({ condition: { op: "rising", threshold: 200, for_seconds: 0, window_seconds: 300 } }),
      rule({ condition: { op: "gt", threshold: 10, for_seconds: 0, window_seconds: 300 }, enabled: false }),
      rule({ condition: { op: "gt", threshold: 5, for_seconds: 0, window_seconds: 300 }, environment: "staging" }),
      rule({ condition: { op: "lt", threshold: 1, for_seconds: 0, window_seconds: 300 } }),
      rule({ condition: { op: "gt", threshold: 1, for_seconds: 0, window_seconds: 300 }, signal: "queue.backlog_age" }),
    ];
    expect(backlogThreshold(rules, "production")).toBe(200);
    expect(backlogThreshold([], "production")).toBeUndefined();
  });

  it("answers each error code: fix the binding, retry, or explain", () => {
    expect(queueErrorView("needs_params")).toMatchObject({ action: "edit", actionKey: "fillParams", body: true });
    expect(queueErrorView("not_found")).toMatchObject({ action: "edit", actionKey: "rebind" });
    expect(queueErrorView("bad_query")).toMatchObject({ action: "edit", actionKey: "editBinding" });
    expect(queueErrorView("timeout")).toMatchObject({ action: "retry", body: true });
    expect(queueErrorView("upstream")).toMatchObject({ action: "retry", body: false });
    expect(queueErrorView("unsupported")).toMatchObject({ action: "none", body: true });
    expect(queueErrorView("something_new")).toMatchObject({ key: "failed", action: "retry" });
  });

  it("puts a column's steady unit in its header and a rescaled one in each cell", () => {
    expect(columnUnit(KAFKA[3], ctx)).toBe("messages");
    expect(columnUnit({ key: "consume_rate", label: "Consumed", unit: "msg/s" }, ctx)).toBe("msg/s");
    expect(columnUnit({ key: "oldest_age", label: "Oldest", unit: "s" }, ctx)).toBe("");
    expect(columnUnit(KAFKA[0], ctx)).toBe("");
    expect(cellText(12_345, KAFKA[3], ctx)).toEqual({ text: "12.3K", title: "12,345" });
    expect(cellText(42, KAFKA[3], ctx)).toEqual({ text: "42", title: undefined });
    expect(cellText(300, { key: "oldest_age", label: "Oldest", unit: "s" }, ctx)?.text).toBe("5 min");
    expect(cellText(123456789, KAFKA[2], ctx)).toEqual({ text: "123456789" });
    expect(cellText(null, KAFKA[3], ctx)).toBeUndefined();
  });
});

describe("key columns", () => {
  const columns: TableColumn[] = [
    { key: "topic", label: "Topic", unit: "" },
    { key: "partition", label: "Partition", unit: "" },
    { key: "member", label: "Member", unit: "" },
    { key: "lag", label: "Lag", unit: "messages" },
  ];
  it("names a row by the columns the server calls keys, else by its label columns", () => {
    const row = { topic: "orders", partition: "3", member: "c-1", lag: 40 };
    expect(isKeyColumn(columns[0], ["topic", "partition"])).toBe(true);
    expect(isKeyColumn(columns[2], ["topic", "partition"])).toBe(false);
    expect(isKeyColumn(columns[0], undefined)).toBe(false);
    expect(rowKey(row, columns, 0, ["topic", "partition"])).toBe("orders\u0000" + "3");
    expect(rowKey(row, columns, 0)).toBe("orders\u00003\u0000c-1");
  });
});
