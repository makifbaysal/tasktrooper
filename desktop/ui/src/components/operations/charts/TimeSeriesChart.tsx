import { ChartLine, Table2 } from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import { Link, useInRouterContext } from "react-router-dom";
import uPlot from "uplot";
import "uplot/dist/uPlot.min.css";
import {
  alignSeries,
  chartTableRows,
  defaultValueFormatter,
  fullTimeFormatter,
  hasChartData,
  nearestMarker,
  seriesColorIndex,
  timeTickFormatter,
  withAlpha,
  type AlignedSeries,
} from "@/components/operations/charts/chartData";
import { useChartTheme } from "@/components/operations/charts/chartTheme";
import { Button } from "@/components/ui/button";
import { EmptyState } from "@/components/ui/empty-state";
import { Skeleton } from "@/components/ui/skeleton";
import { useI18n } from "@/hooks/useI18n";
import { intlLocale } from "@/lib/languages";
import { keepEqual } from "@/lib/stableState";
import { cn } from "@/lib/utils";

export interface ChartSeries {
  id: string;
  label: string;
  points: Array<[number, number | null]>;
  colorIndex?: number;
  dashed?: boolean;
}

export interface DeployMarker {
  at: number;
  label: string;
  href?: string;
}

export interface Threshold {
  value: number;
  label?: string;
}

/** A span of the time axis to shade (unix ms), such as when a rule would have fired. */
export interface ChartBand {
  from: number;
  to: number;
}

export interface TimeSeriesChartProps {
  series: ChartSeries[];
  ariaLabel: string;
  unit?: string;
  formatValue?: (v: number) => string;
  deployMarkers?: DeployMarker[];
  thresholds?: Threshold[];
  bands?: ChartBand[];
  height?: number;
  syncKey?: string;
  area?: boolean;
  showTableToggle?: boolean;
  loading?: boolean;
  emptyLabel?: string;
  /** Pins the time axis to a window (unix ms), so charts side by side line up
   * on the range picked rather than on where each one's data happens to start. */
  xRange?: [number, number];
}

interface MarkerSpot {
  left: number;
  top: number;
  label: string;
  href: string;
}

interface Latest {
  aligned: AlignedSeries;
  series: ChartSeries[];
  format: (v: number) => string;
  unit?: string;
  markers: DeployMarker[];
  thresholds: Threshold[];
  bands: ChartBand[];
  fullTime: (ms: number) => string;
  deployLabel: (label: string) => string;
  xRange?: [number, number];
}

const NO_MARKERS: DeployMarker[] = [];
const NO_THRESHOLDS: Threshold[] = [];
const NO_BANDS: ChartBand[] = [];

function toPlotData(aligned: AlignedSeries): uPlot.AlignedData {
  return [aligned.xs, ...aligned.ys] as uPlot.AlignedData;
}

function withUnit(text: string, unit?: string): string {
  if (!unit) return text;
  return unit === "%" ? `${text}%` : `${text} ${unit}`;
}

export function TimeSeriesChart({
  series,
  ariaLabel,
  unit,
  formatValue,
  deployMarkers,
  thresholds,
  bands,
  height = 220,
  syncKey,
  area = false,
  showTableToggle = false,
  loading = false,
  emptyLabel,
  xRange,
}: TimeSeriesChartProps) {
  const { t, lang } = useI18n();
  const locale = intlLocale(lang);
  const theme = useChartTheme();
  const [view, setView] = useState<"chart" | "table">("chart");
  const [spots, setSpots] = useState<MarkerSpot[]>([]);
  const plotHost = useRef<HTMLDivElement>(null);
  const plotRef = useRef<uPlot | null>(null);

  const markers = deployMarkers ?? NO_MARKERS;
  const lines = thresholds ?? NO_THRESHOLDS;
  const shaded = bands ?? NO_BANDS;
  const aligned = useMemo(() => alignSeries(series), [series]);
  const filled = useMemo(() => hasChartData(series), [series]);
  const format = useMemo(() => formatValue ?? defaultValueFormatter(locale), [formatValue, locale]);
  const fullTime = useMemo(() => fullTimeFormatter(locale), [locale]);

  const latest = useRef<Latest | null>(null);
  latest.current = {
    aligned,
    series,
    format,
    unit,
    markers,
    thresholds: lines,
    bands: shaded,
    fullTime,
    deployLabel: (label) => t("operations.chart.deployAt", { label }),
    xRange,
  };

  const structure = series.map((s, i) => `${s.id}\u0000${s.label}\u0000${seriesColorIndex(s, i)}\u0000${s.dashed ? 1 : 0}`).join("\u0001");
  const showPlot = filled && view === "chart";

  useEffect(() => {
    const host = plotHost.current;
    const now = latest.current;
    if (!host || !showPlot || !now) return;
    const colors = now.series.map((s, i) => theme.series[seriesColorIndex(s, i) - 1]);
    const tip = document.createElement("div");
    tip.className =
      "pointer-events-none absolute z-10 hidden min-w-36 rounded-md border border-border bg-popover px-2.5 py-2 text-caption text-popover-foreground shadow-[var(--shadow-overlay)]";
    let hovered = false;

    const font = `11px ${theme.fontSans}`;

    const renderTooltip = (u: uPlot) => {
      const idx = u.cursor.idx;
      const cur = latest.current;
      if (!hovered || idx == null || !cur) {
        tip.classList.add("hidden");
        return;
      }
      const at = u.data[0][idx];
      tip.replaceChildren();
      const head = document.createElement("div");
      head.className = "mb-1 text-micro text-muted-foreground";
      head.textContent = cur.fullTime(at);
      tip.appendChild(head);
      const pxSpan = Math.abs(u.posToVal(8, "x") - u.posToVal(0, "x"));
      const marker = nearestMarker(cur.markers, at, pxSpan);
      if (marker) {
        const row = document.createElement("div");
        row.className = "mb-1 font-mono text-micro text-muted-foreground";
        row.textContent = cur.deployLabel(marker.label);
        tip.appendChild(row);
      }
      cur.series.forEach((s, i) => {
        const v = u.data[i + 1]?.[idx];
        const row = document.createElement("div");
        row.className = "flex items-center gap-2 py-0.5";
        const key = document.createElement("span");
        key.className = "h-0.5 w-3 shrink-0 rounded-full";
        key.style.backgroundColor = colors[i];
        const value = document.createElement("span");
        value.className = "font-semibold tabular-nums text-foreground";
        value.textContent = v == null ? "—" : withUnit(cur.format(v), cur.unit);
        const label = document.createElement("span");
        label.className = "truncate text-muted-foreground";
        label.textContent = s.label;
        row.append(key, value, label);
        tip.appendChild(row);
      });
      tip.classList.remove("hidden");
      const overWidth = u.over.clientWidth;
      const overHeight = u.over.clientHeight;
      const left = u.cursor.left ?? 0;
      const top = u.cursor.top ?? 0;
      const tipWidth = tip.offsetWidth;
      const tipHeight = tip.offsetHeight;
      const x = left + 12 + tipWidth > overWidth ? Math.max(0, left - 12 - tipWidth) : left + 12;
      const y = Math.min(Math.max(0, top - tipHeight / 2), Math.max(0, overHeight - tipHeight));
      tip.style.left = `${x}px`;
      tip.style.top = `${y}px`;
    };

    const drawOverlays = (u: uPlot) => {
      const cur = latest.current;
      if (!cur) return;
      const ctx = u.ctx;
      const dpr = uPlot.pxRatio;
      const { left, top, width, height: plotHeight } = u.bbox;
      const next: MarkerSpot[] = [];
      ctx.save();
      ctx.fillStyle = withAlpha(theme.destructive, 0.12);
      for (const band of cur.bands) {
        const x0 = Math.max(left, u.valToPos(band.from, "x", true));
        const x1 = Math.min(left + width, u.valToPos(band.to, "x", true));
        if (!Number.isFinite(x0) || !Number.isFinite(x1) || x1 < x0) continue;
        ctx.fillRect(x0, top, Math.max(x1 - x0, dpr), plotHeight);
      }
      for (const line of cur.thresholds) {
        const y = u.valToPos(line.value, "y", true);
        if (!Number.isFinite(y) || y < top || y > top + plotHeight) continue;
        ctx.strokeStyle = theme.destructive;
        ctx.lineWidth = dpr;
        ctx.setLineDash([4 * dpr, 4 * dpr]);
        ctx.beginPath();
        ctx.moveTo(left, y);
        ctx.lineTo(left + width, y);
        ctx.stroke();
        if (line.label) {
          ctx.setLineDash([]);
          ctx.font = `${11 * dpr}px ${theme.fontSans}`;
          ctx.textAlign = "right";
          ctx.textBaseline = "bottom";
          ctx.fillStyle = theme.axis;
          ctx.fillText(line.label, left + width - 4 * dpr, y - 3 * dpr);
        }
      }
      let labelEdge = -Infinity;
      const diamondY = top - 8 * dpr;
      for (const m of [...cur.markers].sort((a, b) => a.at - b.at)) {
        const x = u.valToPos(m.at, "x", true);
        if (!Number.isFinite(x) || x < left || x > left + width) continue;
        ctx.strokeStyle = theme.axis;
        ctx.lineWidth = dpr;
        ctx.setLineDash([3 * dpr, 3 * dpr]);
        ctx.beginPath();
        ctx.moveTo(x, diamondY);
        ctx.lineTo(x, top + plotHeight);
        ctx.stroke();
        ctx.setLineDash([]);
        const r = 4 * dpr;
        ctx.fillStyle = theme.axis;
        ctx.beginPath();
        ctx.moveTo(x, diamondY - r);
        ctx.lineTo(x + r, diamondY);
        ctx.lineTo(x, diamondY + r);
        ctx.lineTo(x - r, diamondY);
        ctx.closePath();
        ctx.fill();
        ctx.font = `${10 * dpr}px ${theme.fontMono}`;
        ctx.textBaseline = "middle";
        const textWidth = ctx.measureText(m.label).width;
        const rightRoom = left + width - (x + r + 3 * dpr);
        const towardLeft = textWidth > rightRoom;
        const start = towardLeft ? x - r - 3 * dpr - textWidth : x + r + 3 * dpr;
        if (start > labelEdge) {
          ctx.textAlign = "left";
          ctx.fillText(m.label, start, diamondY);
          labelEdge = start + textWidth + 6 * dpr;
        }
        if (m.href) next.push({ left: x / dpr, top: diamondY / dpr, label: m.label, href: m.href });
      }
      ctx.restore();
      setSpots((prev) => keepEqual(prev, next));
    };

    const yRange: uPlot.Range.Function = (_u, min, max) => {
      const cur = latest.current;
      const values = (cur?.thresholds ?? []).map((l) => l.value).filter(Number.isFinite);
      let lo = Math.min(min, ...values);
      let hi = Math.max(max, ...values);
      if (!Number.isFinite(lo) || !Number.isFinite(hi)) return [0, 1];
      if (lo === hi) {
        hi = lo === 0 ? 1 : hi + Math.abs(hi) * 0.1;
        lo = lo === 0 ? 0 : lo - Math.abs(lo) * 0.1;
      }
      const [a, b] = uPlot.rangeNum(lo, hi, 0.1, true);
      const top = b ?? hi;
      const bottom = a ?? lo;
      return [lo >= 0 ? Math.max(0, bottom) : bottom, top];
    };

    const opts: uPlot.Options = {
      width: Math.max(host.clientWidth, 1),
      height,
      ms: 1,
      padding: [20, 12, 0, 0],
      legend: { show: false },
      cursor: {
        y: false,
        drag: { x: false, y: false, setScale: false },
        points: {
          size: 8,
          width: 2,
          stroke: () => theme.surface,
          fill: (_u, si) => colors[si - 1] ?? theme.axis,
        },
        ...(syncKey ? { sync: { key: syncKey, setSeries: false } } : {}),
      },
      scales: {
        x: { time: true, range: (_u, min, max) => latest.current?.xRange ?? [min, max] },
        y: { range: yRange },
      },
      axes: [
        {
          stroke: theme.axis,
          font,
          grid: { stroke: theme.grid, width: 1 },
          ticks: { show: false },
          space: 80,
          size: 30,
          values: (u, splits) => {
            const tick = timeTickFormatter(locale, (u.scales.x.max ?? 0) - (u.scales.x.min ?? 0));
            return splits.map((v) => tick(v));
          },
        },
        {
          stroke: theme.axis,
          font,
          grid: { stroke: theme.grid, width: 1 },
          ticks: { show: false },
          space: 32,
          size: 56,
          values: (_u, splits) => splits.map((v) => (latest.current ? latest.current.format(v) : String(v))),
        },
      ],
      series: [
        {},
        ...now.series.map((s, i) => ({
          label: s.label,
          stroke: colors[i],
          width: 2,
          dash: s.dashed ? [6, 4] : undefined,
          fill: area ? withAlpha(colors[i], 0.1) : undefined,
          spanGaps: false,
        })),
      ],
      hooks: {
        ready: [
          (u) => {
            u.over.appendChild(tip);
            u.over.addEventListener("pointerenter", () => {
              hovered = true;
            });
            u.over.addEventListener("pointerleave", () => {
              hovered = false;
              tip.classList.add("hidden");
            });
          },
        ],
        setCursor: [renderTooltip],
        draw: [drawOverlays],
      },
    };

    const u = new uPlot(opts, toPlotData(now.aligned), host);
    plotRef.current = u;
    const observer = new ResizeObserver((entries) => {
      const width = Math.floor(entries[0]?.contentRect.width ?? host.clientWidth);
      if (width > 0 && width !== u.width) u.setSize({ width, height });
    });
    observer.observe(host);
    return () => {
      observer.disconnect();
      u.destroy();
      plotRef.current = null;
      setSpots([]);
    };
  }, [structure, theme, height, syncKey, area, showPlot, locale]);

  const xFrom = xRange?.[0];
  const xTo = xRange?.[1];
  useEffect(() => {
    plotRef.current?.setData(toPlotData(aligned));
  }, [aligned, xFrom, xTo]);

  useEffect(() => {
    plotRef.current?.redraw(false, true);
  }, [markers, lines, shaded, format, unit]);

  const legend = series.length >= 2 && (
    <ul className="flex flex-wrap items-center gap-x-4 gap-y-1 text-caption text-muted-foreground" aria-label={t("operations.chart.legend")}>
      {series.map((s, i) => {
        const slot = seriesColorIndex(s, i);
        return (
          <li key={s.id} className="inline-flex items-center gap-1.5">
            <span
              aria-hidden
              className={cn("w-3 shrink-0", s.dashed ? "border-t-2 border-dashed" : "h-0.5 rounded-full")}
              style={s.dashed ? { borderColor: `var(--chart-${slot})` } : { backgroundColor: `var(--chart-${slot})` }}
            />
            {s.label}
          </li>
        );
      })}
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
        <EmptyState icon={ChartLine} title={emptyLabel ?? t("operations.chart.noData")} className="py-0" />
      </div>
    );
  } else if (view === "table") {
    body = (
      <ChartTable
        series={series}
        caption={ariaLabel}
        format={format}
        unit={unit}
        height={height}
        markers={markers}
        thresholds={lines}
        timeLabel={t("operations.chart.time")}
        deploysLabel={t("operations.chart.deploys")}
        thresholdsLabel={t("operations.chart.thresholds")}
        fullTime={fullTime}
      />
    );
  } else {
    body = (
      <div className={cn("relative", loading && "opacity-60")}>
        <div ref={plotHost} role="img" aria-label={ariaLabel} className="tt-timeseries w-full" style={{ height }} />
        {spots.map((spot) => (
          <MarkerAnchor key={`${spot.href}\u0000${spot.left}`} spot={spot} label={t("operations.chart.deployAt", { label: spot.label })} />
        ))}
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

function MarkerAnchor({ spot, label }: { spot: MarkerSpot; label: string }) {
  const inRouter = useInRouterContext();
  const className =
    "absolute h-4 w-4 -translate-x-1/2 -translate-y-1/2 rounded-sm outline-none focus-visible:ring-2 focus-visible:ring-ring";
  const style = { left: spot.left, top: spot.top };
  if (inRouter && spot.href.startsWith("/")) {
    return <Link to={spot.href} aria-label={label} title={label} className={className} style={style} />;
  }
  return <a href={spot.href} target="_blank" rel="noreferrer" aria-label={label} title={label} className={className} style={style} />;
}

interface ChartTableProps {
  series: ChartSeries[];
  caption: string;
  format: (v: number) => string;
  unit?: string;
  height: number;
  markers: DeployMarker[];
  thresholds: Threshold[];
  timeLabel: string;
  deploysLabel: string;
  thresholdsLabel: string;
  fullTime: (ms: number) => string;
}

function ChartTable({
  series,
  caption,
  format,
  unit,
  height,
  markers,
  thresholds,
  timeLabel,
  deploysLabel,
  thresholdsLabel,
  fullTime,
}: ChartTableProps) {
  const rows = useMemo(() => chartTableRows(series), [series]);
  return (
    <div className="space-y-3">
      <div className="overflow-auto rounded-lg border border-border" style={{ maxHeight: height }}>
        <table className="w-full text-caption">
          <caption className="sr-only">{caption}</caption>
          <thead className="sticky top-0 bg-card">
            <tr className="border-b border-border text-left text-muted-foreground">
              <th scope="col" className="px-3 py-2 font-medium">
                {timeLabel}
              </th>
              {series.map((s) => (
                <th key={s.id} scope="col" className="px-3 py-2 text-right font-medium">
                  {unit ? `${s.label} (${unit})` : s.label}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {rows.map((row) => (
              <tr key={row.at} className="border-b border-border/50 last:border-0">
                <th scope="row" className="whitespace-nowrap px-3 py-1.5 text-left font-normal text-muted-foreground">
                  {fullTime(row.at)}
                </th>
                {row.values.map((v, i) => (
                  <td key={series[i].id} className="px-3 py-1.5 text-right tabular-nums">
                    {v == null ? "—" : format(v)}
                  </td>
                ))}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      {markers.length > 0 && (
        <div className="text-caption">
          <p className="font-medium text-muted-foreground">{deploysLabel}</p>
          <ul className="mt-1 space-y-0.5">
            {markers.map((m) => (
              <li key={`${m.at}\u0000${m.label}`} className="flex gap-2">
                <span className="text-muted-foreground">{fullTime(m.at)}</span>
                <span className="font-mono">{m.label}</span>
              </li>
            ))}
          </ul>
        </div>
      )}
      {thresholds.length > 0 && (
        <div className="text-caption">
          <p className="font-medium text-muted-foreground">{thresholdsLabel}</p>
          <ul className="mt-1 space-y-0.5">
            {thresholds.map((th) => (
              <li key={`${th.value}\u0000${th.label ?? ""}`} className="flex gap-2">
                <span className="tabular-nums">{withUnit(format(th.value), unit)}</span>
                {th.label && <span className="text-muted-foreground">{th.label}</span>}
              </li>
            ))}
          </ul>
        </div>
      )}
    </div>
  );
}
