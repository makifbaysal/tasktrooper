import { useMemo } from "react";
import { defaultValueFormatter, fullTimeFormatter, seriesColorIndex, type ChartPoint } from "@/components/operations/charts/chartData";
import { useI18n } from "@/hooks/useI18n";
import { intlLocale } from "@/lib/languages";
import { cn } from "@/lib/utils";

export interface LiveValueItem {
  id: string;
  label: string;
  value: number | null;
  at?: number;
  colorIndex?: number;
}

/** One value per series from an answer of single points (`isInstantResult`). */
export function liveValueItems(series: ReadonlyArray<{ id: string; label: string; points: ChartPoint[]; colorIndex?: number }>): LiveValueItem[] {
  return series.map((s) => {
    const [at, value] = s.points[s.points.length - 1] ?? [];
    return {
      id: s.id,
      label: s.label,
      value: value === undefined || value === null || !Number.isFinite(value) ? null : value,
      at: at !== undefined && Number.isFinite(at) ? at : undefined,
      colorIndex: s.colorIndex,
    };
  });
}

interface LiveValueListProps {
  items: LiveValueItem[];
  /** Formats a value with its unit. */
  formatValue?: (v: number) => string;
  ariaLabel: string;
  /** Shown when there is no item at all. */
  emptyLabel?: string;
  /** Caps the list's height, scrolling past it; a single value is centred in it. */
  height?: number;
  className?: string;
}

/** The values an instant read answered, where a chart would be one dot per
 * series: a single value large, several as a list (largest first), each with
 * the instant it was read at. */
export function LiveValueList({ items, formatValue, ariaLabel, emptyLabel, height, className }: LiveValueListProps) {
  const { t, lang } = useI18n();
  const locale = intlLocale(lang);
  const format = useMemo(() => formatValue ?? defaultValueFormatter(locale), [formatValue, locale]);
  const time = useMemo(() => fullTimeFormatter(locale), [locale]);
  const sorted = useMemo(
    () => [...items].sort((a, b) => (b.value ?? Number.NEGATIVE_INFINITY) - (a.value ?? Number.NEGATIVE_INFINITY)),
    [items],
  );
  const at = items.find((i) => i.at !== undefined)?.at;
  const noValue = t("operations.chart.live.noValue");

  if (items.length === 0) {
    return (
      <div style={height ? { height } : undefined} className={cn("flex items-center justify-center text-caption text-muted-foreground", className)}>
        {emptyLabel ?? t("operations.chart.noData")}
      </div>
    );
  }

  if (items.length === 1) {
    const item = items[0];
    return (
      <div
        role="group"
        aria-label={ariaLabel}
        style={height ? { minHeight: height } : undefined}
        className={cn("flex flex-col items-center justify-center gap-1 rounded-lg border border-dashed border-border px-4 py-6 text-center", className)}
      >
        <p className="font-mono text-display font-semibold tabular-nums">{item.value === null ? "—" : format(item.value)}</p>
        {item.value === null && <p className="text-caption text-muted-foreground">{noValue}</p>}
        <p className="max-w-full truncate text-caption text-muted-foreground" title={item.label}>
          {item.label}
        </p>
        {item.at !== undefined && <p className="text-micro text-muted-foreground">{t("operations.chart.live.readAt", { time: time(item.at) })}</p>}
      </div>
    );
  }

  return (
    <div className={cn("space-y-1", className)}>
      <ul
        aria-label={ariaLabel}
        style={height ? { maxHeight: height } : undefined}
        className="divide-y divide-border/60 overflow-y-auto rounded-lg border border-border"
      >
        {sorted.map((item, i) => (
          <li key={item.id} className="flex items-center gap-2 px-3 py-1.5 text-caption">
            <span
              aria-hidden
              className="h-2 w-2 shrink-0 rounded-full"
              style={{ backgroundColor: `var(--chart-${seriesColorIndex(item, i)})` }}
            />
            <span className="min-w-0 flex-1 truncate font-mono" title={item.label}>
              {item.label}
            </span>
            <span className="shrink-0 font-mono font-medium tabular-nums">
              {item.value === null ? (
                <span title={noValue}>
                  <span aria-hidden>—</span>
                  <span className="sr-only">{noValue}</span>
                </span>
              ) : (
                format(item.value)
              )}
            </span>
          </li>
        ))}
      </ul>
      {at !== undefined && <p className="text-micro text-muted-foreground">{t("operations.chart.live.readAt", { time: time(at) })}</p>}
    </div>
  );
}
