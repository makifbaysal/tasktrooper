import { ArrowLeft, Bug, Route as RouteIcon, ScrollText } from "lucide-react";
import { Link, useParams } from "react-router-dom";
import { useOpsHref } from "@/components/operations/shell/OpsContext";
import { DeploysTable } from "@/components/operations/telemetry/DeploysTable";
import { EndpointsTable } from "@/components/operations/telemetry/EndpointsTable";
import { HealthMark } from "@/components/operations/telemetry/HealthMark";
import { RedCharts } from "@/components/operations/telemetry/RedCharts";
import { TelemetryQueryView } from "@/components/operations/telemetry/TelemetryQueryView";
import { TelemetryCell, TelemetryRow, TelemetryTable } from "@/components/operations/telemetry/TelemetryTable";
import { TelemetryToolbar } from "@/components/operations/telemetry/TelemetryToolbar";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { StatTile } from "@/components/ui/stat-tile";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { useI18n } from "@/hooks/useI18n";
import { useTelemetryQuery } from "@/hooks/useTelemetryQuery";
import { useTelemetryScope } from "@/hooks/useTelemetryScope";
import { useUrlParam } from "@/hooks/useUrlParam";
import { intlLocale } from "@/lib/languages";
import { formatCount, formatMs, formatRatio, type TelemetryDependency, type TelemetryDeploy, type TelemetryEndpoint, type TelemetryService } from "@/lib/telemetryHub";

const TABS = ["endpoints", "dependencies", "deploys"] as const;
type Tab = (typeof TABS)[number];

/** `/operations/telemetry/services/:name`: one service's golden signals, and
 * under them its endpoints, what it calls and what was deployed. */
export default function TelemetryServicePage() {
  const { name = "" } = useParams();
  const { t, lang } = useI18n();
  const h = (key: string, params?: Record<string, string | number>) => t(`telemetryHub.service.${key}`, params);
  const locale = intlLocale(lang);
  const href = useOpsHref();
  const scope = useTelemetryScope();
  const [tabParam, setTab] = useUrlParam("tab");
  const tab: Tab = TABS.find((x) => x === tabParam) ?? "endpoints";
  const base = `services/${encodeURIComponent(name)}`;
  const q = { range: scope.range, env: scope.env };

  const service = useTelemetryQuery<TelemetryService>(base, q);
  const endpoints = useTelemetryQuery<TelemetryEndpoint[]>(`${base}/endpoints`, q);
  const dependencies = useTelemetryQuery<TelemetryDependency[]>(`${base}/dependencies`, q, { enabled: tab === "dependencies", intervalMs: 0 });
  const deploys = useTelemetryQuery<TelemetryDeploy[]>(`${base}/deploys`, q);
  const linkTo = (path: string) => href(`${path}?service=${encodeURIComponent(name)}`);

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center gap-2">
        <Button asChild variant="ghost" size="sm">
          <Link to={href("/operations/telemetry/services")}>
            <ArrowLeft aria-hidden />
            {h("back")}
          </Link>
        </Button>
        <div className="ml-auto flex flex-wrap items-center gap-2">
          <Button asChild variant="outline" size="sm">
            <Link to={linkTo("/operations/telemetry/traces")}>
              <RouteIcon aria-hidden />
              {h("traces")}
            </Link>
          </Button>
          <Button asChild variant="outline" size="sm">
            <Link to={linkTo("/operations/telemetry/logs")}>
              <ScrollText aria-hidden />
              {h("logs")}
            </Link>
          </Button>
          <Button asChild variant="outline" size="sm">
            <Link to={linkTo("/operations/telemetry/errors")}>
              <Bug aria-hidden />
              {h("errors")}
            </Link>
          </Button>
        </div>
      </div>

      <TelemetryToolbar />

      <TelemetryQueryView query={service}>
        {(s) => (
          <div className="space-y-4">
            <div className="flex flex-wrap items-center gap-2">
              <HealthMark health={s.status} showLabel />
              <h2 className="text-heading font-semibold">{s.name}</h2>
              {s.type && <Badge variant="secondary">{s.type}</Badge>}
              {s.language && <Badge variant="outline">{s.language}</Badge>}
              {s.version && <Badge variant="outline" className="font-mono">{s.version}</Badge>}
            </div>
            <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
              <StatTile label={h("rpm")} value={formatCount(s.rpm, locale)} foot={h("previous", { value: formatCount(s.prevRpm, locale) })} />
              <StatTile
                label={h("errorRate")}
                value={formatRatio(s.errorRate, locale)}
                tone={s.status !== "ok" && s.errorRate > s.prevErrorRate ? "warning" : "default"}
                foot={h("previous", { value: formatRatio(s.prevErrorRate, locale) })}
              />
              <StatTile label={h("p95")} value={formatMs(s.p95, locale)} foot={h("previous", { value: formatMs(s.prevP95, locale) })} />
              <StatTile label={h("instances")} value={formatCount(s.instances, locale)} />
            </div>
          </div>
        )}
      </TelemetryQueryView>

      <RedCharts path={`${base}/series`} scope={scope} deploys={deploys.data} />

      <Tabs value={tab} onValueChange={(v) => setTab(v === "endpoints" ? null : v)}>
        <TabsList aria-label={h("sections")}>
          <TabsTrigger value="endpoints">{h("tabs.endpoints")}</TabsTrigger>
          <TabsTrigger value="dependencies">{h("tabs.dependencies")}</TabsTrigger>
          <TabsTrigger value="deploys">{h("tabs.deploys")}</TabsTrigger>
        </TabsList>
        <TabsContent value="endpoints" className="pt-4">
          <TelemetryQueryView query={endpoints} isEmpty={(d) => d.length === 0} emptyTitle={h("noEndpoints")}>
            {(rows) => <EndpointsTable service={name} endpoints={rows} />}
          </TelemetryQueryView>
        </TabsContent>
        <TabsContent value="dependencies" className="pt-4">
          <TelemetryQueryView query={dependencies} isEmpty={(d) => d.length === 0} emptyTitle={h("noDependencies")}>
            {(rows) => (
              <TelemetryTable
                label={h("tabs.dependencies")}
                columns={[
                  { key: "name", label: h("dependencyColumns.name") },
                  { key: "kind", label: h("dependencyColumns.kind") },
                  { key: "calls", label: h("dependencyColumns.calls"), numeric: true },
                  { key: "avg", label: h("dependencyColumns.avg"), numeric: true },
                  { key: "p95", label: h("dependencyColumns.p95"), numeric: true },
                  { key: "err", label: h("dependencyColumns.errorRate"), numeric: true },
                ]}
              >
                {rows.map((d) => (
                  <TelemetryRow key={d.id}>
                    <TelemetryCell className="font-medium">{d.name}</TelemetryCell>
                    <TelemetryCell>
                      <Badge variant="secondary">{d.kind}</Badge>
                    </TelemetryCell>
                    <TelemetryCell numeric>{formatCount(d.callsPerMin, locale)}</TelemetryCell>
                    <TelemetryCell numeric>{formatMs(d.avgMs, locale)}</TelemetryCell>
                    <TelemetryCell numeric>{formatMs(d.p95, locale)}</TelemetryCell>
                    <TelemetryCell numeric>{formatRatio(d.errorRate, locale)}</TelemetryCell>
                  </TelemetryRow>
                ))}
              </TelemetryTable>
            )}
          </TelemetryQueryView>
        </TabsContent>
        <TabsContent value="deploys" className="pt-4">
          <TelemetryQueryView query={deploys} isEmpty={(d) => d.length === 0} emptyTitle={h("noDeploys")}>
            {(rows) => <DeploysTable deploys={rows} showService={false} />}
          </TelemetryQueryView>
        </TabsContent>
      </Tabs>
    </div>
  );
}
