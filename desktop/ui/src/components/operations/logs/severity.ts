import { LOG_SEVERITIES, type LogSeverity } from "@/api";
import type { HistogramSeries } from "@/components/operations/charts/VolumeHistogram";

/** Bottom to top in the volume histogram: the worst sits on the baseline,
 * where it reads first. */
export const SEVERITY_STACK: LogSeverity[] = ["critical", "error", "warning", "info", "debug"];

// Semantic tokens, not chart slots: a red bar must mean the same as a red
// pill, in both themes.
export const SEVERITY_BAR: Record<LogSeverity, string> = {
  critical: "bg-destructive",
  error: "bg-destructive/55",
  warning: "bg-warning",
  info: "bg-chart-1",
  debug: "bg-muted-foreground/40",
};

export function isLogSeverity(value: string | null | undefined): value is LogSeverity {
  return LOG_SEVERITIES.some((s) => s === value);
}

export function severitySeries(label: (s: LogSeverity) => string): HistogramSeries[] {
  return SEVERITY_STACK.map((s) => ({ id: s, label: label(s), swatchClassName: SEVERITY_BAR[s] }));
}

export function severityIndex(severity: string): number {
  const i = SEVERITY_STACK.indexOf(severity as LogSeverity);
  return i >= 0 ? i : SEVERITY_STACK.indexOf("info");
}
