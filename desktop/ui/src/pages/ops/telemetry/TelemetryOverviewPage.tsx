import { useMemo } from "react";
import { Link } from "react-router-dom";
import { useOpsHref } from "@/components/operations/shell/OpsContext";
import { HealthMark } from "@/components/operations/telemetry/HealthMark";
import { RedCharts } from "@/components/operations/telemetry/RedCharts";
import { SeverityBadge } from "@/components/operations/telemetry/TelemetryBadges";
import { TelemetryQueryView } from "@/components/operations/telemetry/TelemetryQueryView";
import { TelemetrySection, TelemetrySectionEmpty, TelemetrySectionRow } from "@/components/operations/telemetry/TelemetrySection";
import { TelemetryToolbar } from "@/components/operations/telemetry/TelemetryToolbar";
import { StatTile } from "@/components/ui/stat-tile";
import { useI18n } from "@/hooks/useI18n";
import { useTelemetryQuery } from "@/hooks/useTelemetryQuery";
import { useTelemetryScope } from "@/hooks/useTelemetryScope";
import { intlLocale } from "@/lib/languages";
import { formatCount, formatMs, formatRatio, type TelemetryDeploy, type TelemetryIncident, type TelemetryKpi, type TelemetryService } from "@/lib/telemetryHub";
import { serviceHref } from "@/pages/ops/telemetry/TelemetryServicesPage";

const BACKEND = ["http", "worker", "consumer"];
const RANK: Record<string, number> = { critical: 0, warning: 1, ok: 2 };

/** `/operations/telemetry/overview`: the whole project at a glance: its RED
 * signals, what is firing, which services and objectives are hurting and what
 * shipped last. */
export default function TelemetryOverviewPage() {
  const { t, lang } = useI18n();
  const v = (key: string, params?: Record<string, string | number>) => t(`telemetryViews.overview.${key}`, params);
  const locale = intlLocale(lang);
  const href = useOpsHref();
  const scope = useTelemetryScope();
  const q = { range: scope.range, env: scope.env };
  const services = useTelemetryQuery<TelemetryService[]>("services", q);
  const incidents = useTelemetryQuery<TelemetryIncident[]>("incidents", { state: "open" });
  const kpis = useTelemetryQuery<TelemetryKpi[]>("kpis");
  const deploys = useTelemetryQuery<TelemetryDeploy[]>("deploys", q);
  const time = new Intl.DateTimeFormat(intlLocale(lang), { dateStyle: "short", timeStyle: "short" });

  const backend = useMemo(() => (services.data ?? []).filter((s) => BACKEND.includes(s.type)), [services.data]);
  const hurting = useMemo(
    () => backend.filter((s) => s.status !== "ok").sort((a, b) => (RANK[a.status] ?? 3) - (RANK[b.status] ?? 3) || b.rpm - a.rpm),
    [backend],
  );
  const atRisk = useMemo(() => (kpis.data ?? []).filter((k) => k.status !== "ok"), [kpis.data]);
  const open = incidents.data ?? [];
  const recent = useMemo(() => [...(deploys.data ?? [])].sort((a, b) => b.ts - a.ts).slice(0, 5), [deploys.data]);

  return (
    <div className="space-y-4">
      <TelemetryToolbar />
      <TelemetryQueryView query={services} isEmpty={(d) => d.length === 0} emptyTitle={v("empty")} emptyDescription={v("emptyHint")} skeletonHeight="h-24">
        {(all) => (
          <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
            <StatTile label={v("services")} value={formatCount(all.length, locale)} foot={v("backend", { count: backend.length })} />
            <StatTile label={v("unhealthy")} value={formatCount(hurting.length, locale)} tone={hurting.length > 0 ? "warning" : "default"} />
            <StatTile label={v("openIncidents")} value={incidents.data ? formatCount(open.length, locale) : "-"} tone={open.length > 0 ? "warning" : "default"} />
            <StatTile label={v("slosAtRisk")} value={kpis.data ? formatCount(atRisk.length, locale) : "-"} tone={atRisk.length > 0 ? "warning" : "default"} />
          </div>
        )}
      </TelemetryQueryView>

      <RedCharts path="overview/series" scope={scope} deploys={deploys.data} />

      <div className="grid gap-4 lg:grid-cols-2">
        <TelemetrySection title={v("incidentsTitle")} more={{ to: "/operations/telemetry/incidents", label: v("seeAll") }}>
          {open.length === 0 ? (
            <TelemetrySectionEmpty>{incidents.data ? v("noIncidents") : v("loading")}</TelemetrySectionEmpty>
          ) : (
            open.slice(0, 5).map((i) => (
              <TelemetrySectionRow key={i.id}>
                <SeverityBadge severity={i.severity} />
                <span className="min-w-0 flex-1">
                  <span className="block truncate font-medium">{i.title}</span>
                  <span className="block truncate text-micro text-muted-foreground">{[i.service, i.value].filter(Boolean).join(" · ")}</span>
                </span>
                <span className="shrink-0 text-micro text-muted-foreground">{time.format(i.startedAt)}</span>
              </TelemetrySectionRow>
            ))
          )}
        </TelemetrySection>

        <TelemetrySection title={v("attentionTitle")} more={{ to: "/operations/telemetry/services", label: v("seeAll") }}>
          {hurting.length === 0 ? (
            <TelemetrySectionEmpty>{services.data ? v("allHealthy") : v("loading")}</TelemetrySectionEmpty>
          ) : (
            hurting.slice(0, 6).map((s) => (
              <TelemetrySectionRow key={s.id}>
                <HealthMark health={s.status} />
                <Link to={href(serviceHref(s.name))} className="min-w-0 flex-1 truncate font-medium hover:underline">
                  {s.name}
                </Link>
                <span className="shrink-0 font-mono text-micro text-muted-foreground">
                  {v("serviceSignals", { errors: formatRatio(s.errorRate, locale), p95: formatMs(s.p95, locale) })}
                </span>
              </TelemetrySectionRow>
            ))
          )}
        </TelemetrySection>

        <TelemetrySection title={v("slosTitle")} more={{ to: "/operations/telemetry/slo", label: v("seeAll") }}>
          {atRisk.length === 0 ? (
            <TelemetrySectionEmpty>{kpis.data ? v("slosFine") : v("loading")}</TelemetrySectionEmpty>
          ) : (
            atRisk.slice(0, 5).map((k) => (
              <TelemetrySectionRow key={k.id}>
                <HealthMark health={k.status} />
                <span className="min-w-0 flex-1 truncate font-medium">{k.name}</span>
                <span className="shrink-0 font-mono text-micro text-muted-foreground">{v("budgetLeft", { value: formatRatio(k.budgetRemaining, locale) })}</span>
              </TelemetrySectionRow>
            ))
          )}
        </TelemetrySection>

        <TelemetrySection title={v("deploysTitle")} more={{ to: "/operations/telemetry/deploys", label: v("seeAll") }}>
          {recent.length === 0 ? (
            <TelemetrySectionEmpty>{deploys.data ? v("noDeploys") : v("loading")}</TelemetrySectionEmpty>
          ) : (
            recent.map((d) => (
              <TelemetrySectionRow key={`${d.service}:${d.version}:${d.ts}`}>
                <span className="shrink-0 font-mono">{d.version}</span>
                <span className="min-w-0 flex-1 truncate">{[d.service, d.env].filter(Boolean).join(" · ")}</span>
                <span className="shrink-0 text-micro text-muted-foreground">{time.format(d.ts)}</span>
              </TelemetrySectionRow>
            ))
          )}
        </TelemetrySection>
      </div>
    </div>
  );
}
