import { Plug } from "lucide-react";
import { useMemo, type ReactNode } from "react";
import { Link } from "react-router-dom";
import type { Release } from "@/api";
import type { DeployMarker } from "@/components/operations/charts/TimeSeriesChart";
import { ChartSlot, DATA_SOURCES_PATH } from "@/components/operations/console/ChartSlot";
import { entityPath, releaseLabel, releaseTime, repositoryDeployPath } from "@/components/operations/console/opsEntities";
import { isSevereIncident } from "@/components/operations/console/opsStatus";
import { BUILTIN_SIGNALS, BUILTIN_TILES, viewFor, type SignalSpec } from "@/components/operations/console/roleViews";
import { SignalTile, type SignalDelta } from "@/components/operations/console/SignalTile";
import { DeployTimeline } from "@/components/operations/entity/DeployTimeline";
import { EntityActivity, buildActivity } from "@/components/operations/entity/EntityActivity";
import { inRange, previousPeriod, useOpsContext, useOpsHref, useOpsPageScope } from "@/components/operations/shell/OpsContext";
import { Card } from "@/components/ui/card";
import { Notice } from "@/components/ui/notice";
import { useI18n } from "@/hooks/useI18n";
import { useEntityHealth, useEntityIncidents, useEntityReleases } from "@/hooks/useOpsEntityData";
import { formatDate, formatRelativeTime } from "@/lib/utils";
import type { EntitySubPageProps } from "@/pages/ops/entity/types";

function at(release: Release): number {
  return Date.parse(releaseTime(release));
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

  const golden = (spec: SignalSpec) => (
    <SignalTile
      key={spec.id}
      compact
      label={t(`operations.signals.${spec.label}`)}
      unit={spec.unit ? t(`operations.units.${spec.unit}`) : undefined}
      title={t("operations.console.noMetricsSource")}
      value={<span className="font-mono text-muted-foreground">—</span>}
      foot={
        <Link to={href(DATA_SOURCES_PATH)} className="text-info hover:underline">
          {t("operations.console.connectSource")}
        </Link>
      }
    />
  );

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

          <section aria-labelledby={`${syncKey}-golden`} className="space-y-2">
            <h2 id={`${syncKey}-golden`} className="text-micro font-medium tracking-wide text-muted-foreground uppercase">
              {s("golden")}
            </h2>
            <div className="grid grid-cols-2 gap-2 @xl:grid-cols-3 @3xl:grid-cols-4">
              {view.golden.map(golden)}
              {BUILTIN_TILES.map(builtin)}
            </div>
          </section>

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
                {view.charts.map((spec) => (
                  <ChartSlot
                    key={spec.id}
                    title={t(`operations.signals.${spec.label}`)}
                    unit={spec.unit ? t(`operations.units.${spec.unit}`) : undefined}
                    syncKey={syncKey}
                    deployMarkers={markers}
                    xRange={xRange}
                  />
                ))}
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
    </div>
  );
}
