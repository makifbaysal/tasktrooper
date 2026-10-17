import { ChevronDown, ChevronRight, OctagonAlert } from "lucide-react";
import { useMemo, useState } from "react";
import {
  formatSpanDuration,
  visibleRows,
  waterfallTicks,
  type Waterfall,
  type WaterfallRow,
} from "@/components/operations/traces/waterfallLayout";
import { Button } from "@/components/ui/button";
import { useI18n } from "@/hooks/useI18n";
import { intlLocale } from "@/lib/languages";
import { cn } from "@/lib/utils";

// Static class names, so the build keeps every service colour.
export const SERVICE_BG = ["bg-chart-1", "bg-chart-2", "bg-chart-3", "bg-chart-4", "bg-chart-5", "bg-chart-6", "bg-chart-7"];

export function serviceBg(colorIndex: number): string {
  return SERVICE_BG[(Math.max(1, colorIndex) - 1) % SERVICE_BG.length];
}

const INDENT_PX = 12;

interface TraceWaterfallProps {
  waterfall: Waterfall;
  selectedId?: string;
  onSelect: (spanId: string) => void;
}

/** New Relic's trace waterfall: one row per span in tree order, its bar on
 * the trace's time axis in its service's colour, failed spans marked, rows
 * under a span foldable. Each row is a button that opens the span. */
export function TraceWaterfall({ waterfall, selectedId, onSelect }: TraceWaterfallProps) {
  const { t, lang } = useI18n();
  const w = (key: string, params?: Record<string, string | number>) => t(`operations.telemetry.traces.waterfall.${key}`, params);
  const locale = intlLocale(lang);
  const [collapsed, setCollapsed] = useState<ReadonlySet<string>>(() => new Set());
  const rows = useMemo(() => visibleRows(waterfall.rows, collapsed), [waterfall.rows, collapsed]);
  const ticks = useMemo(() => waterfallTicks(waterfall.durationMs), [waterfall.durationMs]);
  const duration = (ms: number) => formatSpanDuration(ms, locale);

  const toggle = (id: string) =>
    setCollapsed((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });

  const describe = (row: WaterfallRow) =>
    w("rowLabel", {
      service: row.span.service || w("noService"),
      name: row.span.name || w("noName"),
      offset: duration(row.offsetMs),
      duration: duration(row.durationMs),
    }) + (row.error ? ` · ${w("failed")}` : "");

  return (
    <div className="min-w-0 overflow-x-auto rounded-lg border border-border">
      <div className="min-w-[640px]">
        <div className="grid grid-cols-[minmax(14rem,2fr)_minmax(0,3fr)] border-b border-border bg-muted/40 text-micro font-medium tracking-wide text-muted-foreground uppercase">
          <div className="px-3 py-1.5">{w("span")}</div>
          <div className="relative h-7" aria-hidden>
            {ticks.map((tick) => (
              <span
                key={tick.ms}
                className={cn(
                  "absolute top-1.5 px-1 font-mono normal-case tabular-nums",
                  tick.at === 0 ? "" : tick.at >= 0.95 ? "-translate-x-full" : "-translate-x-1/2",
                )}
                style={{ left: `${tick.at * 100}%` }}
              >
                {duration(tick.ms)}
              </span>
            ))}
          </div>
        </div>
        <ul aria-label={w("listLabel")}>
          {rows.map((row) => {
            const folded = collapsed.has(row.span.id);
            const selected = row.span.id === selectedId;
            const label = describe(row);
            return (
              <li
                key={row.span.id}
                className={cn(
                  "grid grid-cols-[minmax(14rem,2fr)_minmax(0,3fr)] border-b border-border/50 last:border-b-0",
                  selected ? "bg-info/10" : "hover:bg-muted/30",
                )}
              >
                <div className="flex min-w-0 items-center gap-1 py-1 pr-2" style={{ paddingLeft: `${8 + row.depth * INDENT_PX}px` }}>
                  {row.childCount > 0 ? (
                    <Button
                      variant="ghost"
                      size="icon"
                      className="h-5 w-5 shrink-0 [&_svg]:size-3.5"
                      aria-expanded={!folded}
                      aria-label={folded ? w("expand", { name: row.span.name }) : w("collapse", { name: row.span.name })}
                      onClick={() => toggle(row.span.id)}
                    >
                      {folded ? <ChevronRight aria-hidden /> : <ChevronDown aria-hidden />}
                    </Button>
                  ) : (
                    <span className="w-5 shrink-0" aria-hidden />
                  )}
                  <Button
                    variant="ghost"
                    size="sm"
                    className="h-auto min-w-0 flex-1 justify-start gap-1.5 px-1 py-0.5 text-left font-normal"
                    aria-pressed={selected}
                    aria-label={label}
                    title={label}
                    onClick={() => onSelect(row.span.id)}
                  >
                    <span className={cn("h-2 w-2 shrink-0 rounded-full", serviceBg(row.colorIndex))} aria-hidden />
                    {row.error && <OctagonAlert className="h-3.5 w-3.5 shrink-0 text-destructive" aria-hidden />}
                    <span className="shrink-0 text-micro text-muted-foreground">{row.span.service || w("noService")}</span>
                    <span className="min-w-0 truncate font-mono text-caption">{row.span.name || w("noName")}</span>
                    {folded && row.childCount > 0 && (
                      <span className="shrink-0 rounded bg-muted px-1 text-micro text-muted-foreground tabular-nums">
                        +{row.childCount}
                      </span>
                    )}
                  </Button>
                </div>
                <div className="relative min-w-0 py-1" aria-hidden>
                  {ticks.map((tick) => (
                    <span key={tick.ms} className="absolute inset-y-0 w-px bg-border/60" style={{ left: `${tick.at * 100}%` }} />
                  ))}
                  <span
                    className={cn(
                      "absolute top-1/2 h-3 -translate-y-1/2 rounded-sm",
                      serviceBg(row.colorIndex),
                      row.error && "ring-2 ring-destructive ring-offset-1 ring-offset-background",
                      selected && "outline-2 outline-foreground",
                    )}
                    style={{ left: `${row.left * 100}%`, width: `${row.width * 100}%` }}
                  />
                  <span
                    className={cn(
                      "absolute top-1/2 -translate-y-1/2 px-1 font-mono text-micro tabular-nums whitespace-nowrap",
                      row.error ? "text-destructive" : "text-muted-foreground",
                    )}
                    style={
                      row.left + row.width > 0.8
                        ? { right: `${(1 - row.left) * 100}%` }
                        : { left: `${(row.left + row.width) * 100}%` }
                    }
                  >
                    {duration(row.durationMs)}
                  </span>
                </div>
              </li>
            );
          })}
        </ul>
      </div>
    </div>
  );
}
