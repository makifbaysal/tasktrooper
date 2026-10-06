import type { UsageByDay, UsageByModel, UsageKind, UsageTotals } from "@/api";
import { intlLocale } from "@/lib/languages";

export const EMPTY_TOTALS: UsageTotals = {
  calls: 0,
  prompt_tokens: 0,
  completion_tokens: 0,
  cache_read_tokens: 0,
  cache_write_tokens: 0,
};

export interface TokenBreakdown {
  fresh: number;
  cacheWrite: number;
  cacheRead: number;
  output: number;
  total: number;
}

// prompt_tokens already contains both cache shares, so fresh input is what is
// left after taking them out — adding them on top would count them twice.
export function breakdown(t: UsageTotals): TokenBreakdown {
  const cacheRead = t.cache_read_tokens;
  const cacheWrite = t.cache_write_tokens;
  return {
    fresh: Math.max(0, t.prompt_tokens - cacheRead - cacheWrite),
    cacheWrite,
    cacheRead,
    output: t.completion_tokens,
    total: t.prompt_tokens + t.completion_tokens,
  };
}

export function cacheHitRate(t: UsageTotals): number | null {
  return t.prompt_tokens > 0 ? t.cache_read_tokens / t.prompt_tokens : null;
}

export function sumTotals(rows: UsageTotals[]): UsageTotals {
  return rows.reduce<UsageTotals>(
    (acc, r) => ({
      calls: acc.calls + r.calls,
      prompt_tokens: acc.prompt_tokens + r.prompt_tokens,
      completion_tokens: acc.completion_tokens + r.completion_tokens,
      cache_read_tokens: acc.cache_read_tokens + r.cache_read_tokens,
      cache_write_tokens: acc.cache_write_tokens + r.cache_write_tokens,
    }),
    EMPTY_TOTALS,
  );
}

export function rowsOfKind(rows: UsageByModel[], ...kinds: UsageKind[]): UsageByModel[] {
  return rows.filter((r) => kinds.includes(r.kind));
}

const DAY_MS = 86_400_000;

function parseDay(day: string): number {
  const [y, m, d] = day.split("-").map(Number);
  return Date.UTC(y, m - 1, d);
}

// The server only returns days that have rows; the chart needs every calendar
// day of the window so a quiet day reads as zero instead of disappearing.
export function fillDays(from: string, to: string, daily: UsageByDay[]): UsageByDay[] {
  if (!from || !to) return daily;
  const byDay = new Map(daily.map((d) => [d.day, d]));
  const out: UsageByDay[] = [];
  for (let cur = parseDay(from), end = parseDay(to); cur <= end; cur += DAY_MS) {
    const day = new Date(cur).toISOString().slice(0, 10);
    out.push(byDay.get(day) ?? { day, ...EMPTY_TOTALS });
  }
  return out;
}

// K/M/B stay in English on purpose: Turkish compact notation writes thousands
// as "B" (bin), which reads as billions next to every other token counter.
const compactFormat = new Intl.NumberFormat("en-US", { notation: "compact", maximumFractionDigits: 1 });

export function formatCompact(n: number): string {
  return compactFormat.format(n);
}

export function formatInteger(n: number, lang: string): string {
  return new Intl.NumberFormat(intlLocale(lang)).format(n);
}

export function formatPercent(ratio: number, lang: string): string {
  const format = (r: number, digits: number) =>
    new Intl.NumberFormat(intlLocale(lang), {
      style: "percent",
      maximumFractionDigits: digits,
      minimumFractionDigits: 0,
    }).format(r);
  if (ratio > 0 && ratio < 0.001) return `<${format(0.001, 1)}`;
  return format(ratio, ratio > 0 && ratio < 0.1 ? 1 : 0);
}

// Days are calendar dates in the server's bucketing timezone, so they are
// formatted as UTC dates — the viewer's own offset must not shift them.
export function formatDay(day: string, lang: string, opts: Intl.DateTimeFormatOptions): string {
  return new Intl.DateTimeFormat(intlLocale(lang), { ...opts, timeZone: "UTC" }).format(parseDay(day));
}

// Every step halves to a clean number too, since the axis labels max/2.
export function niceCeil(v: number): number {
  if (v <= 0) return 1;
  const exp = 10 ** Math.floor(Math.log10(v));
  const step = [1, 2, 3, 4, 5, 6, 8, 10].find((s) => s * exp >= v) ?? 10;
  return step * exp;
}

export function browserTimeZone(): string {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone ?? "";
  } catch {
    return "";
  }
}
