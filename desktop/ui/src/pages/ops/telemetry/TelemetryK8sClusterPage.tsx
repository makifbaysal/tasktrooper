import { useParams } from "react-router-dom";
import { InfraHealthMark } from "@/components/operations/telemetry/HealthMark";
import { K8sEventsTable, PodsTable } from "@/components/operations/telemetry/K8sTables";
import { SeriesChartCard } from "@/components/operations/telemetry/SeriesChartCard";
import { TelemetryBackLink } from "@/components/operations/telemetry/TelemetryBackLink";
import { TelemetryQueryView } from "@/components/operations/telemetry/TelemetryQueryView";
import { TelemetryCell, TelemetryRow, TelemetryTable } from "@/components/operations/telemetry/TelemetryTable";
import { UsageMeter } from "@/components/operations/telemetry/UsageMeter";
import { Badge } from "@/components/ui/badge";
import { StatTile } from "@/components/ui/stat-tile";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { useI18n } from "@/hooks/useI18n";
import { useTelemetryQuery } from "@/hooks/useTelemetryQuery";
import { useTelemetryScope } from "@/hooks/useTelemetryScope";
import { useUrlParam } from "@/hooks/useUrlParam";
import { intlLocale } from "@/lib/languages";
import { formatBytes, formatCores, type TelemetryK8sClusterDetail, type TelemetryK8sEvent, type TelemetryPod } from "@/lib/telemetryViews";

const TABS = ["nodes", "namespaces", "pods", "events"] as const;
type Tab = (typeof TABS)[number];

/** `/operations/telemetry/infra/kubernetes/clusters/:cluster`: one cluster's
 * capacity over the window, its nodes and namespaces, pods and warnings. */
export default function TelemetryK8sClusterPage() {
  const { cluster = "" } = useParams();
  const { t, lang } = useI18n();
  const v = (key: string, params?: Record<string, string | number>) => t(`telemetryViews.k8s.${key}`, params);
  const locale = intlLocale(lang);
  const scope = useTelemetryScope();
  const [tabParam, setTab] = useUrlParam("tab");
  const tab: Tab = TABS.find((x) => x === tabParam) ?? "nodes";
  const read = useTelemetryQuery<TelemetryK8sClusterDetail>(`infra/k8s/clusters/${encodeURIComponent(cluster)}`, { range: scope.range });
  const pods = useTelemetryQuery<TelemetryPod[]>("infra/k8s/pods", { range: scope.range, cluster }, { enabled: tab === "pods" });
  const events = useTelemetryQuery<TelemetryK8sEvent[]>("infra/k8s/events", { range: scope.range, cluster, limit: 200 }, { enabled: tab === "events" });
  const loading = read.data === undefined && !read.error;
  const lines = { used: v("used"), requested: v("requested"), allocatable: v("allocatable") };

  return (
    <div className="space-y-4">
      <TelemetryBackLink to="/operations/telemetry/infra/kubernetes" label={v("back")} />
      <TelemetryQueryView query={read}>
        {(c) => (
          <div className="space-y-4">
            <div className="flex flex-wrap items-center gap-2">
              <InfraHealthMark status={c.status} showLabel />
              <h2 className="text-heading font-semibold">{c.name}</h2>
            </div>
            <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
              <StatTile label={v("columns.nodes")} value={`${c.nodesReady}/${c.nodes}`} foot={v("nodesReady")} />
              <StatTile label={v("columns.pods")} value={`${c.podsRunning}/${c.pods}`} tone={c.podsFailing > 0 ? "warning" : "default"} foot={v("failing", { count: c.podsFailing })} />
              <StatTile label={v("columns.cpu")} value={formatCores(c.cpuUsed, locale)} foot={v("ofAllocatable", { value: formatCores(c.cpuAllocatable, locale) })} />
              <StatTile label={v("columns.memory")} value={formatBytes(c.memoryUsed, locale)} foot={v("ofAllocatable", { value: formatBytes(c.memoryAllocatable, locale) })} />
            </div>
            <div className="grid gap-4 lg:grid-cols-2">
              <SeriesChartCard title={v("cpuChart")} set={c.series?.cpu} scope={scope} loading={loading} labels={lines} formatValue={(n) => formatCores(n, locale)} syncKey="tel:k8s" />
              <SeriesChartCard title={v("memoryChart")} set={c.series?.memory} scope={scope} loading={loading} labels={lines} formatValue={(n) => formatBytes(n, locale)} syncKey="tel:k8s" />
            </div>
            <Tabs value={tab} onValueChange={(x) => setTab(x === "nodes" ? null : x)}>
              <TabsList aria-label={v("sections")}>
                {TABS.map((x) => (
                  <TabsTrigger key={x} value={x}>
                    {v(`tabs.${x}`)}
                  </TabsTrigger>
                ))}
              </TabsList>
              <TabsContent value="nodes" className="pt-4">
                <TelemetryTable
                  label={v("tabs.nodes")}
                  columns={[
                    { key: "health", label: v("columns.health"), className: "w-12" },
                    { key: "name", label: v("columns.node") },
                    { key: "ready", label: v("columns.ready") },
                    { key: "pods", label: v("columns.pods"), numeric: true },
                    { key: "cpu", label: v("columns.cpu"), className: "w-40" },
                    { key: "memory", label: v("columns.memory"), className: "w-40" },
                  ]}
                >
                  {(c.nodeList ?? []).map((n) => (
                    <TelemetryRow key={n.name}>
                      <TelemetryCell>
                        <InfraHealthMark status={n.status} />
                      </TelemetryCell>
                      <TelemetryCell className="font-medium">{n.name}</TelemetryCell>
                      <TelemetryCell>
                        <Badge variant={n.ready ? "success" : "destructive"}>{n.ready ? v("ready") : v("notReady")}</Badge>
                      </TelemetryCell>
                      <TelemetryCell numeric>{n.pods}</TelemetryCell>
                      <TelemetryCell>
                        <UsageMeter used={n.cpu} limit={n.cpuAllocatable} label={v("columns.cpu")} caption={v("ofLimit", { used: formatCores(n.cpu, locale), limit: formatCores(n.cpuAllocatable, locale) })} />
                      </TelemetryCell>
                      <TelemetryCell>
                        <UsageMeter
                          used={n.memoryBytes}
                          limit={n.memoryAllocatable}
                          label={v("columns.memory")}
                          caption={v("ofLimit", { used: formatBytes(n.memoryBytes, locale), limit: formatBytes(n.memoryAllocatable, locale) })}
                        />
                      </TelemetryCell>
                    </TelemetryRow>
                  ))}
                </TelemetryTable>
              </TabsContent>
              <TabsContent value="namespaces" className="pt-4">
                <TelemetryTable
                  label={v("tabs.namespaces")}
                  columns={[
                    { key: "name", label: v("columns.namespace") },
                    { key: "workloads", label: v("columns.workloads"), numeric: true },
                    { key: "pods", label: v("columns.pods"), numeric: true },
                    { key: "failing", label: v("columns.failing"), numeric: true },
                  ]}
                >
                  {[...(c.namespaceList ?? [])]
                    .sort((a, b) => b.failing - a.failing || a.name.localeCompare(b.name))
                    .map((n) => (
                      <TelemetryRow key={n.name}>
                        <TelemetryCell className="font-medium">{n.name}</TelemetryCell>
                        <TelemetryCell numeric>{n.workloads}</TelemetryCell>
                        <TelemetryCell numeric>{n.pods}</TelemetryCell>
                        <TelemetryCell numeric className={n.failing > 0 ? "text-destructive" : undefined}>
                          {n.failing}
                        </TelemetryCell>
                      </TelemetryRow>
                    ))}
                </TelemetryTable>
              </TabsContent>
              <TabsContent value="pods" className="pt-4">
                <TelemetryQueryView query={pods} isEmpty={(d) => d.length === 0} emptyTitle={v("noPods")}>
                  {(rows) => <PodsTable pods={rows} />}
                </TelemetryQueryView>
              </TabsContent>
              <TabsContent value="events" className="pt-4">
                <TelemetryQueryView query={events} isEmpty={(d) => d.length === 0} emptyTitle={v("noEvents")}>
                  {(rows) => <K8sEventsTable events={rows} />}
                </TelemetryQueryView>
              </TabsContent>
            </Tabs>
          </div>
        )}
      </TelemetryQueryView>
    </div>
  );
}
