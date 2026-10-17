export type ChartPoint = [number, number | null];

export interface ChartSeriesLike {
  id: string;
  label: string;
  points: ChartPoint[];
  colorIndex?: number;
}

export const CHART_SLOTS = 8;

export function seriesColorIndex(series: Pick<ChartSeriesLike, "colorIndex">, position: number): number {
  const wanted = series.colorIndex ?? position + 1;
  if (!Number.isFinite(wanted)) return 1;
  return ((Math.round(wanted) - 1) % CHART_SLOTS + CHART_SLOTS) % CHART_SLOTS + 1;
}

export interface AlignedSeries {
  xs: number[];
  ys: (number | null | undefined)[][];
}

// A timestamp one series has and another lacks is `undefined` in the other
// (uPlot draws straight through it); only an explicit `null` breaks a line.
export function alignSeries(series: ChartSeriesLike[]): AlignedSeries {
  const stamps = new Set<number>();
  for (const s of series) for (const [x] of s.points) if (Number.isFinite(x)) stamps.add(x);
  const xs = [...stamps].sort((a, b) => a - b);
  const index = new Map(xs.map((x, i) => [x, i]));
  const ys = series.map((s) => {
    const row: (number | null | undefined)[] = new Array(xs.length).fill(undefined);
    for (const [x, y] of s.points) {
      const i = index.get(x);
      if (i === undefined) continue;
      row[i] = y === null || !Number.isFinite(y) ? null : y;
    }
    return row;
  });
  return { xs, ys };
}

export function hasChartData(series: ChartSeriesLike[]): boolean {
  return series.some((s) => s.points.some(([x, y]) => Number.isFinite(x) && y !== null && Number.isFinite(y)));
}

/** A live-only answer: some series, none with more than one point. A direct
 * broker source reads its broker as it is now, so a chart of it would be one
 * lone dot per series; it reads better as values. */
export function isInstantResult(series: ReadonlyArray<{ points: ChartPoint[] }>): boolean {
  return series.length > 0 && series.every((s) => (s.points ?? []).length <= 1);
}

/** Whether points are worth a line: at least two that hold a value. */
export function hasTrend(points: ReadonlyArray<ChartPoint> | undefined): boolean {
  let n = 0;
  for (const [x, y] of points ?? []) {
    if (Number.isFinite(x) && y !== null && Number.isFinite(y)) n += 1;
    if (n >= 2) return true;
  }
  return false;
}

export interface ChartTableRow {
  at: number;
  values: (number | null)[];
}

export function chartTableRows(series: ChartSeriesLike[]): ChartTableRow[] {
  const { xs, ys } = alignSeries(series);
  return xs.map((at, i) => ({ at, values: ys.map((row) => row[i] ?? null) }));
}

export function withAlpha(color: string, alpha: number): string {
  const c = color.trim();
  const hex = /^#([0-9a-f]{3}|[0-9a-f]{6})$/i.exec(c);
  if (hex) {
    const h = hex[1].length === 3 ? [...hex[1]].map((ch) => ch + ch).join("") : hex[1];
    const n = parseInt(h, 16);
    return `rgba(${(n >> 16) & 255}, ${(n >> 8) & 255}, ${n & 255}, ${alpha})`;
  }
  const fn = /^(rgba?|hsla?|oklch|oklab|lab|lch)\((.*)\)$/i.exec(c);
  if (fn) {
    const name = fn[1].toLowerCase().replace(/a$/, "");
    const body = fn[2].split("/")[0].trim();
    if (name === "rgb" || name === "hsl") {
      const parts = body.includes(",") ? body.split(",").map((p) => p.trim()).slice(0, 3) : body.split(/\s+/);
      return `${name}(${parts.join(" ")} / ${alpha})`;
    }
    return `${name}(${body} / ${alpha})`;
  }
  return c;
}

export function defaultValueFormatter(lang: string): (v: number) => string {
  const compact = new Intl.NumberFormat(lang, { notation: "compact", maximumFractionDigits: 1 });
  const plain = new Intl.NumberFormat(lang, { maximumFractionDigits: 2 });
  return (v) => (Math.abs(v) >= 10_000 ? compact.format(v) : plain.format(v));
}

const HOUR = 3_600_000;
const DAY = 24 * HOUR;

export function timeTickFormatter(lang: string, spanMs: number): (ms: number) => string {
  const opts: Intl.DateTimeFormatOptions =
    spanMs > 2 * DAY
      ? { month: "short", day: "numeric" }
      : spanMs > 12 * HOUR
        ? { weekday: "short", hour: "2-digit", minute: "2-digit" }
        : { hour: "2-digit", minute: "2-digit" };
  const fmt = new Intl.DateTimeFormat(lang, opts);
  return (ms) => fmt.format(new Date(ms));
}

export function fullTimeFormatter(lang: string): (ms: number) => string {
  const fmt = new Intl.DateTimeFormat(lang, {
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  });
  return (ms) => fmt.format(new Date(ms));
}

export function nearestMarker<T extends { at: number }>(markers: T[], at: number, toleranceMs: number): T | undefined {
  let best: T | undefined;
  let bestGap = Infinity;
  for (const m of markers) {
    const gap = Math.abs(m.at - at);
    if (gap <= toleranceMs && gap < bestGap) {
      best = m;
      bestGap = gap;
    }
  }
  return best;
}
