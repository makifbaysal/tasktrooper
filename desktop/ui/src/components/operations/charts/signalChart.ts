import type { MetricPoint, MetricSeries, SignalSeries } from "@/api";
import { CHART_SLOTS } from "@/components/operations/charts/chartData";
import { histogramStep } from "@/components/operations/charts/histogramData";
import type { ChartSeries } from "@/components/operations/charts/TimeSeriesChart";
import { seriesLabel } from "@/components/operations/sources/dataSourceSchema";

export const QUANTILES = ["p50", "p75", "p95", "p99"] as const;

// The palette in quantile order, so the lines read from the median up.
const QUANTILE_COLOR: Record<string, number> = { p50: 1, p75: 2, p95: 3, p99: 4 };

// The quantile a latency chart leads with: its tile, sparkline and table row.
export const HEADLINE_QUANTILE = "p95";

const STEP_TARGET_POINTS = 240;
const MIN_STEP_SECONDS = 15;

/** A round step that gives a window about 240 points, never under 15 s. */
export function autoStepSeconds(spanMs: number): number {
  return Math.max(MIN_STEP_SECONDS, Math.round(histogramStep(spanMs, STEP_TARGET_POINTS) / 1000));
}

const QUANTILE_ORDER: readonly string[] = QUANTILES;

// The server names a per-quantile read "p95", or "p95 <name>" when the
// recipe's aggregation gave several series.
function quantileOf(name: string): string | undefined {
  const head = (name ?? "").split(" ")[0];
  return QUANTILE_ORDER.includes(head) ? head : undefined;
}

function ordered(series: MetricSeries[]): MetricSeries[] {
  if (!series.every((s) => quantileOf(s.name))) return series;
  const rank = (s: MetricSeries) => QUANTILE_ORDER.indexOf(quantileOf(s.name) ?? "");
  return series.map((s, i) => ({ s, i })).sort((a, b) => rank(a.s) - rank(b.s) || a.i - b.i).map(({ s }) => s);
}

function nameOf(series: MetricSeries, index: number, total: number, signalName: string): string {
  if (quantileOf(series.name)) return series.name;
  if (total === 1) return signalName;
  return seriesLabel(series) || `${signalName} ${index + 1}`;
}

function colorOf(series: MetricSeries, index: number): number {
  return QUANTILE_COLOR[series.name] ?? (index % CHART_SLOTS) + 1;
}

export interface SignalChartLabels {
  /** The signal's own name, for a series that has none worth showing. */
  signal: string;
  /** "{label} · previous period" in the reader's language. */
  previous: (label: string) => string;
}

/** One signal's answer as chart lines: one per series (a latency signal's
 * p50/p75/p95/p99 in that order, each in its own colour), and with compare on
 * the previous period's lines dashed in the colour of the line they match. */
export function signalChartSeries(signal: SignalSeries, compare: boolean, labels: SignalChartLabels): ChartSeries[] {
  const current = ordered(signal.series ?? []);
  const lines: ChartSeries[] = current.map((s, i) => ({
    id: `cur:${i}:${s.name}`,
    label: nameOf(s, i, current.length, labels.signal),
    points: s.points,
    colorIndex: colorOf(s, i),
  }));
  if (!compare) return lines;
  const previous = ordered(signal.previous ?? []);
  previous.forEach((s, i) => {
    const match = current.findIndex((c) => c.name === s.name && seriesLabel(c) === seriesLabel(s));
    const at = match >= 0 ? match : i;
    const base = lines[at];
    const label = base?.label ?? nameOf(s, i, previous.length, labels.signal);
    lines.push({
      id: `prev:${i}:${s.name}`,
      label: labels.previous(label),
      points: s.points,
      colorIndex: base?.colorIndex ?? colorOf(s, i),
      dashed: true,
    });
  });
  return lines;
}

/** The series a sparkline or a single value stands for: the headline
 * quantile when there is one, else the first. */
export function headlineSeries(series: MetricSeries[] | undefined): MetricSeries | undefined {
  if (!series || series.length === 0) return undefined;
  return series.find((s) => quantileOf(s.name) === HEADLINE_QUANTILE) ?? series[0];
}

export function headlinePoints(series: MetricSeries[] | undefined): MetricPoint[] {
  return headlineSeries(series)?.points ?? [];
}

/** What a signal's read error offers: try again, open the binding editor on
 * the binding as it is (a param to fill in, a source to replace), or nothing
 * (retrying cannot help). */
export type SignalErrorAction = "retry" | "edit" | "none";

export interface SignalErrorView {
  /** Under `operations.chart.errors`; `failed` is the generic one. */
  key: string;
  /** A sentence of its own under `${key}Body`, in place of the server's. */
  body: boolean;
  action: SignalErrorAction;
  /** The edit action's label, under `operations.chart.errors`. */
  actionKey?: string;
}

const SIGNAL_ERRORS: Record<string, SignalErrorView> = {
  needs_params: { key: "needsParams", body: true, action: "edit", actionKey: "fillParams" },
  unsupported: { key: "unsupported", body: true, action: "none" },
  unknown_signal: { key: "unknownSignal", body: true, action: "none" },
  timeout: { key: "timeout", body: true, action: "retry" },
  not_found: { key: "notFound", body: true, action: "edit", actionKey: "rebind" },
  not_enabled: { key: "notEnabled", body: false, action: "none" },
  bad_query: { key: "badQuery", body: false, action: "retry" },
  upstream: { key: "upstream", body: false, action: "retry" },
};

const SIGNAL_ERROR_FALLBACK: SignalErrorView = { key: "failed", body: false, action: "retry" };

/** How the console shows a series error code (the series route's `error.code`). */
export function signalErrorView(code: string | undefined): SignalErrorView {
  return (code && SIGNAL_ERRORS[code]) || SIGNAL_ERROR_FALLBACK;
}
