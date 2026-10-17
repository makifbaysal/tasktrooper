import { formatSpanDuration, type Waterfall } from "@/components/operations/traces/waterfallLayout";
import { serviceBg } from "@/components/operations/traces/TraceWaterfall";
import { Button } from "@/components/ui/button";
import { useI18n } from "@/hooks/useI18n";
import { intlLocale } from "@/lib/languages";
import { cn } from "@/lib/utils";

const TH = "px-3 py-1.5 text-left text-micro font-medium tracking-wide text-muted-foreground uppercase whitespace-nowrap";
const TD = "px-3 py-1.5 align-middle";

interface SpanTableProps {
  waterfall: Waterfall;
  selectedId?: string;
  onSelect: (spanId: string) => void;
}

/** The waterfall as a plain table — depth, service, operation, start offset,
 * duration, status — for a screen reader or a copy into a ticket. */
export function SpanTable({ waterfall, selectedId, onSelect }: SpanTableProps) {
  const { t, lang } = useI18n();
  const w = (key: string, params?: Record<string, string | number>) => t(`operations.telemetry.traces.waterfall.${key}`, params);
  const locale = intlLocale(lang);
  return (
    <div className="overflow-x-auto rounded-lg border border-border">
      <table className="w-full border-collapse text-caption" aria-label={w("tableLabel")}>
        <thead className="bg-muted/40">
          <tr className="border-b border-border">
            <th scope="col" className={TH}>{w("columns.depth")}</th>
            <th scope="col" className={TH}>{w("columns.service")}</th>
            <th scope="col" className={TH}>{w("columns.operation")}</th>
            <th scope="col" className={cn(TH, "text-right")}>{w("columns.start")}</th>
            <th scope="col" className={cn(TH, "text-right")}>{w("columns.duration")}</th>
            <th scope="col" className={TH}>{w("columns.status")}</th>
          </tr>
        </thead>
        <tbody>
          {waterfall.rows.map((row) => (
            <tr
              key={row.span.id}
              className={cn("border-b border-border/50 last:border-b-0", row.span.id === selectedId && "bg-info/10")}
            >
              <td className={cn(TD, "font-mono tabular-nums text-muted-foreground")}>{row.depth}</td>
              <td className={TD}>
                <span className="inline-flex items-center gap-1.5">
                  <span className={cn("h-2 w-2 rounded-full", serviceBg(row.colorIndex))} aria-hidden />
                  {row.span.service || w("noService")}
                </span>
              </td>
              <td className={cn(TD, "max-w-[24rem]")}>
                <Button
                  variant="link"
                  size="sm"
                  className="h-auto max-w-full justify-start truncate p-0 font-mono text-caption"
                  onClick={() => onSelect(row.span.id)}
                >
                  {row.span.name || w("noName")}
                </Button>
              </td>
              <td className={cn(TD, "text-right font-mono tabular-nums")}>+{formatSpanDuration(row.offsetMs, locale)}</td>
              <td className={cn(TD, "text-right font-mono tabular-nums")}>{formatSpanDuration(row.durationMs, locale)}</td>
              <td className={cn(TD, row.error && "font-medium text-destructive")}>{w(`status.${row.span.status || "unset"}`)}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
