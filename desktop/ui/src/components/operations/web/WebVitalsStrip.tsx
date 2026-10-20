import { useEffect, useMemo, useRef, type ReactNode } from "react";
import type { CloudAnalytics, DeployEnvironment, SignalSeries } from "@/api";
import { hasTrend } from "@/components/operations/charts/chartData";
import { signalErrorView } from "@/components/operations/charts/signalChart";
import { formatSignalValue, signalLabel, signalValueParts, type SignalFormatContext } from "@/components/operations/console/signalFormat";
import { analyticsOff } from "@/components/operations/observability/observabilityReadiness";
import { SignalTile } from "@/components/operations/console/SignalTile";
import type { OpsTimeRange } from "@/components/operations/shell/OpsContext";
import {
  CWV_SIGNALS,
  CWV_THRESHOLDS,
  TRAFFIC_SIGNALS,
  bandPosition,
  bandSegments,
  cwvBand,
  vitalRead,
  type CwvBand,
  type CwvSignal,
} from "@/components/operations/web/webVitals";
import { Button } from "@/components/ui/button";
import { useI18n } from "@/hooks/useI18n";
import { useComponentSeries } from "@/hooks/useOpsSignals";
import { cn } from "@/lib/utils";

const SIGNALS = [...CWV_SIGNALS, ...TRAFFIC_SIGNALS];

const BAND_TEXT: Record<CwvBand, string> = {
  good: "text-success",
  needsImprovement: "text-warning",
  poor: "text-destructive",
};

interface WebVitalsStripProps {
  componentId: string;
  env: DeployEnvironment;
  range: OpsTimeRange;
  formatCtx: SignalFormatContext;
  onBind: (signal: string, unit?: string) => void;
  onEdit: (signal: string, unit?: string) => void;
  /** Whether Vercel is measuring the project; a signal it is not measuring is
   * said so instead of asking for a data source. */
  analytics?: CloudAnalytics | null;
  /** Bumped when a binding was saved elsewhere on the page: read again. */
  reloadToken?: number;
}

function BandMeter({ signal, value, label }: { signal: CwvSignal; value: number; label: string }) {
  const segments = bandSegments(signal);
  return (
    <span className="relative mt-1 block h-1.5 w-full" role="img" aria-label={label}>
      <span className="flex h-full w-full overflow-hidden rounded-full">
        <span className="h-full bg-success/70" style={{ width: `${segments.good * 100}%` }} />
        <span className="h-full bg-warning/70" style={{ width: `${segments.needsImprovement * 100}%` }} />
        <span className="h-full bg-destructive/70" style={{ width: `${segments.poor * 100}%` }} />
      </span>
      <span
        className="absolute -top-0.5 h-2.5 w-0.5 -translate-x-1/2 rounded-full bg-foreground"
        style={{ left: `${bandPosition(signal, value) * 100}%` }}
      />
    </span>
  );
}

/** A frontend's Core Web Vitals at p75 — LCP, INP and CLS, each against
 * Google's good / needs improvement / poor bands — and its traffic. */
export function WebVitalsStrip({ componentId, env, range, formatCtx, onBind, onEdit, analytics, reloadToken = 0 }: WebVitalsStripProps) {
  const { t } = useI18n();
  const v = (key: string, params?: Record<string, string | number>) => t(`operations.telemetry.webVitals.${key}`, params);
  const series = useComponentSeries(componentId, env, SIGNALS, range);
  const reload = useRef(series.reload);
  reload.current = series.reload;
  useEffect(() => {
    if (reloadToken > 0) void reload.current();
  }, [reloadToken]);
  const bySignal = useMemo(() => new Map((series.data?.signals ?? []).map((s) => [s.signal, s])), [series.data]);
  const failed = Boolean(series.error) && !series.data;

  const threshold = (signal: CwvSignal, which: "good" | "poor") =>
    formatSignalValue(CWV_THRESHOLDS[signal][which], signal === "web.cls" ? "score" : "ms", formatCtx);

  const blank = (signal: string, sig: SignalSeries | undefined, unitLabel?: string): ReactNode => {
    const label = signalLabel(t, signal, true);
    let foot: ReactNode = null;
    if (series.loading) foot = null;
    else if (failed) {
      foot = (
        <span className="flex flex-wrap items-baseline gap-x-2">
          <span>{v("readFailed")}</span>
          <Button variant="link" size="sm" className="h-auto p-0 text-micro" onClick={() => void series.reload()}>
            {t("operations.chart.retry")}
          </Button>
        </span>
      );
    } else if (sig && !sig.bound && analyticsOff(analytics, signal)) {
      foot = <span title={t("operations.observability.codes.analytics_not_enabled")}>{t("operations.chart.errors.notEnabled")}</span>;
    } else if (sig && !sig.bound) {
      foot = (
        <Button variant="link" size="sm" className="h-auto p-0 text-micro" onClick={() => onBind(signal, sig.unit)}>
          {v("bind")}
        </Button>
      );
    } else if (sig?.error) {
      const view = signalErrorView(sig.error.code);
      foot = (
        <span className="flex flex-wrap items-baseline gap-x-2">
          <span>{t(`operations.chart.errors.${view.key}`)}</span>
          {view.action === "edit" ? (
            <Button variant="link" size="sm" className="h-auto p-0 text-micro" onClick={() => onEdit(signal, sig.unit)}>
              {t(`operations.chart.errors.${view.actionKey}`)}
            </Button>
          ) : view.action === "retry" ? (
            <Button variant="link" size="sm" className="h-auto p-0 text-micro" onClick={() => void series.reload()}>
              {t("operations.chart.retry")}
            </Button>
          ) : null}
        </span>
      );
    } else foot = v("noData");
    return (
      <SignalTile
        key={signal}
        compact
        label={label}
        unit={unitLabel}
        value={
          <span className="font-mono text-muted-foreground" aria-busy={series.loading || undefined}>
            —
          </span>
        }
        foot={foot}
      />
    );
  };

  const vitalTile = (signal: CwvSignal) => {
    const sig = bySignal.get(signal);
    const read = vitalRead(sig);
    if (!read.last) return blank(signal, sig);
    const band = cwvBand(signal, read.last.value)!;
    const parts =
      signal === "web.cls"
        ? { value: new Intl.NumberFormat(formatCtx.locale, { maximumFractionDigits: 3 }).format(read.last.value), unit: "" }
        : signalValueParts(read.last.value, sig?.unit || "ms", formatCtx);
    const bandWord = v(`bands.${band}`);
    const quantile = v("quantileP75");
    return (
      <SignalTile
        key={signal}
        compact
        label={signalLabel(t, signal, true)}
        title={signalLabel(t, signal)}
        unit={parts.unit || undefined}
        value={<span className="font-mono tabular-nums">{parts.value}</span>}
        state={band === "poor" ? "alarm" : band === "needsImprovement" ? "warn" : undefined}
        sparkline={hasTrend(read.points) ? read.points : undefined}
        sparklineColorIndex={1}
        foot={
          <span className="block space-y-0.5">
            <span className="flex flex-wrap items-baseline justify-between gap-x-2">
              <span className={cn("font-medium", BAND_TEXT[band])}>{bandWord}</span>
              <span>{quantile}</span>
            </span>
            <BandMeter
              signal={signal}
              value={read.last.value}
              label={v("meterLabel", { band: bandWord, good: threshold(signal, "good"), poor: threshold(signal, "poor") })}
            />
            <span className="block">{v("thresholds", { good: threshold(signal, "good"), poor: threshold(signal, "poor") })}</span>
          </span>
        }
      />
    );
  };

  const trafficTile = (signal: string) => {
    const sig = bySignal.get(signal);
    const read = vitalRead(sig);
    if (!read.last) return blank(signal, sig);
    const parts = signalValueParts(read.last.value, sig?.unit, formatCtx);
    return (
      <SignalTile
        key={signal}
        compact
        label={signalLabel(t, signal)}
        unit={parts.unit || undefined}
        value={<span className="font-mono tabular-nums">{parts.value}</span>}
        sparkline={hasTrend(read.points) ? read.points : undefined}
        sparklineColorIndex={2}
        foot={hasTrend(read.points) ? v("latest") : v("live")}
      />
    );
  };

  return (
    <div className="space-y-4">
      <section aria-labelledby={`cwv-${componentId}`} className="min-w-0 space-y-2">
        <div className="flex flex-wrap items-baseline justify-between gap-2">
          <h2 id={`cwv-${componentId}`} className="text-micro font-medium tracking-wide text-muted-foreground uppercase">
            {v("title")}
          </h2>
          <p className="text-micro text-muted-foreground">{v("hint")}</p>
        </div>
        <div className="grid grid-cols-1 gap-2 @xl:grid-cols-3">{CWV_SIGNALS.map(vitalTile)}</div>
      </section>
      <section aria-labelledby={`traffic-${componentId}`} className="min-w-0 space-y-2">
        <h2 id={`traffic-${componentId}`} className="text-micro font-medium tracking-wide text-muted-foreground uppercase">
          {v("traffic")}
        </h2>
        <div className="grid grid-cols-1 gap-2 @xl:grid-cols-3">{TRAFFIC_SIGNALS.map(trafficTile)}</div>
      </section>
    </div>
  );
}
