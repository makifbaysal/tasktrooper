import { OctagonAlert } from "lucide-react";
import { useMemo, useState } from "react";
import { Link } from "react-router-dom";
import type { TraceSummary } from "@/api";
import { formatSpanDuration } from "@/components/operations/traces/waterfallLayout";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { useI18n } from "@/hooks/useI18n";
import { intlLocale } from "@/lib/languages";
import { cn, formatDate, formatRelativeTime } from "@/lib/utils";

const TH = "px-3 py-1.5 text-left text-micro font-medium tracking-wide text-muted-foreground uppercase whitespace-nowrap";
const TD = "px-3 py-1.5 align-middle";

export type TraceOrder = "newest" | "slowest";

export function orderTraces(traces: TraceSummary[], order: TraceOrder): TraceSummary[] {
  if (order === "newest") return traces;
  return traces
    .map((tr, i) => ({ tr, i }))
    .sort((a, b) => (b.tr.duration_ms ?? 0) - (a.tr.duration_ms ?? 0) || a.i - b.i)
    .map(({ tr }) => tr);
}

interface TraceSearchListProps {
  traces: TraceSummary[];
  hrefFor: (trace: TraceSummary) => string;
}

/** The trace search's answer, New Relic's distributed tracing list: when,
 * the root span, how long (a bar against the slowest), how many spans
 * matched, and whether it failed. Newest first, or slowest first. */
export function TraceSearchList({ traces, hrefFor }: TraceSearchListProps) {
  const { t, lang } = useI18n();
  const l = (key: string, params?: Record<string, string | number>) => t(`operations.telemetry.traces.list.${key}`, params);
  const locale = intlLocale(lang);
  const [order, setOrder] = useState<TraceOrder>("newest");
  const shown = useMemo(() => orderTraces(traces, order), [traces, order]);
  const slowest = useMemo(() => Math.max(0, ...traces.map((tr) => tr.duration_ms ?? 0)), [traces]);
  const failed = traces.filter((tr) => tr.error).length;

  return (
    <Card className="min-w-0 overflow-hidden">
      <div className="flex flex-wrap items-center justify-between gap-2 px-3 pt-3">
        <p className="text-micro text-muted-foreground tabular-nums">
          {l("count", { count: traces.length })}
          {failed > 0 && <span className="ml-2 text-destructive">{l("failedCount", { count: failed })}</span>}
        </p>
        <div className="flex items-center gap-1" role="group" aria-label={l("order")}>
          {(["newest", "slowest"] as const).map((o) => (
            <Button
              key={o}
              variant={order === o ? "secondary" : "ghost"}
              size="sm"
              className="h-7 px-2 text-micro"
              aria-pressed={order === o}
              onClick={() => setOrder(o)}
            >
              {l(`orders.${o}`)}
            </Button>
          ))}
        </div>
      </div>
      <div className="mt-2 overflow-x-auto">
        <table className="w-full min-w-[640px] border-collapse text-caption" aria-label={l("label")}>
          <thead className="bg-muted/40">
            <tr className="border-y border-border">
              <th scope="col" className={cn(TH, "w-40")}>{l("columns.start")}</th>
              <th scope="col" className={TH}>{l("columns.root")}</th>
              <th scope="col" className={cn(TH, "w-48 text-right")}>{l("columns.duration")}</th>
              <th scope="col" className={cn(TH, "w-20 text-right")}>{l("columns.spans")}</th>
              <th scope="col" className={cn(TH, "w-24")}>{l("columns.status")}</th>
            </tr>
          </thead>
          <tbody>
            {shown.map((tr) => {
              const share = slowest > 0 ? Math.min(1, (tr.duration_ms ?? 0) / slowest) : 0;
              return (
                <tr key={tr.id} className={cn("border-b border-border/60 last:border-b-0 hover:bg-muted/30", tr.error && "bg-destructive/5")}>
                  <td className={cn(TD, "whitespace-nowrap font-mono tabular-nums text-muted-foreground")} title={formatDate(tr.start)}>
                    {formatRelativeTime(tr.start, lang)}
                  </td>
                  <td className={cn(TD, "max-w-0 w-full")}>
                    <Link to={hrefFor(tr)} className="group block min-w-0" aria-label={l("open", { name: tr.root_name || tr.id })}>
                      <span className="block truncate font-mono text-caption text-foreground group-hover:underline">
                        {tr.root_name || l("noRoot")}
                      </span>
                      <span className="block truncate text-micro text-muted-foreground">
                        {tr.root_service || l("noService")}
                        <span className="ml-2 font-mono">{tr.id.slice(0, 16)}</span>
                      </span>
                    </Link>
                  </td>
                  <td className={cn(TD, "text-right")}>
                    <span className="inline-flex w-full flex-col items-end gap-0.5">
                      <span className="font-mono tabular-nums">{formatSpanDuration(tr.duration_ms ?? 0, locale)}</span>
                      <span className="block h-1 w-full overflow-hidden rounded-full bg-muted" aria-hidden>
                        <span className={cn("block h-full rounded-full", tr.error ? "bg-destructive" : "bg-chart-1")} style={{ width: `${share * 100}%` }} />
                      </span>
                    </span>
                  </td>
                  <td className={cn(TD, "text-right font-mono tabular-nums")}>{tr.span_count}</td>
                  <td className={TD}>
                    {tr.error ? (
                      <Badge variant="destructive" className="gap-1 px-1.5 py-0 text-micro">
                        <OctagonAlert className="h-3 w-3" aria-hidden />
                        {l("error")}
                      </Badge>
                    ) : (
                      <span className="text-micro text-muted-foreground">{l("ok")}</span>
                    )}
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>
    </Card>
  );
}
