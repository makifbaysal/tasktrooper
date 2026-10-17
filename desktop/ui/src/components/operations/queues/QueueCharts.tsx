import { useMemo, type ReactNode } from "react";
import { Link } from "react-router-dom";
import type { ComponentSeries, SignalSeries } from "@/api";
import { LiveValueList, liveValueItems } from "@/components/operations/charts/LiveValueList";
import { signalChartSeries, signalErrorView } from "@/components/operations/charts/signalChart";
import type { ChartSeries, DeployMarker } from "@/components/operations/charts/TimeSeriesChart";
import { ChartSlot, DATA_SOURCES_PATH, type ChartSlotStatus } from "@/components/operations/console/ChartSlot";
import { signalFormatter, signalLabel, type SignalFormatContext } from "@/components/operations/console/signalFormat";
import { QUEUE_SIGNALS, combinePoints, isLiveSignal } from "@/components/operations/queues/queueSignals";
import type { QueueBindTarget } from "@/components/operations/queues/QueueHealthStrip";
import { useOpsHref } from "@/components/operations/shell/OpsContext";
import { Button } from "@/components/ui/button";
import { useI18n } from "@/hooks/useI18n";

interface QueueChartsProps {
  series: { data: ComponentSeries | null; loading: boolean; error?: string };
  compare: boolean;
  formatCtx: SignalFormatContext;
  deployMarkers: DeployMarker[];
  xRange: [number, number];
  syncKey: string;
  manageHref?: string;
  onBind: (target: QueueBindTarget) => void;
  onRetry: () => void;
}

interface SlotState {
  status?: ChartSlotStatus;
  error?: string;
  errorTitle?: string;
  errorAction?: ReactNode;
  retry?: () => void;
}

const RATE_SIGNALS = [QUEUE_SIGNALS.consumeRate, QUEUE_SIGNALS.publishRate];
const RATE_COLORS: Record<string, number> = { [QUEUE_SIGNALS.consumeRate]: 1, [QUEUE_SIGNALS.publishRate]: 2 };

interface Lines {
  backlog?: SignalSeries;
  backlogLines?: ChartSeries[];
  backlogNow: ChartSeries[];
  rates: Array<{ id: string; sig?: SignalSeries; label: string }>;
  rateLines: ChartSeries[];
}

/** The window's backlog, and what is consumed against what is published, with
 * the deploys marked. A source that reads only the live value (a direct
 * broker) has no history to draw: its values stand in for the chart. */
export function QueueCharts({ series, compare, formatCtx, deployMarkers, xRange, syncKey, manageHref, onBind, onRetry }: QueueChartsProps) {
  const { t } = useI18n();
  const q = (key: string, params?: Record<string, string | number>) => t(`operations.queues.${key}`, params);
  const href = useOpsHref();
  const failed = Boolean(series.error) && !series.data;
  const backlogTitle = t("operations.queues.chart.backlog");

  const lines = useMemo<Lines>(() => {
    const of = (id: string) => series.data?.signals?.find((s) => s.signal === id);
    const previous = (label: string) => t("operations.entity.summary.previousSeries", { label });
    const backlog = of(QUEUE_SIGNALS.backlog);
    const readable = backlog?.bound && !backlog.error;
    const rates = RATE_SIGNALS.map((id) => ({ id, sig: of(id), label: signalLabel(t, id) }));
    const rateLines: ChartSeries[] = [];
    for (const { id, sig, label } of rates) {
      if (!sig?.bound || sig.error) continue;
      rateLines.push({ id: `cur:${id}`, label, points: combinePoints(sig.series, "sum"), colorIndex: RATE_COLORS[id] });
      if (compare && (sig.previous ?? []).length > 0) {
        rateLines.push({
          id: `prev:${id}`,
          label: previous(label),
          points: combinePoints(sig.previous, "sum"),
          colorIndex: RATE_COLORS[id],
          dashed: true,
        });
      }
    }
    return {
      backlog,
      backlogLines: readable ? signalChartSeries(backlog, compare, { signal: backlogTitle, previous }) : undefined,
      backlogNow: readable ? signalChartSeries(backlog, false, { signal: backlogTitle, previous }) : [],
      rates,
      rateLines,
    };
  }, [series.data, compare, t, backlogTitle]);

  const backlogUnit = lines.backlog?.unit;
  const backlogFormat = useMemo(() => signalFormatter(backlogUnit, formatCtx), [backlogUnit, formatCtx]);
  const ratesUnit = lines.rates.find((r) => r.sig?.bound)?.sig?.unit ?? "msg/s";
  const ratesFormat = useMemo(() => signalFormatter(ratesUnit, formatCtx), [ratesUnit, formatCtx]);

  const errorState = (signal: string, sig: SignalSeries): SlotState => {
    const view = signalErrorView(sig.error?.code);
    return {
      status: "error",
      errorTitle: t(`operations.chart.errors.${view.key}`),
      error: view.body ? t(`operations.chart.errors.${view.key}Body`) : sig.error?.message || undefined,
      retry: view.action === "retry" ? onRetry : undefined,
      errorAction:
        view.action === "edit" ? (
          <Button size="sm" onClick={() => onBind({ signal, unit: sig.unit, current: true })}>
            {t(`operations.chart.errors.${view.actionKey}`)}
          </Button>
        ) : undefined,
    };
  };

  const backlog = lines.backlog;
  let backlogState: SlotState;
  if (series.loading) backlogState = {};
  else if (failed) backlogState = { status: "error", error: series.error, retry: onRetry };
  else if (backlog?.error) backlogState = errorState(QUEUE_SIGNALS.backlog, backlog);
  else if (isLiveSignal(backlog)) backlogState = { status: "live" };
  else backlogState = { status: backlog?.bound ? "bound" : "unbound", retry: onRetry };

  const readableRates = lines.rates.filter((r) => r.sig?.bound && !r.sig.error);
  const ratesNotes: string[] = [];
  let ratesState: SlotState;
  if (series.loading) ratesState = {};
  else if (failed) ratesState = { status: "error", error: series.error, retry: onRetry };
  else if (lines.rates.every((r) => !r.sig?.bound)) ratesState = { status: "unbound" };
  else if (readableRates.length === 0) {
    const first = lines.rates.find((r) => r.sig?.error);
    ratesState = first?.sig ? errorState(first.id, first.sig) : { status: "unbound" };
  } else {
    ratesState = { status: readableRates.every((r) => isLiveSignal(r.sig)) ? "live" : "bound", retry: onRetry };
    for (const r of lines.rates) {
      if (!r.sig?.bound) ratesNotes.push(q("chart.unboundPart", { signal: r.label }));
      else if (r.sig.error) {
        ratesNotes.push(q("chart.failedPart", { signal: r.label, reason: t(`operations.chart.errors.${signalErrorView(r.sig.error.code).key}`) }));
      }
    }
  }
  const unboundRate = lines.rates.find((r) => !r.sig?.bound);

  const liveNote = (
    <span>
      {q("chart.liveNote")}{" "}
      <Link to={href(DATA_SOURCES_PATH)} className="font-medium text-info hover:underline">
        {q("chart.liveLink")}
      </Link>
    </span>
  );
  const ratesNote =
    ratesState.status === "live" || ratesNotes.length > 0 ? (
      <span className="flex flex-wrap gap-x-1">
        {ratesNotes.map((n) => (
          <span key={n}>{n}</span>
        ))}
        {ratesState.status === "live" && liveNote}
      </span>
    ) : undefined;

  return (
    <div className="grid gap-3 @2xl:grid-cols-2">
      <ChartSlot
        title={backlogTitle}
        formatValue={backlog ? backlogFormat : undefined}
        series={lines.backlogLines}
        status={backlogState.status}
        error={backlogState.error}
        errorTitle={backlogState.errorTitle}
        errorAction={backlogState.errorAction}
        onRetry={backlogState.retry}
        onBind={() => onBind({ signal: QUEUE_SIGNALS.backlog, unit: backlog?.unit })}
        manageHref={manageHref}
        loading={series.loading}
        syncKey={syncKey}
        deployMarkers={deployMarkers}
        xRange={xRange}
        live={
          backlogState.status === "live" ? (
            <LiveValueList items={liveValueItems(lines.backlogNow)} formatValue={backlogFormat} ariaLabel={backlogTitle} height={150} />
          ) : undefined
        }
        note={backlogState.status === "live" ? liveNote : undefined}
      />
      <ChartSlot
        title={q("chart.rates")}
        formatValue={ratesFormat}
        series={lines.rateLines}
        status={ratesState.status}
        error={ratesState.error}
        errorTitle={ratesState.errorTitle}
        errorAction={ratesState.errorAction}
        onRetry={ratesState.retry}
        onBind={() => onBind({ signal: unboundRate?.id ?? QUEUE_SIGNALS.consumeRate, unit: unboundRate?.sig?.unit })}
        manageHref={manageHref}
        loading={series.loading}
        syncKey={syncKey}
        deployMarkers={deployMarkers}
        xRange={xRange}
        live={
          ratesState.status === "live" ? (
            <LiveValueList
              items={liveValueItems(lines.rateLines.filter((l) => !l.dashed))}
              formatValue={ratesFormat}
              ariaLabel={q("chart.rates")}
              height={150}
            />
          ) : undefined
        }
        note={ratesNote}
      />
    </div>
  );
}
