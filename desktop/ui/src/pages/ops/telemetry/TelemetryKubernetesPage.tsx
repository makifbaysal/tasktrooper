import { useMemo } from "react";
import { Link } from "react-router-dom";
import { useOpsHref } from "@/components/operations/shell/OpsContext";
import { InfraHealthMark } from "@/components/operations/telemetry/HealthMark";
import { K8sEventsTable, PodsTable } from "@/components/operations/telemetry/K8sTables";
import { TelemetryQueryView } from "@/components/operations/telemetry/TelemetryQueryView";
import { TelemetryCell, TelemetryRow, TelemetryTable } from "@/components/operations/telemetry/TelemetryTable";
import { TelemetryToolbar } from "@/components/operations/telemetry/TelemetryToolbar";
import { UsageMeter } from "@/components/operations/telemetry/UsageMeter";
import { Badge } from "@/components/ui/badge";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { useI18n } from "@/hooks/useI18n";
import { useTelemetryQuery } from "@/hooks/useTelemetryQuery";
import { useTelemetryScope } from "@/hooks/useTelemetryScope";
import { useUrlParam } from "@/hooks/useUrlParam";
import { intlLocale } from "@/lib/languages";
import { formatBytes, formatCores, statusRank, type TelemetryK8sCluster, type TelemetryK8sEvent, type TelemetryPod, type TelemetryWorkload } from "@/lib/telemetryViews";

const TABS = ["clusters", "workloads", "pods", "events"] as const;
type Tab = (typeof TABS)[number];
const ALL = "__all__";

export function k8sClusterHref(name: string): string {
  return `/operations/telemetry/infra/kubernetes/clusters/${encodeURIComponent(name)}`;
}

/** `/operations/telemetry/infra/kubernetes`: clusters with node and pod
 * health, and across them the workloads, pods and warning events. */
export default function TelemetryKubernetesPage() {
  const { t, lang } = useI18n();
  const v = (key: string, params?: Record<string, string | number>) => t(`telemetryViews.k8s.${key}`, params);
  const locale = intlLocale(lang);
  const href = useOpsHref();
  const scope = useTelemetryScope();
  const [tabParam, setTab] = useUrlParam("tab");
  const [cluster, setCluster] = useUrlParam("cluster");
  const tab: Tab = TABS.find((x) => x === tabParam) ?? "clusters";
  const filter = { range: scope.range, cluster: cluster || undefined };
  const clusters = useTelemetryQuery<TelemetryK8sCluster[]>("infra/k8s/clusters", { range: scope.range });
  const workloads = useTelemetryQuery<TelemetryWorkload[]>("infra/k8s/workloads", filter, { enabled: tab === "workloads" });
  const pods = useTelemetryQuery<TelemetryPod[]>("infra/k8s/pods", filter, { enabled: tab === "pods" });
  const events = useTelemetryQuery<TelemetryK8sEvent[]>("infra/k8s/events", { ...filter, limit: 200 }, { enabled: tab === "events" });
  const sortedWorkloads = useMemo(() => [...(workloads.data ?? [])].sort((a, b) => statusRank(a.status) - statusRank(b.status) || b.restarts - a.restarts), [workloads.data]);

  return (
    <div className="space-y-4">
      <TelemetryToolbar env={false}>
        <Select value={cluster || ALL} onValueChange={(x) => setCluster(x === ALL ? null : x)}>
          <SelectTrigger className="h-8 w-52" aria-label={v("clusterFilter")}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={ALL}>{v("allClusters")}</SelectItem>
            {(clusters.data ?? []).map((c) => (
              <SelectItem key={c.name} value={c.name}>
                {c.name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </TelemetryToolbar>

      <Tabs value={tab} onValueChange={(x) => setTab(x === "clusters" ? null : x)}>
        <TabsList aria-label={v("sections")}>
          {TABS.map((x) => (
            <TabsTrigger key={x} value={x}>
              {v(`tabs.${x}`)}
            </TabsTrigger>
          ))}
        </TabsList>
        <TabsContent value="clusters" className="pt-4">
          <TelemetryQueryView query={clusters} isEmpty={(d) => d.length === 0} emptyTitle={v("empty")} emptyDescription={v("emptyHint")}>
            {(rows) => (
              <TelemetryTable
                label={v("clustersLabel")}
                columns={[
                  { key: "health", label: v("columns.health"), className: "w-12" },
                  { key: "name", label: v("columns.cluster") },
                  { key: "nodes", label: v("columns.nodes"), numeric: true },
                  { key: "pods", label: v("columns.pods"), numeric: true },
                  { key: "cpu", label: v("columns.cpu"), className: "w-40" },
                  { key: "memory", label: v("columns.memory"), className: "w-40" },
                  { key: "events", label: v("columns.warnings"), numeric: true },
                ]}
              >
                {[...rows]
                  .filter((c) => !cluster || c.name === cluster)
                  .sort((a, b) => statusRank(a.status) - statusRank(b.status))
                  .map((c) => (
                    <TelemetryRow key={c.name}>
                      <TelemetryCell>
                        <InfraHealthMark status={c.status} />
                      </TelemetryCell>
                      <TelemetryCell>
                        <Link to={href(k8sClusterHref(c.name))} className="font-medium hover:underline">
                          {c.name}
                        </Link>
                        <p className="text-micro text-muted-foreground">{v("clusterMeta", { namespaces: c.namespaces, workloads: c.workloads })}</p>
                      </TelemetryCell>
                      <TelemetryCell numeric>{`${c.nodesReady}/${c.nodes}`}</TelemetryCell>
                      <TelemetryCell numeric>
                        {`${c.podsRunning}/${c.pods}`}
                        {c.podsFailing > 0 && (
                          <Badge variant="destructive" className="ml-2">
                            {v("failing", { count: c.podsFailing })}
                          </Badge>
                        )}
                      </TelemetryCell>
                      <TelemetryCell>
                        <UsageMeter used={c.cpuUsed} limit={c.cpuAllocatable} label={v("columns.cpu")} caption={v("ofLimit", { used: formatCores(c.cpuUsed, locale), limit: formatCores(c.cpuAllocatable, locale) })} />
                      </TelemetryCell>
                      <TelemetryCell>
                        <UsageMeter
                          used={c.memoryUsed}
                          limit={c.memoryAllocatable}
                          label={v("columns.memory")}
                          caption={v("ofLimit", { used: formatBytes(c.memoryUsed, locale), limit: formatBytes(c.memoryAllocatable, locale) })}
                        />
                      </TelemetryCell>
                      <TelemetryCell numeric>{c.warningEvents}</TelemetryCell>
                    </TelemetryRow>
                  ))}
              </TelemetryTable>
            )}
          </TelemetryQueryView>
        </TabsContent>
        <TabsContent value="workloads" className="pt-4">
          <TelemetryQueryView query={workloads} isEmpty={(d) => d.length === 0} emptyTitle={v("noWorkloads")}>
            {() => (
              <TelemetryTable
                label={v("tabs.workloads")}
                columns={[
                  { key: "health", label: v("columns.health"), className: "w-12" },
                  { key: "name", label: v("columns.workload") },
                  { key: "kind", label: v("columns.kind") },
                  { key: "ready", label: v("columns.ready"), numeric: true },
                  { key: "restarts", label: v("columns.restarts"), numeric: true },
                  { key: "cpu", label: v("columns.cpuCores"), numeric: true },
                  { key: "memory", label: v("columns.memory"), numeric: true },
                ]}
              >
                {sortedWorkloads.map((w) => (
                  <TelemetryRow key={`${w.cluster}/${w.namespace}/${w.kind}/${w.name}`}>
                    <TelemetryCell>
                      <InfraHealthMark status={w.status} />
                    </TelemetryCell>
                    <TelemetryCell>
                      <span className="font-medium">{w.name}</span>
                      <p className="text-micro text-muted-foreground">{[w.cluster, w.namespace, w.service].filter(Boolean).join(" · ")}</p>
                    </TelemetryCell>
                    <TelemetryCell>
                      <Badge variant="secondary">{w.kind}</Badge>
                    </TelemetryCell>
                    <TelemetryCell numeric>{`${w.ready}/${w.desired}`}</TelemetryCell>
                    <TelemetryCell numeric>{w.restarts}</TelemetryCell>
                    <TelemetryCell numeric>{formatCores(w.cpu, locale)}</TelemetryCell>
                    <TelemetryCell numeric>{formatBytes(w.memoryBytes, locale)}</TelemetryCell>
                  </TelemetryRow>
                ))}
              </TelemetryTable>
            )}
          </TelemetryQueryView>
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
  );
}
