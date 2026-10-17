import { Plug } from "lucide-react";
import { useMemo, useState, type ReactNode } from "react";
import { Link } from "react-router-dom";
import type { Release, SignalSeries, SignalSeriesError } from "@/api";
import { hasTrend } from "@/components/operations/charts/chartData";
import { LiveValueList, liveValueItems } from "@/components/operations/charts/LiveValueList";
import { signalChartSeries, signalErrorView } from "@/components/operations/charts/signalChart";
import { isLiveSignal } from "@/components/operations/queues/queueSignals";
import { boundSourceKinds, vitalsLagLikely } from "@/components/operations/releases/vitalsWindow";
import type { DeployMarker } from "@/components/operations/charts/TimeSeriesChart";
import { ChartSlot, DATA_SOURCES_PATH, type ChartSlotStatus } from "@/components/operations/console/ChartSlot";
import { goldenValue, goldenWindow, indexGolden } from "@/components/operations/console/goldenMetrics";
import { entityPath, releaseLabel, releaseTime, repositoryDeployPath } from "@/components/operations/console/opsEntities";
import { isSevereIncident } from "@/components/operations/console/opsStatus";
import { BUILTIN_SIGNALS, BUILTIN_TILES, hasTab, viewFor, type SignalSpec } from "@/components/operations/console/roleViews";
import { signalDelta, signalFormatter, signalValueParts } from "@/components/operations/console/signalFormat";
import { SignalTile, type SignalDelta } from "@/components/operations/console/SignalTile";
import { SignalBindingEditor } from "@/components/operations/signals/SignalBindingEditor";
import { DeployTimeline } from "@/components/operations/entity/DeployTimeline";
import { EntityActivity, buildActivity } from "@/components/operations/entity/EntityActivity";
import { ObservabilityReadinessCard } from "@/components/operations/observability/ObservabilityReadinessCard";
import { inRange, previousPeriod, useOpsContext, useOpsHref, useOpsPageScope } from "@/components/operations/shell/OpsContext";
import { WebVitalsStrip } from "@/components/operations/web/WebVitalsStrip";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Notice } from "@/components/ui/notice";
import { useI18n } from "@/hooks/useI18n";
import { useEntityHealth, useEntityIncidents, useEntityReleases } from "@/hooks/useOpsEntityData";
import { useComponentSeries, useComponentSignals, useOpsGolden } from "@/hooks/useOpsSignals";
import { intlLocale } from "@/lib/languages";
import { formatDate, formatRelativeTime } from "@/lib/utils";
import type { EntitySubPageProps } from "@/pages/ops/entity/types";

function at(release: Release): number {
  return Date.parse(releaseTime(release));
}

interface BindTarget {
  signal: string;
  unit?: string;
  /** Open on the signal's binding as it is (a recipe missing a param). */
  current?: boolean;
}

/** An entity's Summary: golden metrics, the role's golden-signal charts with
 * the window's deploys marked, and what happened in the window beside them. */
export default function EntitySummary({ entity, env, environment }: EntitySubPageProps) {
  const { t, lang } = useI18n();
  const s = (key: string, params?: Record<string, string | number>) => t(`operations.entity.summary.${key}`, params);
  const { range, compare } = useOpsContext();
  const href = useOpsHref();
  useOpsPageScope({ time: true, compare: true, env: true });

  const view = viewFor(entity.role);
  const envLabel = t(`cloud.environments.${env}`);
  const locale = intlLocale(lang);
  const formatCtx = useMemo(() => ({ locale, t }), [locale, t]);
  const [binding, setBinding] = useState<BindTarget | null>(null);
  const [savedCount, setSavedCount] = useState(0);
  const chartIds = useMemo(() => view.charts.map((c) => c.id), [view]);
  const series = useComponentSeries(entity.id, env, chartIds, range, { compare });
  const golden = useOpsGolden(env, range, [entity.id], view.golden.length > 0);
  const goldenIndex = useMemo(() => indexGolden(golden.data), [golden.data]);
  // Only an app's vitals can be late (Google Play's): which source each is
  // bound to says whether an empty tile is "not published yet".
  const appVitals = entity.role === "mobile" || entity.role === "desktop";
  const bindings = useComponentSignals(entity.id, env, appVitals);
  const sourceKinds = useMemo(() => boundSourceKinds(bindings.data), [bindings.data]);
  const signalsHref = hasTab(entity.role, "signals") ? href(entityPath(entity.id, "signals")) : undefined;
  const formatterFor = useMemo(() => {
    const made = new Map<string, (v: number) => string>();
    return (unit: string) => {
      let format = made.get(unit);
      if (!format) {
        format = signalFormatter(unit, formatCtx);
        made.set(unit, format);
      }
      return format;
    };
  }, [formatCtx]);
  const chartLines = useMemo(
    () =>
      new Map(
        view.charts.map((spec) => {
          const sig = series.data?.signals?.find((x) => x.signal === spec.id);
          const lines =
            sig?.bound && !sig.error
              ? signalChartSeries(sig, compare, {
                  signal: t(`operations.signals.${spec.label}`),
                  previous: (label) => t("operations.entity.summary.previousSeries", { label }),
                })
              : undefined;
          return [spec.id, lines] as const;
        }),
      ),
    [view, series.data, compare, t],
  );
  const { releases, loading: releasesLoading } = useEntityReleases(entity, env);
  const { incidents, loading: incidentsLoading } = useEntityIncidents(entity, env);
  const { openIncidents } = useEntityHealth(entity, env, environment);

  const changesPath = entityPath(entity.id, "changes");
  const issuesSubPath = entityPath(entity.id, "issues");
  const inWindow = useMemo(() => releases.filter((r) => inRange(at(r), range)), [releases, range]);
  const previousCount = useMemo(() => {
    const prev = previousPeriod(range);
    return releases.filter((r) => inRange(at(r), prev)).length;
  }, [releases, range]);
  const markers = useMemo<DeployMarker[]>(
    () => inWindow.map((r) => ({ at: at(r), label: releaseLabel(r), href: href(`${changesPath}?release=${encodeURIComponent(r.id)}`) })),
    [inWindow, href, changesPath],
  );
  const activity = useMemo(() => buildActivity(releases, incidents, range.from, range.to), [releases, incidents, range]);
  const xRange = useMemo<[number, number]>(() => [range.from, range.to], [range]);
  const syncKey = `ops:${entity.id}`;
  const lastRelease = releases[0];

  const seriesFailed = Boolean(series.error) && !series.data;
  const goldenFailed = Boolean(golden.error) && !golden.data;
  const goldenClamped = view.golden.length > 0 && goldenWindow(range).clamped;
  const signalOf = (id: string): SignalSeries | undefined => series.data?.signals?.find((x) => x.signal === id);
  const retryReads = () => {
    void series.reload();
    void golden.reload();
  };
  const editCurrent = (spec: SignalSpec, sig: SignalSeries | undefined) =>
    setBinding({ signal: spec.id, unit: sig?.unit, current: true });
  const errorTitle = (error: SignalSeriesError) => t(`operations.chart.errors.${signalErrorView(error.code).key}`);

  const readFailedFoot = (text: string, action?: ReactNode) => (
    <span className="flex flex-wrap items-baseline gap-x-2">
      <span>{text}</span>
      {action}
    </span>
  );
  const retryButton = (
    <Button variant="link" size="sm" className="h-auto p-0 text-micro" onClick={retryReads}>
      {t("operations.chart.retry")}
    </Button>
  );

  const goldenTile = (spec: SignalSpec) => {
    const label = t(`operations.signals.${spec.label}`);
    const found = goldenValue(goldenIndex, entity.id, spec.id);
    // Bound or not: the series answer says, else the golden answer's flag,
    // else a golden answer without this component (it lists every component
    // with a confirmed binding). A read that failed says nothing either way.
    const sig = signalOf(spec.id);
    const bound = sig ? sig.bound : found?.bound !== undefined ? found.bound : golden.data && !found ? false : undefined;
    if (bound === false) {
      return (
        <SignalTile
          key={spec.id}
          compact
          label={label}
          unit={spec.unit ? t(`operations.units.${spec.unit}`) : undefined}
          title={t("operations.console.noMetricsSource")}
          value={<span className="font-mono text-muted-foreground">—</span>}
          foot={
            <Link to={signalsHref ?? href(DATA_SOURCES_PATH)} className="text-info hover:underline">
              {signalsHref ? s("tileFoot.bind") : t("operations.console.connectSource")}
            </Link>
          }
        />
      );
    }
    if (!found || found.value == null) {
      const waiting = (!found && golden.loading) || (bound === undefined && series.loading);
      let foot: ReactNode = null;
      if (!waiting) {
        if (sig?.error) {
          const errView = signalErrorView(sig.error.code);
          foot = readFailedFoot(
            errorTitle(sig.error),
            errView.action === "retry" ? (
              retryButton
            ) : errView.action === "edit" ? (
              <Button variant="link" size="sm" className="h-auto p-0 text-micro" onClick={() => editCurrent(spec, sig)}>
                {t(`operations.chart.errors.${errView.actionKey}`)}
              </Button>
            ) : undefined,
          );
        } else if ((seriesFailed && bound !== true) || (goldenFailed && !found)) {
          foot = readFailedFoot(s("tileFoot.readFailed"), retryButton);
        } else if (bound === true && vitalsLagLikely(spec.id, range, sourceKinds.get(spec.id))) {
          foot = t("operations.telemetry.mobileVitals.notYet");
        } else {
          foot = s("tileFoot.noData");
        }
      }
      return (
        <SignalTile
          key={spec.id}
          compact
          label={label}
          value={
            <span className="font-mono text-muted-foreground" aria-busy={waiting || undefined}>
              —
            </span>
          }
          foot={foot}
        />
      );
    }
    const parts = signalValueParts(found.value, found.unit, formatCtx);
    const previous = found.previous == null ? undefined : formatterFor(found.unit)(found.previous);
    return (
      <SignalTile
        key={spec.id}
        compact
        label={label}
        unit={parts.unit || undefined}
        value={<span className="font-mono tabular-nums">{parts.value}</span>}
        delta={signalDelta(spec.id, found.value, found.previous, found.unit, formatCtx)}
        foot={found.live ? s("tileFoot.live") : previous ? s("tileFoot.previousValue", { value: previous }) : undefined}
        sparkline={!found.live && hasTrend(found.spark) ? found.spark : undefined}
        sparklineColorIndex={1}
      />
    );
  };

  const chartSlot = (spec: SignalSpec) => {
    const title = t(`operations.signals.${spec.label}`);
    const sig = signalOf(spec.id);
    let status: ChartSlotStatus | undefined;
    let error: string | undefined;
    let failedTitle: string | undefined;
    let errorAction: ReactNode;
    let retry: (() => void) | undefined = () => void series.reload();
    if (series.loading) status = undefined;
    else if (seriesFailed) {
      status = "error";
      error = series.error;
    } else if (sig?.error) {
      const errView = signalErrorView(sig.error.code);
      status = "error";
      failedTitle = errorTitle(sig.error);
      error = errView.body ? t(`operations.chart.errors.${errView.key}Body`) : sig.error.message || undefined;
      if (errView.action !== "retry") retry = undefined;
      if (errView.action === "edit") {
        errorAction = (
          <Button size="sm" onClick={() => editCurrent(spec, sig)}>
            {t(`operations.chart.errors.${errView.actionKey}`)}
          </Button>
        );
      }
    } else if (isLiveSignal(sig)) status = "live";
    else status = sig?.bound ? "bound" : "unbound";
    const previousError = compare && status === "bound" ? sig?.previous_error : undefined;
    return (
      <ChartSlot
        key={spec.id}
        title={title}
        formatValue={sig ? formatterFor(sig.unit) : undefined}
        series={chartLines.get(spec.id)}
        status={status}
        error={error}
        errorTitle={failedTitle}
        errorAction={errorAction}
        note={
          previousError ? (
            <span title={previousError.message || undefined}>
              {t("operations.chart.errors.previousFailed", { reason: errorTitle(previousError) })}
            </span>
          ) : status === "live" ? (
            t("operations.queues.chart.liveNote")
          ) : undefined
        }
        live={
          status === "live" ? (
            <LiveValueList
              items={liveValueItems(chartLines.get(spec.id) ?? [])}
              formatValue={sig ? formatterFor(sig.unit) : undefined}
              ariaLabel={title}
              height={150}
            />
          ) : undefined
        }
        loading={series.loading}
        onRetry={retry}
        onBind={() => setBinding({ signal: spec.id, unit: sig?.unit })}
        manageHref={signalsHref}
        syncKey={syncKey}
        deployMarkers={markers}
        xRange={xRange}
      />
    );
  };

  const builtin = (spec: SignalSpec) => {
    let value: ReactNode = <span className="font-mono text-muted-foreground">—</span>;
    let foot: ReactNode = null;
    let state: "alarm" | "warn" | undefined;
    let title: string | undefined;
    let delta: SignalDelta | undefined;
    switch (spec.id) {
      case BUILTIN_SIGNALS.errors24h:
        if (environment) value = <span className="font-mono tabular-nums">{environment.error_count_24h}</span>;
        foot = environment ? s("tileFoot.errors24h") : s("tileFoot.noEnvironment", { env: envLabel });
        break;
      case BUILTIN_SIGNALS.openIssues: {
        const severe = openIncidents.some(isSevereIncident);
        value = <span className="font-mono tabular-nums">{openIncidents.length}</span>;
        state = openIncidents.length === 0 ? undefined : severe ? "alarm" : "warn";
        foot =
          openIncidents.length > 0 ? (
            <Link
              to={href(issuesSubPath)}
              className="text-info hover:underline"
              aria-label={t("operations.explorer.issuesLabel", { count: openIncidents.length, name: entity.name })}
            >
              {s("tileFoot.viewIssues")}
            </Link>
          ) : (
            s("tileFoot.noIssues", { env: envLabel })
          );
        break;
      }
      case BUILTIN_SIGNALS.deploysInRange: {
        value = <span className="font-mono tabular-nums">{inWindow.length}</span>;
        foot = s("tileFoot.inRange");
        if (compare) {
          const diff = inWindow.length - previousCount;
          delta = {
            text: diff > 0 ? `+${diff}` : String(diff),
            tone: "flat",
            direction: diff > 0 ? "up" : diff < 0 ? "down" : "flat",
            label: s("tileFoot.previous", { count: previousCount }),
          };
        }
        break;
      }
      case BUILTIN_SIGNALS.lastDeploy:
        if (lastRelease) {
          value = <span className="font-mono">{releaseLabel(lastRelease)}</span>;
          title = formatDate(releaseTime(lastRelease));
          foot = s("tileFoot.lastDeploy", {
            status: t(`release.statuses.${lastRelease.status}`),
            time: formatRelativeTime(releaseTime(lastRelease), lang),
          });
        } else {
          foot = s("tileFoot.noRelease", { env: envLabel });
        }
        break;
      case BUILTIN_SIGNALS.resource:
        if (environment) {
          value = environment.provider ? t(`cloud.providers.${environment.provider}`) : s("tileFoot.customUrl");
          foot = <span className="block truncate font-mono">{environment.resource_name || environment.url || envLabel}</span>;
        } else {
          foot = (
            <Link to={repositoryDeployPath(entity.repositoryId)} className="text-info hover:underline">
              {t("operations.entity.connect")}
            </Link>
          );
        }
        break;
    }
    return (
      <SignalTile
        key={spec.id}
        compact
        label={t(`operations.signals.${spec.label}`)}
        value={value}
        foot={foot}
        state={state}
        title={title}
        delta={delta}
      />
    );
  };

  return (
    <div className="@container">
      <div className="grid gap-4 @4xl:grid-cols-[minmax(0,1fr)_18rem]">
        <div className="@container min-w-0 space-y-4">
          {!environment && (
            <Notice variant="info" title={s("notReporting.title", { env: envLabel })}>
              <span>{s("notReporting.body", { env: envLabel })} </span>
              <Link to={repositoryDeployPath(entity.repositoryId)} className="inline-flex items-center gap-1 font-medium text-info hover:underline">
                <Plug className="h-3 w-3" aria-hidden />
                {t("operations.entity.connect")}
              </Link>
            </Notice>
          )}
          <ObservabilityReadinessCard entity={entity} env={env} />

          <section aria-labelledby={`${syncKey}-golden`} className="space-y-2">
            <h2 id={`${syncKey}-golden`} className="text-micro font-medium tracking-wide text-muted-foreground uppercase">
              {s("golden")}
            </h2>
            {goldenClamped && <p className="text-caption text-muted-foreground">{t("operations.console.goldenClamped")}</p>}
            <div className="grid grid-cols-2 gap-2 @xl:grid-cols-3 @3xl:grid-cols-4">
              {view.golden.map(goldenTile)}
              {BUILTIN_TILES.map(builtin)}
            </div>
          </section>

          {entity.role === "frontend" && (
            <WebVitalsStrip
              componentId={entity.id}
              env={env}
              range={range}
              formatCtx={formatCtx}
              onBind={(signal, unit) => setBinding({ signal, unit })}
              onEdit={(signal, unit) => setBinding({ signal, unit, current: true })}
              reloadToken={savedCount}
            />
          )}

          <section aria-labelledby={`${syncKey}-signals`} className="space-y-2">
            <h2 id={`${syncKey}-signals`} className="text-micro font-medium tracking-wide text-muted-foreground uppercase">
              {s("charts")}
            </h2>
            <Card className="px-3 pt-2 pb-1.5">
              <p className="mb-0.5 text-micro font-medium text-muted-foreground">{s("deployMarkers")}</p>
              <DeployTimeline markers={markers} from={range.from} to={range.to} label={s("deployMarkers")} />
            </Card>
            {view.charts.length > 0 ? (
              <div className="grid gap-3 @2xl:grid-cols-2">
                {view.charts.map(chartSlot)}
              </div>
            ) : (
              <Card className="px-4 py-6 text-center text-caption text-muted-foreground">{s("noCharts")}</Card>
            )}
          </section>
        </div>

        <EntityActivity
          className="@4xl:sticky @4xl:top-0 @4xl:self-start"
          items={activity}
          loading={releasesLoading || incidentsLoading}
          changeHref={(r) => href(`${changesPath}?release=${encodeURIComponent(r.id)}`)}
          issueHref={(i) => href(`${issuesSubPath}?id=${encodeURIComponent(i.id)}`)}
          changesHref={href(changesPath)}
          issuesHref={href(issuesSubPath)}
        />
      </div>

      {binding && (
        <SignalBindingEditor
          open
          onOpenChange={(open) => !open && setBinding(null)}
          componentId={entity.id}
          env={env}
          signal={binding.signal}
          unit={binding.unit}
          prefillCurrent={binding.current}
          previewWindow={range}
          onSaved={() => {
            void series.reload();
            void golden.reload();
            setSavedCount((n) => n + 1);
          }}
        />
      )}
    </div>
  );
}
