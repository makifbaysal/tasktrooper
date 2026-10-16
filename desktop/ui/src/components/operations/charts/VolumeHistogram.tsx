import { ChartColumn, Table2 } from "lucide-react";
import { useMemo, useState } from "react";
import { fullTimeFormatter, timeTickFormatter } from "@/components/operations/charts/chartData";
import { bucketTotal, niceCeiling, type HistogramBucket } from "@/components/operations/charts/histogramData";
import { Button } from "@/components/ui/button";
import { EmptyState } from "@/components/ui/empty-state";
import { Skeleton } from "@/components/ui/skeleton";
import { useI18n } from "@/hooks/useI18n";
import { intlLocale } from "@/lib/languages";
import { cn } from "@/lib/utils";

export interface HistogramSeries {
  id: string;
  label: string;
  /** A background utility on a colour token (`bg-destructive`, `bg-chart-1`, …),
   * so both themes and a theme switch need nothing from here. */
  swatchClassName: string;
}

export interface VolumeHistogramProps {
  buckets: HistogramBucket[];
  /** Stacked bottom to top in this order. */
  series: HistogramSeries[];
  ariaLabel: string;
  height?: number;
  showTableToggle?: boolean;
  loading?: boolean;
  emptyLabel?: string;
}

const TICKS = 5;

/** Stacked count bars over time, the bar-chart sibling of `TimeSeriesChart`:
 * the same legend row, table toggle, tooltip and empty state. Drawn as HTML
 * boxes on the colour tokens rather than on a canvas, so the bars stay crisp
 * at any width and the theme switch needs no redraw. */
export function VolumeHistogram({
  buckets,
  series,
  ariaLabel,
  height = 120,
  showTableToggle = false,
  loading = false,
  emptyLabel,
}: VolumeHistogramProps) {
  const { t, lang } = useI18n();
  const locale = intlLocale(lang);
  const [view, setView] = useState<"chart" | "table">("chart");
  const [hover, setHover] = useState<number | null>(null);

  const totals = useMemo(() => buckets.map(bucketTotal), [buckets]);
  const max = totals.reduce((a, b) => Math.max(a, b), 0);
  const filled = max > 0;
  const top = niceCeiling(max);
  const number = useMemo(() => new Intl.NumberFormat(locale, { notation: "compact", maximumFractionDigits: 1 }), [locale]);
  const fullTime = useMemo(() => fullTimeFormatter(locale), [locale]);
  const span = buckets.length > 0 ? buckets[buckets.length - 1].to - buckets[0].from : 0;
  const tick = useMemo(() => timeTickFormatter(locale, span), [locale, span]);

  const legend = series.length >= 2 && (
    <ul className="flex flex-wrap items-center gap-x-4 gap-y-1 text-caption text-muted-foreground" aria-label={t("operations.chart.legend")}>
      {series.map((s) => (
        <li key={s.id} className="inline-flex items-center gap-1.5">
          <span aria-hidden className={cn("h-2 w-3 shrink-0 rounded-[2px]", s.swatchClassName)} />
          {s.label}
        </li>
      ))}
    </ul>
  );

  const toggle = showTableToggle && filled && (
    <Button
      variant={view === "table" ? "secondary" : "ghost"}
      size="sm"
      className="ml-auto"
      aria-pressed={view === "table"}
      onClick={() => setView((v) => (v === "chart" ? "table" : "chart"))}
    >
      <Table2 aria-hidden />
      {t("operations.chart.showTable")}
    </Button>
  );

  let body;
  if (loading && !filled) {
    body = <Skeleton role="status" style={{ height }} className="w-full" aria-label={t("operations.chart.loading")} />;
  } else if (!filled) {
    body = (
      <div style={{ height }} className="flex items-center justify-center rounded-lg border border-dashed border-border">
        <EmptyState icon={ChartColumn} title={emptyLabel ?? t("operations.chart.noData")} className="py-0" />
      </div>
    );
  } else if (view === "table") {
    body = (
      <div className="overflow-auto rounded-lg border border-border" style={{ maxHeight: Math.max(height, 160) }}>
        <table className="w-full text-caption">
          <caption className="sr-only">{ariaLabel}</caption>
          <thead className="sticky top-0 bg-card">
            <tr className="border-b border-border text-left text-muted-foreground">
              <th scope="col" className="px-3 py-2 font-medium">
                {t("operations.chart.time")}
              </th>
              {series.map((s) => (
                <th key={s.id} scope="col" className="px-3 py-2 text-right font-medium">
                  {s.label}
                </th>
              ))}
              <th scope="col" className="px-3 py-2 text-right font-medium">
                {t("operations.opsQuery.histogram.total")}
              </th>
            </tr>
          </thead>
          <tbody>
            {buckets.map((b, i) =>
              totals[i] === 0 ? null : (
                <tr key={b.from} className="border-b border-border/50 last:border-0">
                  <th scope="row" className="whitespace-nowrap px-3 py-1.5 text-left font-normal text-muted-foreground">
                    {fullTime(b.from)}
                  </th>
                  {series.map((s, si) => (
                    <td key={s.id} className="px-3 py-1.5 text-right font-mono tabular-nums">
                      {b.counts[si] ?? 0}
                    </td>
                  ))}
                  <td className="px-3 py-1.5 text-right font-mono font-semibold tabular-nums">{totals[i]}</td>
                </tr>
              ),
            )}
          </tbody>
        </table>
      </div>
    );
  } else {
    const n = buckets.length;
    const tickAt = Array.from(new Set(Array.from({ length: TICKS }, (_, k) => Math.round((k * (n - 1)) / (TICKS - 1)))));
    const mid = top / 2;
    const hovered = hover !== null ? buckets[hover] : undefined;
    const hoverX = hover !== null ? ((hover + 0.5) / n) * 100 : 0;
    body = (
      <div>
        <div className="flex" style={{ height }}>
          <div aria-hidden className="relative w-10 shrink-0 pr-2 text-right font-mono text-micro tabular-nums text-muted-foreground">
            <span className="absolute top-0 right-2 -translate-y-1/2">{number.format(top)}</span>
            {Number.isInteger(mid) && <span className="absolute top-1/2 right-2 -translate-y-1/2">{number.format(mid)}</span>}
            <span className="absolute right-2 bottom-0 translate-y-1/2">0</span>
          </div>
          <div
            role="img"
            aria-label={ariaLabel}
            className="relative min-w-0 flex-1 border-b border-border"
            onPointerLeave={() => setHover(null)}
          >
            <span aria-hidden className="absolute inset-x-0 top-0 border-t border-border/60" />
            <span aria-hidden className="absolute inset-x-0 top-1/2 border-t border-border/60" />
            <div className="absolute inset-0 flex items-end gap-px">
              {buckets.map((b, i) => (
                <div
                  key={b.from}
                  data-testid="histogram-bar"
                  className={cn("flex h-full min-w-0 flex-1 flex-col-reverse", hover === i && "bg-muted/60")}
                  onPointerEnter={() => setHover(i)}
                >
                  {series.map((s, si) =>
                    (b.counts[si] ?? 0) > 0 ? (
                      <div key={s.id} className={cn("w-full shrink-0", s.swatchClassName)} style={{ height: `${(b.counts[si] / top) * 100}%` }} />
                    ) : null,
                  )}
                </div>
              ))}
            </div>
            {hovered && (
              <div
                className="pointer-events-none absolute top-0 z-10 min-w-36 rounded-md border border-border bg-popover px-2.5 py-2 text-caption text-popover-foreground shadow-[var(--shadow-overlay)]"
                style={hoverX <= 50 ? { left: `calc(${hoverX}% + 10px)` } : { right: `calc(${100 - hoverX}% + 10px)` }}
              >
                <div className="mb-1 text-micro text-muted-foreground">
                  {fullTime(hovered.from)} – {fullTime(hovered.to)}
                </div>
                {series.map((s, si) => (
                  <div key={s.id} className="flex items-center gap-2 py-0.5">
                    <span aria-hidden className={cn("h-2 w-3 shrink-0 rounded-[2px]", s.swatchClassName)} />
                    <span className="font-semibold tabular-nums text-foreground">{hovered.counts[si] ?? 0}</span>
                    <span className="truncate text-muted-foreground">{s.label}</span>
                  </div>
                ))}
                <div className="mt-1 flex items-center gap-2 border-t border-border pt-1">
                  <span className="font-semibold tabular-nums text-foreground">{totals[hover ?? 0]}</span>
                  <span className="text-muted-foreground">{t("operations.opsQuery.histogram.total")}</span>
                </div>
              </div>
            )}
          </div>
        </div>
        <div aria-hidden className="relative ml-10 h-5 font-mono text-micro text-muted-foreground">
          {tickAt.map((i) => (
            <span
              key={i}
              className={cn(
                "absolute top-1 whitespace-nowrap",
                i === 0 ? "left-0" : i === n - 1 ? "right-0" : "-translate-x-1/2",
              )}
              style={i === 0 || i === n - 1 ? undefined : { left: `${(i / n) * 100}%` }}
            >
              {tick(buckets[i].from)}
            </span>
          ))}
        </div>
      </div>
    );
  }

  return (
    <div className="space-y-2" aria-busy={loading || undefined}>
      {(legend || toggle) && (
        <div className="flex min-h-8 flex-wrap items-center gap-2">
          {legend}
          {toggle}
        </div>
      )}
      {body}
    </div>
  );
}
