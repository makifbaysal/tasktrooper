import { ArrowDown, ArrowUp, ArrowUpDown, CloudOff, Database, OctagonAlert, TriangleAlert } from "lucide-react";
import { useMemo, useState } from "react";
import type { QueueTable, TableColumn } from "@/api";
import type { SignalFormatContext } from "@/components/operations/console/signalFormat";
import {
  backlogColumn,
  cellText,
  columnUnit,
  defaultSort,
  isKeyColumn,
  isValueColumn,
  paramChips,
  queueErrorView,
  rowKey,
  sortRows,
  toggleSort,
  worstRows,
  type TableSort,
} from "@/components/operations/queues/queueTables";
import { TableReadAt } from "@/components/operations/tables/TableReadAt";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { EmptyState } from "@/components/ui/empty-state";
import { useI18n } from "@/hooks/useI18n";
import { cn } from "@/lib/utils";

// Enough to see the worst partitions at once; the rest are a click away.
export const QUEUE_TABLE_PAGE = 100;

interface QueueTableCardProps {
  table: QueueTable;
  formatCtx: SignalFormatContext;
  /** The backlog alert's threshold in this environment, when a rule sets one. */
  threshold?: number;
  /** Opens the binding editor on the binding the table was read through. */
  onEditBinding: () => void;
  onRetry: () => void;
  retrying?: boolean;
  /** The answer's `read_at`, shown in the header. */
  readAt?: string;
}

const TH = "px-3 py-1.5 text-left text-micro font-medium tracking-wide text-muted-foreground uppercase whitespace-nowrap";
const TD = "px-3 py-1.5 align-middle whitespace-nowrap";

/** One source's queues as they are now: a dense, sortable table (largest
 * backlog first), its worst rows called out, or why it could not be read. */
export function QueueTableCard({ table, formatCtx, threshold, onEditBinding, onRetry, retrying = false, readAt }: QueueTableCardProps) {
  const { t } = useI18n();
  const q = (key: string, params?: Record<string, string | number>) => t(`operations.queues.${key}`, params);
  const columns = table.columns ?? [];
  const [sort, setSort] = useState<TableSort | undefined>(() => defaultSort(columns));
  const [showAll, setShowAll] = useState(false);
  const backlog = backlogColumn(columns);
  const sorted = useMemo(() => sortRows(table.rows ?? [], sort, columns), [table.rows, sort, columns]);
  const worst = useMemo(() => worstRows(table.rows ?? [], backlog, threshold), [table.rows, backlog, threshold]);
  const shown = showAll ? sorted : sorted.slice(0, QUEUE_TABLE_PAGE);
  const keys = useMemo(() => {
    const seen = new Map<string, number>();
    return shown.map((row, i) => {
      const base = rowKey(row, columns, i, table.key_columns);
      const n = seen.get(base) ?? 0;
      seen.set(base, n + 1);
      return n === 0 ? base : `${base}\u0000${n}`;
    });
  }, [shown, columns, table.key_columns]);

  const source = table.data_source_name || q("table.gone");
  const system = table.messaging_system ? systemLabel(t, table.messaging_system) : "";
  const chips = paramChips(table.params);
  const titleId = `queue-table-${table.signal_binding_id}-${table.data_source_id}`;
  const thresholdText = threshold !== undefined && backlog ? cellText(threshold, backlog, formatCtx)?.text : undefined;

  const header = (
    <div className="flex flex-wrap items-start justify-between gap-2 px-3 pt-3">
      <div className="min-w-0 space-y-0.5">
        <h3 id={titleId} className="flex min-w-0 items-center gap-1.5 text-caption font-semibold">
          <Database className="h-3.5 w-3.5 shrink-0 text-muted-foreground" aria-hidden />
          <span className="truncate">{source}</span>
          {system && <Badge variant="secondary" className="shrink-0 px-2 py-0 text-micro">{system}</Badge>}
        </h3>
        <p className="flex flex-wrap items-center gap-x-2 gap-y-1 text-micro text-muted-foreground">
          <span className={table.recipe_id ? "font-mono" : undefined}>
            {table.recipe_id ? q("table.recipe", { recipe: table.recipe_id }) : q("table.customQuery")}
          </span>
          {chips.map((chip) => (
            <span key={chip} className="rounded bg-muted px-1.5 py-px font-mono text-foreground">
              {chip}
            </span>
          ))}
        </p>
      </div>
      <div className="flex flex-col items-end gap-0.5">
        {!table.error && (
          <span className="text-micro text-muted-foreground tabular-nums">{q("table.rows", { count: (table.rows ?? []).length })}</span>
        )}
        <TableReadAt readAt={readAt} />
      </div>
    </div>
  );

  if (table.error) {
    const view = queueErrorView(table.error.code);
    const description = view.body ? q(`errors.${view.key}Body`) : table.error.message || undefined;
    return (
      <Card className="min-w-0">
        {header}
        <EmptyState
          icon={CloudOff}
          variant={view.action === "none" ? "default" : "critical"}
          title={q(`errors.${view.key}`)}
          description={description}
          className="py-6"
          action={
            view.action === "edit" ? (
              <Button size="sm" onClick={onEditBinding}>
                {q(`errors.${view.actionKey}`)}
              </Button>
            ) : view.action === "retry" ? (
              <Button variant="outline" size="sm" onClick={onRetry} disabled={retrying}>
                {q("errors.retry")}
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
        <p className="px-3 py-4 text-caption text-muted-foreground">{q("table.noRows")}</p>
      ) : (
        <div className="mt-2 overflow-x-auto">
          <table className="w-full border-collapse text-caption" aria-labelledby={titleId}>
            <thead className="bg-muted/40">
              <tr className="border-y border-border">
                <th scope="col" className={cn(TH, "w-6 px-1.5")}>
                  <span className="sr-only">{q("table.flag")}</span>
                </th>
                {columns.map((column) => {
                  const value = isValueColumn(column) && !isKeyColumn(column, table.key_columns);
                  const unit = columnUnit(column, formatCtx);
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
                        aria-label={q("table.sortBy", { column: label })}
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
                const flagged = worst.rows.has(row);
                const reason = flagged ? worst.reason : undefined;
                return (
                  <tr
                    key={keys[i]}
                    className={cn(
                      "border-b border-border/60 last:border-b-0",
                      reason === "threshold" && "bg-destructive/5",
                      reason === "largest" && "bg-warning/5",
                    )}
                  >
                    <td className={cn(TD, "w-6 px-1.5")}>
                      {reason === "threshold" && (
                        <span title={q("table.worst.threshold", { threshold: thresholdText ?? String(threshold) })}>
                          <OctagonAlert className="h-3.5 w-3.5 text-destructive" aria-hidden />
                          <span className="sr-only">{q("table.worst.threshold", { threshold: thresholdText ?? String(threshold) })}</span>
                        </span>
                      )}
                      {reason === "largest" && (
                        <span title={q("table.worst.largest")}>
                          <TriangleAlert className="h-3.5 w-3.5 text-warning" aria-hidden />
                          <span className="sr-only">{q("table.worst.largest")}</span>
                        </span>
                      )}
                    </td>
                    {columns.map((column) => {
                      const cell = cellText(row[column.key], column, formatCtx);
                      const key = isKeyColumn(column, table.key_columns);
                      const value = isValueColumn(column) && !key;
                      return (
                        <td
                          key={column.key}
                          className={cn(
                            TD,
                            value ? "text-right font-mono tabular-nums" : "max-w-[18rem] truncate font-mono",
                            key && "font-medium text-foreground",
                            value && column.key === backlog?.key && flagged && (reason === "threshold" ? "font-semibold text-destructive" : "font-semibold"),
                          )}
                          title={cell?.title ?? (!value && cell ? cell.text : undefined)}
                        >
                          {cell ? (
                            cell.text
                          ) : (
                            <span className="text-muted-foreground">
                              <span aria-hidden>—</span>
                              <span className="sr-only">{q("table.noValue")}</span>
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
      {(worst.reason || table.truncated || sorted.length > shown.length) && (
        <div className="flex flex-wrap items-center gap-x-3 gap-y-1 border-t border-border px-3 py-2 text-micro text-muted-foreground">
          {worst.reason === "threshold" && <span>{q("table.worst.legendThreshold", { threshold: thresholdText ?? String(threshold) })}</span>}
          {worst.reason === "largest" && <span>{q("table.worst.legendLargest")}</span>}
          {table.truncated && <span>{q("table.truncated", { count: (table.rows ?? []).length })}</span>}
          {sorted.length > shown.length && (
            <Button variant="link" size="sm" className="h-auto p-0 text-micro" onClick={() => setShowAll(true)}>
              {q("table.showAll", { count: sorted.length })}
            </Button>
          )}
        </div>
      )}
    </Card>
  );
}

/** A messaging system's name, else as the server sent it. */
export function systemLabel(t: (key: string) => string, system: string): string {
  const key = `operations.queues.systems.${system}`;
  const text = t(key);
  return text === key ? system : text;
}
