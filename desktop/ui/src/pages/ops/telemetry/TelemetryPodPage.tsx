import { Link, useParams } from "react-router-dom";
import { DetailFacts } from "@/components/operations/console/DetailPanel";
import { useOpsHref } from "@/components/operations/shell/OpsContext";
import { InfraHealthMark } from "@/components/operations/telemetry/HealthMark";
import { K8sEventsTable } from "@/components/operations/telemetry/K8sTables";
import { SeriesChartCard } from "@/components/operations/telemetry/SeriesChartCard";
import { TelemetryBackLink } from "@/components/operations/telemetry/TelemetryBackLink";
import { TelemetryQueryView } from "@/components/operations/telemetry/TelemetryQueryView";
import { TelemetryCell, TelemetryRow, TelemetryTable } from "@/components/operations/telemetry/TelemetryTable";
import { Badge } from "@/components/ui/badge";
import { Card } from "@/components/ui/card";
import { useI18n } from "@/hooks/useI18n";
import { useTelemetryQuery } from "@/hooks/useTelemetryQuery";
import { useTelemetryScope } from "@/hooks/useTelemetryScope";
import { intlLocale } from "@/lib/languages";
import { formatBytes, formatCores, type TelemetryPodDetail } from "@/lib/telemetryViews";
import { k8sClusterHref } from "@/pages/ops/telemetry/TelemetryKubernetesPage";
import { serviceHref } from "@/pages/ops/telemetry/TelemetryServicesPage";

/** `/operations/telemetry/infra/kubernetes/pods/:uid`: one pod's usage against
 * its requests and limits, its restarts, containers and warning events. */
export default function TelemetryPodPage() {
  const { uid = "" } = useParams();
  const { t, lang } = useI18n();
  const v = (key: string, params?: Record<string, string | number>) => t(`telemetryViews.k8s.${key}`, params);
  const locale = intlLocale(lang);
  const href = useOpsHref();
  const scope = useTelemetryScope();
  const read = useTelemetryQuery<TelemetryPodDetail>(`infra/k8s/pods/${encodeURIComponent(uid)}`, { range: scope.range });
  const loading = read.data === undefined && !read.error;
  const lines = { usage: v("usage"), request: v("request"), limit: v("limit") };

  return (
    <div className="space-y-4">
      <TelemetryBackLink to="/operations/telemetry/infra/kubernetes?tab=pods" label={v("backToPods")} />
      <TelemetryQueryView query={read}>
        {(p) => (
          <div className="space-y-4">
            <div className="flex flex-wrap items-center gap-2">
              <InfraHealthMark status={p.status} showLabel />
              <h2 className="text-heading font-semibold break-all">{p.name}</h2>
              <Badge variant={p.reason ? "destructive" : p.phase === "Running" ? "success" : "secondary"}>{p.reason || p.phase}</Badge>
            </div>
            <Card className="p-4">
              <DetailFacts
                items={[
                  { label: v("columns.cluster"), value: p.cluster ? <Link className="hover:underline" to={href(k8sClusterHref(p.cluster))}>{p.cluster}</Link> : "-" },
                  { label: v("columns.namespace"), value: p.namespace || "-" },
                  { label: v("columns.node"), value: p.node || "-" },
                  { label: v("columns.workload"), value: p.workload ? `${p.workloadKind}/${p.workload}` : "-" },
                  { label: v("service"), value: p.service ? <Link className="hover:underline" to={href(serviceHref(p.service))}>{p.service}</Link> : "-" },
                  { label: v("columns.restarts"), value: v("restartsValue", { total: p.restarts, window: p.restartsInRange }) },
                ]}
              />
            </Card>
            <div className="grid gap-4 lg:grid-cols-3">
              <SeriesChartCard title={v("cpuChart")} set={p.series?.cpu} scope={scope} loading={loading} labels={lines} formatValue={(n) => formatCores(n, locale)} syncKey="tel:pod" />
              <SeriesChartCard title={v("memoryChart")} set={p.series?.memory} scope={scope} loading={loading} labels={lines} formatValue={(n) => formatBytes(n, locale)} syncKey="tel:pod" />
              <SeriesChartCard title={v("restartsChart")} set={p.series?.restarts} scope={scope} loading={loading} syncKey="tel:pod" />
            </div>
            {(p.containers ?? []).length > 0 && (
              <TelemetryTable
                label={v("containers")}
                columns={[
                  { key: "name", label: v("containerColumns.name") },
                  { key: "cpu", label: v("columns.cpuCores"), numeric: true },
                  { key: "memory", label: v("columns.memory"), numeric: true },
                  { key: "restarts", label: v("columns.restarts"), numeric: true },
                ]}
              >
                {(p.containers ?? []).map((c) => (
                  <TelemetryRow key={c.name}>
                    <TelemetryCell className="max-w-sm">
                      <span className="block truncate font-medium">{c.name}</span>
                      <span className="block truncate font-mono text-micro text-muted-foreground">{c.image}</span>
                    </TelemetryCell>
                    <TelemetryCell numeric>{formatCores(c.cpu, locale)}</TelemetryCell>
                    <TelemetryCell numeric>{formatBytes(c.memoryBytes, locale)}</TelemetryCell>
                    <TelemetryCell numeric>{c.restarts}</TelemetryCell>
                  </TelemetryRow>
                ))}
              </TelemetryTable>
            )}
            {(p.events ?? []).length > 0 && <K8sEventsTable events={p.events ?? []} />}
          </div>
        )}
      </TelemetryQueryView>
    </div>
  );
}
