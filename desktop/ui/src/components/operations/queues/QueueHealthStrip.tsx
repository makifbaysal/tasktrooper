import type { ReactNode } from "react";
import type { ComponentSeries, SignalSeries } from "@/api";
import { hasTrend } from "@/components/operations/charts/chartData";
import { signalErrorView } from "@/components/operations/charts/signalChart";
import { formatSignalValue, signalDelta, signalLabel, signalValueParts, type SignalFormatContext } from "@/components/operations/console/signalFormat";
import { SignalTile } from "@/components/operations/console/SignalTile";
import {
  QUEUE_SIGNALS,
  backlogTrend,
  combineFor,
  queueSignalValue,
  type QueueSignalValue,
} from "@/components/operations/queues/queueSignals";
import { Button } from "@/components/ui/button";
import { useI18n } from "@/hooks/useI18n";

/** What a tile's actions open: the binding editor on a signal, empty or as it is. */
export interface QueueBindTarget {
  signal: string;
  unit?: string;
  current?: boolean;
}

interface QueueHealthStripProps {
  series: { data: ComponentSeries | null; loading: boolean; error?: string };
  compare: boolean;
  formatCtx: SignalFormatContext;
  onBind: (target: QueueBindTarget) => void;
  onRetry: () => void;
}

const DASH = <span className="font-mono text-muted-foreground">—</span>;

/** The Queues page's golden strip, from the series read: total backlog,
 * oldest message, consume against publish rate and dead letters. A source
 * that reads only the live value shows that value, with no trend. */
export function QueueHealthStrip({ series, compare, formatCtx, onBind, onRetry }: QueueHealthStripProps) {
  const { t } = useI18n();
  const q = (key: string, params?: Record<string, string | number>) => t(`operations.queues.${key}`, params);
  const signalOf = (id: string): SignalSeries | undefined => series.data?.signals?.find((s) => s.signal === id);
  const failed = Boolean(series.error) && !series.data;

  const action = (label: string, onClick: () => void, ariaLabel?: string) => (
    <Button variant="link" size="sm" className="h-auto p-0 text-micro" onClick={onClick} aria-label={ariaLabel}>
      {label}
    </Button>
  );
  const withAction = (text: ReactNode, button?: ReactNode) => (
    <span className="flex flex-wrap items-baseline gap-x-2">
      <span>{text}</span>
      {button}
    </span>
  );
  const bindButton = (signal: string, unit?: string) =>
    action(q("tileFoot.bind"), () => onBind({ signal, unit }), q("tileFoot.bindLabel", { signal: signalLabel(t, signal) }));

  // A tile that has no value to show: why, and what can be done about it.
  const missing = (signal: string, sig: SignalSeries | undefined): ReactNode => {
    if (series.loading && !series.data) return null;
    if (failed) return withAction(q("tileFoot.readFailed"), action(t("operations.chart.retry"), onRetry));
    if (!sig || !sig.bound) return withAction(t("operations.console.noMetricsSource"), bindButton(signal, sig?.unit));
    if (sig.error) {
      const view = signalErrorView(sig.error.code);
      const title = t(`operations.chart.errors.${view.key}`);
      if (view.action === "retry") return withAction(title, action(t("operations.chart.retry"), onRetry));
      if (view.action === "edit") {
        return withAction(
          title,
          action(t(`operations.chart.errors.${view.actionKey}`), () => onBind({ signal, unit: sig.unit, current: true })),
        );
      }
      return title;
    }
    return q("tileFoot.noData");
  };

  const valueOf = (signal: string): { sig?: SignalSeries; read: QueueSignalValue } => {
    const sig = signalOf(signal);
    return { sig, read: queueSignalValue(sig, combineFor(signal)) };
  };

  const tile = (signal: string, label: string, foot?: (read: QueueSignalValue, sig: SignalSeries) => ReactNode) => {
    const { sig, read } = valueOf(signal);
    if (!sig || read.value === undefined) {
      const waiting = series.loading && !series.data;
      return (
        <SignalTile
          key={signal}
          compact
          label={label}
          value={<span aria-busy={waiting || undefined}>{DASH}</span>}
          foot={missing(signal, sig)}
        />
      );
    }
    const parts = signalValueParts(read.value, sig.unit, formatCtx);
    const defaultFoot = read.live
      ? q("tileFoot.live")
      : read.previous !== undefined
        ? q("tileFoot.previous", { value: formatSignalValue(read.previous, sig.unit, formatCtx) })
        : undefined;
    return (
      <SignalTile
        key={signal}
        compact
        label={label}
        unit={parts.unit || undefined}
        value={<span className="font-mono tabular-nums">{parts.value}</span>}
        delta={compare && !read.live ? signalDelta(signal, read.value, read.previous, sig.unit, formatCtx) : undefined}
        state={signal === QUEUE_SIGNALS.deadLetter && read.value > 0 ? "warn" : undefined}
        foot={foot ? foot(read, sig) : defaultFoot}
        sparkline={hasTrend(read.points) ? read.points : undefined}
        sparklineColorIndex={1}
      />
    );
  };

  const ratesFoot = (consume: QueueSignalValue) => {
    const publish = valueOf(QUEUE_SIGNALS.publishRate);
    if (!publish.sig?.bound) {
      return withAction(q("tileFoot.publishUnbound"), bindButton(QUEUE_SIGNALS.publishRate, publish.sig?.unit));
    }
    if (publish.read.value === undefined) return consume.live ? q("tileFoot.live") : undefined;
    const trend = backlogTrend(consume.value, publish.read.value);
    const text = q("tileFoot.publish", { value: formatSignalValue(publish.read.value, publish.sig.unit, formatCtx) });
    return trend ? `${text} · ${q(`tileFoot.${trend}`)}` : text;
  };

  return (
    <div className="grid grid-cols-2 gap-2 @3xl:grid-cols-4">
      {tile(QUEUE_SIGNALS.backlog, q("tiles.backlog"))}
      {tile(QUEUE_SIGNALS.backlogAge, q("tiles.oldest"))}
      {tile(QUEUE_SIGNALS.consumeRate, q("tiles.rates"), (read) => ratesFoot(read))}
      {tile(QUEUE_SIGNALS.deadLetter, q("tiles.deadLetters"))}
    </div>
  );
}
