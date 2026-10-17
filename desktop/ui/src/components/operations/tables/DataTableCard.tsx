import { ArrowDown, ArrowUp, ArrowUpDown, CloudOff, Database } from "lucide-react";
import { useMemo, useState, type ReactNode } from "react";
import { Link } from "react-router-dom";
import type { ComponentTable, TableColumn } from "@/api";
import type { SignalFormatContext } from "@/components/operations/console/signalFormat";
import {
  cellText,
  columnUnit,
  isKeyColumn,
  isValueColumn,
  queueErrorView,
  rowKey,
  sortRows,
  toggleSort,
  type TableSort,
} from "@/components/operations/queues/queueTables";
import { barShare, columnMax, isCountUnit, type TableRow } from "@/components/operations/tables/componentTables";
import { TableReadAt } from "@/components/operations/tables/TableReadAt";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { EmptyState } from "@/components/ui/empty-state";
import { useI18n } from "@/hooks/useI18n";
import { cn } from "@/lib/utils";

export const DATA_TABLE_PAGE = 100;

const NONE: string[] = [];

export interface RowFlag {
  /** A short word for the row's flag column and its legend. */
  label: string;
  tone: "destructive" | "warning";
  icon: ReactNode;
}

export interface DataTableCardProps {
  table: ComponentTable;
  formatCtx: SignalFormatContext;
  /** Defaults to the source's name. */
  title?: ReactNode;
  /** The column whose values are drawn as bars behind the numbers. */
  barColumn?: TableColumn;
  defaultSort?: TableSort;
  /** Rows to call out, with why; a legend lists the reasons shown. */
  flags?: Map<TableRow, RowFlag[]>;
  /** A cell drawn its own way (a link, a badge, a time); undefined falls back to the plain cell. */
  renderCell?: (column: TableColumn, row: TableRow) => ReactNode | undefined;
  /** Columns read but not shown as columns of their own (a link a row already carries). */
  hiddenColumns?: string[];
  /** Columns that name a row but read as prose, not code. */
  textColumns?: string[];
  /** Opens the binding editor on the table's `signal`, for a read error the binding fixes. */
  onEditBinding?: () => void;
  /** Where the bindings behind a table that cannot be read are fixed, when there is no editor to open. */
  signalsHref?: string;
  /** The answer's `read_at`, shown in the header. */
  readAt?: string;
  /** A line under the recipe in the header: the span of time the table covers. */
  meta?: ReactNode;
  onRetry: () => void;
  retrying?: boolean;
}

const TH = "px-3 py-1.5 text-left text-micro font-medium tracking-wide text-muted-foreground uppercase whitespace-nowrap";
const TD = "px-3 py-1.5 align-middle";

/** One recipe table as a dense, sortable list: New Relic's transactions or
 * page views table — bars on the headline measure, flagged rows called out,
 * or why the table could not be read. */
export function DataTableCard({
  table,
  formatCtx,
  title,
  barColumn,
  defaultSort,
  flags,
  renderCell,
  hiddenColumns = NONE,
  textColumns = NONE,
  onEditBinding,
  signalsHref,
  readAt,
  meta,
  onRetry,
  retrying = false,
}: DataTableCardProps) {
  const { t } = useI18n();
  const d = (key: string, params?: Record<string, string | number>) => t(`operations.telemetry.table.${key}`, params);
  const columns = useMemo(() => (table.columns ?? []).filter((c) => !hiddenColumns.includes(c.key)), [table.columns, hiddenColumns]);
  const rows = useMemo(() => table.rows ?? [], [table.rows]);
  const [sort, setSort] = useState<TableSort | undefined>(defaultSort);
  const [showAll, setShowAll] = useState(false);
  const sorted = useMemo(() => sortRows(rows, sort, table.columns ?? []), [rows, sort, table.columns]);
  const shown = showAll ? sorted : sorted.slice(0, DATA_TABLE_PAGE);
  const max = useMemo(() => columnMax(rows, barColumn), [rows, barColumn]);
  const keys = useMemo(() => {
    const seen = new Map<string, number>();
    return shown.map((row, i) => {
      const base = rowKey(row, columns, i, table.key_columns);
      const n = seen.get(base) ?? 0;
      seen.set(base, n + 1);
      return n === 0 ? base : `${base}\u0000${n}`;
    });
  }, [shown, columns, table.key_columns]);
  const flagged = flags && flags.size > 0;
  const legend = useMemo(() => {
    const seen = new Map<string, RowFlag>();
    for (const list of flags?.values() ?? []) for (const f of list) if (!seen.has(f.label)) seen.set(f.label, f);
    return [...seen.values()];
  }, [flags]);

  const titleId = `data-table-${table.table}-${table.data_source_id}-${table.recipe_id}`;
  const source = title ?? (table.data_source_name || d("unnamedSource"));

  const header = (
    <div className="flex flex-wrap items-start justify-between gap-2 px-3 pt-3">
      <div className="min-w-0 space-y-0.5">
        <h3 id={titleId} className="flex min-w-0 items-center gap-1.5 text-caption font-semibold">
          <Database className="h-3.5 w-3.5 shrink-0 text-muted-foreground" aria-hidden />
          <span className="truncate">{source}</span>
        </h3>
        <p className="flex flex-wrap items-center gap-x-2 gap-y-0.5 text-micro text-muted-foreground">
          <span className={table.recipe_id ? "font-mono" : undefined}>
            {table.recipe_id ? d("recipe", { recipe: table.recipe_id }) : d("customQuery")}
          </span>
          {meta && (
            <>
              <span aria-hidden>·</span>
              {meta}
            </>
          )}
        </p>
      </div>
      <div className="flex flex-col items-end gap-0.5">
        {!table.error && <span className="text-micro text-muted-foreground tabular-nums">{d("rows", { count: rows.length })}</span>}
        <TableReadAt readAt={readAt} />
      </div>
    </div>
  );

  if (table.error) {
    const view = queueErrorView(table.error.code);
    const description = view.body ? d(`errors.${view.key}Body`) : table.error.message || undefined;
    return (
      <Card className="min-w-0">
        {header}
        <EmptyState
          icon={CloudOff}
          variant={view.action === "none" ? "default" : "critical"}
          title={d(`errors.${view.key}`)}
          description={description}
          className="py-6"
          action={
            view.action === "edit" && onEditBinding ? (
              <Button size="sm" onClick={onEditBinding}>
                {t(`operations.queues.errors.${view.actionKey}`)}
              </Button>
            ) : view.action === "edit" && signalsHref ? (
              <Button size="sm" asChild>
                <Link to={signalsHref}>{d("errors.openSignals")}</Link>
              </Button>
            ) : view.action === "retry" ? (
              <Button variant="outline" size="sm" onClick={onRetry} disabled={retrying}>
                {d("errors.retry")}
              </Button>
            ) : undefined
          }
        />
        {view.body && table.error.message && (
          <p className="px-3 pb-3 text-center font-mono text-micro break-words text-muted-foreground">{table.error.message}</p>
        )}
      </Card>
    );
  }

  const sortIcon = (column: TableColumn) => {
    if (sort?.key !== column.key) return <ArrowUpDown className="h-3 w-3 opacity-50" aria-hidden />;
    return sort.direction === "asc" ? <ArrowUp className="h-3 w-3" aria-hidden /> : <ArrowDown className="h-3 w-3" aria-hidden />;
  };

  return (
    <Card className="min-w-0 overflow-hidden">
      {header}
      {sorted.length === 0 ? (
        <p className="px-3 py-4 text-caption text-muted-foreground">{d("noRows")}</p>
      ) : (
        <div className="mt-2 overflow-x-auto">
          <table className="w-full border-collapse text-caption" aria-labelledby={titleId}>
            <thead className="bg-muted/40">
              <tr className="border-y border-border">
                {flagged && (
                  <th scope="col" className={cn(TH, "w-6 px-1.5")}>
                    <span className="sr-only">{d("flag")}</span>
                  </th>
                )}
                {columns.map((column) => {
                  const value = isValueColumn(column) && !isKeyColumn(column, table.key_columns);
                  const unit = isCountUnit(column.unit) ? "" : columnUnit(column, formatCtx);
                  const label = column.label || column.key;
                  return (
                    <th
                      key={column.key}
                      scope="col"
                      className={cn(TH, value && "text-right")}
                      aria-sort={sort?.key === column.key ? (sort.direction === "asc" ? "ascending" : "descending") : undefined}
                    >
                      <Button
                        variant="ghost"
                        size="sm"
                        className={cn(
                          "-mx-1.5 h-auto gap-1 px-1.5 py-0.5 text-micro font-medium tracking-wide text-muted-foreground uppercase",
                          value && "flex-row-reverse",
                        )}
                        onClick={() => setSort(toggleSort(sort, column))}
                        aria-label={d("sortBy", { column: label })}
                      >
                        <span>
                          {label}
                          {unit && <span className="ml-1 font-normal normal-case">({unit})</span>}
                        </span>
                        {sortIcon(column)}
                      </Button>
                    </th>
                  );
                })}
              </tr>
            </thead>
            <tbody>
              {shown.map((row, i) => {
                const rowFlags = flags?.get(row) ?? [];
                const worst = rowFlags.find((f) => f.tone === "destructive") ?? rowFlags[0];
                return (
                  <tr
                    key={keys[i]}
                    className={cn(
                      "border-b border-border/60 last:border-b-0 hover:bg-muted/30",
                      worst?.tone === "destructive" && "bg-destructive/5",
                      worst?.tone === "warning" && "bg-warning/5",
                    )}
                  >
                    {flagged && (
                      <td className={cn(TD, "w-6 px-1.5")}>
                        {rowFlags.length > 0 && (
                          <span className="inline-flex items-center gap-0.5" title={rowFlags.map((f) => f.label).join(" · ")}>
                            {rowFlags.map((f) => (
                              <span key={f.label} className={f.tone === "destructive" ? "text-destructive" : "text-warning"} aria-hidden>
                                {f.icon}
                              </span>
                            ))}
                            <span className="sr-only">{rowFlags.map((f) => f.label).join(", ")}</span>
                          </span>
                        )}
                      </td>
                    )}
                    {columns.map((column) => {
                      const custom = renderCell?.(column, row);
                      const key = isKeyColumn(column, table.key_columns);
                      const value = isValueColumn(column) && !key;
                      if (custom !== undefined) {
                        return (
                          <td key={column.key} className={cn(TD, value ? "text-right" : "max-w-[24rem]", key && "font-medium text-foreground")}>
                            {custom}
                          </td>
                        );
                      }
                      const cell = cellText(row[column.key], column, formatCtx);
                      const share = column.key === barColumn?.key ? barShare(row[column.key], max) : 0;
                      const prose = textColumns.includes(column.key);
                      return (
                        <td
                          key={column.key}
                          className={cn(
                            TD,
                            value ? "text-right font-mono tabular-nums whitespace-nowrap" : cn("max-w-[24rem] truncate", !prose && "font-mono"),
                            key && "font-medium text-foreground",
                          )}
                          title={cell?.title ?? (!value && cell ? cell.text : undefined)}
                        >
                          {cell ? (
                            column.key === barColumn?.key ? (
                              <span className="inline-flex min-w-[7rem] flex-col items-end gap-0.5">
                                <span>{cell.text}</span>
                                <span className="block h-1 w-full overflow-hidden rounded-full bg-muted" aria-hidden>
                                  <span className="block h-full rounded-full bg-chart-1" style={{ width: `${share * 100}%` }} />
                                </span>
                              </span>
                            ) : (
                              cell.text
                            )
                          ) : (
                            <span className="text-muted-foreground">
                              <span aria-hidden>—</span>
                              <span className="sr-only">{d("noValue")}</span>
                            </span>
                          )}
                        </td>
                      );
                    })}
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}
      {(legend.length > 0 || table.truncated || sorted.length > shown.length) && (
        <div className="flex flex-wrap items-center gap-x-3 gap-y-1 border-t border-border px-3 py-2 text-micro text-muted-foreground">
          {legend.map((f) => (
            <span key={f.label} className="inline-flex items-center gap-1">
              <span className={f.tone === "destructive" ? "text-destructive" : "text-warning"} aria-hidden>
                {f.icon}
              </span>
              {f.label}
            </span>
          ))}
          {table.truncated && <span>{d("truncated", { count: rows.length })}</span>}
          {sorted.length > shown.length && (
            <Button variant="link" size="sm" className="h-auto p-0 text-micro" onClick={() => setShowAll(true)}>
              {d("showAll", { count: sorted.length })}
            </Button>
          )}
        </div>
      )}
    </Card>
  );
}
