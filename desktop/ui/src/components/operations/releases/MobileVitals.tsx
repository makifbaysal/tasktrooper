import { useMemo, type ReactNode } from "react";
import { Link } from "react-router-dom";
import type { DeployEnvironment } from "@/api";
import { hasTrend } from "@/components/operations/charts/chartData";
import { headlinePoints, signalErrorView } from "@/components/operations/charts/signalChart";
import { DATA_SOURCES_PATH } from "@/components/operations/console/ChartSlot";
import { entityPath, signalSpec, type SignalSpec } from "@/components/operations/console/roleViews";
import { signalValueParts } from "@/components/operations/console/signalFormat";
import { SignalTile } from "@/components/operations/console/SignalTile";
import { MOBILE_VITAL_SIGNALS, boundSourceKinds, vitalsLagLikely, vitalsWindow } from "@/components/operations/releases/vitalsWindow";
import { useOpsHref } from "@/components/operations/shell/OpsContext";
import { lastValue } from "@/components/operations/web/webVitals";
import { Button } from "@/components/ui/button";
import { useI18n } from "@/hooks/useI18n";
import { useComponentSeries, useComponentSignals } from "@/hooks/useOpsSignals";
import { intlLocale } from "@/lib/languages";

export const MOBILE_VITALS: SignalSpec[] = MOBILE_VITAL_SIGNALS.map((id) => signalSpec(id)).filter((s): s is SignalSpec => Boolean(s));

interface MobileVitalsProps {
  /** The mobile component whose bindings feed the tiles; without one (a
   * repository's store apps no component carries) they wait for a source. */
  componentId?: string;
  env?: DeployEnvironment;
}

function Unbound({ spec }: { spec: SignalSpec }) {
  const { t } = useI18n();
  const href = useOpsHref();
  return (
    <SignalTile
      compact
      label={t(`operations.signals.${spec.label}`)}
      unit={spec.unit ? t(`operations.units.${spec.unit}`) : undefined}
      title={t("operations.console.noMetricsSource")}
      value={<span className="font-mono text-muted-foreground">—</span>}
      foot={
        <span className="flex flex-wrap items-baseline gap-x-2">
          <span>{t("operations.console.noMetricsSource")}</span>
          <Link to={href(DATA_SOURCES_PATH)} className="text-info hover:underline">
            {t("operations.console.connectSource")}
          </Link>
        </span>
      }
    />
  );
}

/** Crash and launch health of the app: crash-free users, crash rate, ANR
 * and cold start over the last 7 days, read through the component's
 * bindings (Google Play vitals, App Store Connect, Sentry). */
export function MobileVitals({ componentId, env = "production" }: MobileVitalsProps) {
  if (!componentId) {
    return (
      <div className="grid grid-cols-1 gap-2 @xl:grid-cols-2 @3xl:grid-cols-4">
        {MOBILE_VITALS.map((spec) => (
          <Unbound key={spec.id} spec={spec} />
        ))}
      </div>
    );
  }
  return <BoundVitals componentId={componentId} env={env} />;
}

function BoundVitals({ componentId, env }: { componentId: string; env: DeployEnvironment }) {
  const { t, lang } = useI18n();
  const m = (key: string, params?: Record<string, string | number>) => t(`operations.telemetry.mobileVitals.${key}`, params);
  const href = useOpsHref();
  const win = vitalsWindow();
  const ids = useMemo(() => MOBILE_VITALS.map((s) => s.id), []);
  const series = useComponentSeries(componentId, env, ids, win);
  const bindings = useComponentSignals(componentId, env);
  const sourceKinds = useMemo(() => boundSourceKinds(bindings.data), [bindings.data]);
  const formatCtx = useMemo(() => ({ locale: intlLocale(lang), t }), [lang, t]);
  const day = useMemo(() => new Intl.DateTimeFormat(intlLocale(lang), { month: "short", day: "numeric" }), [lang]);
  const signalsHref = href(entityPath(componentId, "signals"));
  const failed = Boolean(series.error) && !series.data;
  let lagged = false;

  const tile = (spec: SignalSpec) => {
    const label = t(`operations.signals.${spec.label}`);
    const sig = series.data?.signals?.find((s) => s.signal === spec.id);
    const points = headlinePoints(sig?.series);
    const last = lastValue(points);
    if (sig && !sig.bound) {
      return (
        <SignalTile
          key={spec.id}
          compact
          label={label}
          title={t("operations.console.noMetricsSource")}
          value={<span className="font-mono text-muted-foreground">—</span>}
          foot={
            <Link to={signalsHref} className="text-info hover:underline">
              {m("bind")}
            </Link>
          }
        />
      );
    }
    if (!last) {
      let foot: ReactNode = null;
      if (series.loading) foot = null;
      else if (failed) {
        foot = (
          <span className="flex flex-wrap items-baseline gap-x-2">
            <span>{m("readFailed")}</span>
            <Button variant="link" size="sm" className="h-auto p-0 text-micro" onClick={() => void series.reload()}>
              {t("operations.chart.retry")}
            </Button>
          </span>
        );
      } else if (sig?.error) {
        const view = signalErrorView(sig.error.code);
        foot = (
          <span className="flex flex-wrap items-baseline gap-x-2">
            <span title={sig.error.message || undefined}>{t(`operations.chart.errors.${view.key}`)}</span>
            {view.action === "retry" ? (
              <Button variant="link" size="sm" className="h-auto p-0 text-micro" onClick={() => void series.reload()}>
                {t("operations.chart.retry")}
              </Button>
            ) : view.action === "edit" ? (
              <Link to={signalsHref} className="text-info hover:underline">
                {t(`operations.chart.errors.${view.actionKey}`)}
              </Link>
            ) : null}
          </span>
        );
      } else if (vitalsLagLikely(spec.id, win, sourceKinds.get(spec.id))) {
        lagged = true;
        foot = m("notYet");
      } else foot = m("noData");
      return (
        <SignalTile
          key={spec.id}
          compact
          label={label}
          value={
            <span className="font-mono text-muted-foreground" aria-busy={series.loading || undefined}>
              —
            </span>
          }
          foot={foot}
        />
      );
    }
    const parts = signalValueParts(last.value, sig?.unit, formatCtx);
    return (
      <SignalTile
        key={spec.id}
        compact
        label={label}
        unit={parts.unit || undefined}
        value={<span className="font-mono tabular-nums">{parts.value}</span>}
        sparkline={hasTrend(points) ? points : undefined}
        sparklineColorIndex={1}
        foot={m("asOf", { date: day.format(last.at) })}
      />
    );
  };

  const tiles = MOBILE_VITALS.map(tile);
  return (
    <div className="space-y-2">
      <div className="grid grid-cols-1 gap-2 @xl:grid-cols-2 @3xl:grid-cols-4">{tiles}</div>
      <p className="text-micro text-muted-foreground">
        {m("window", { env: t(`cloud.environments.${env}`) })}
        {lagged && <span className="ml-1">{m("playLag")}</span>}
      </p>
    </div>
  );
}
