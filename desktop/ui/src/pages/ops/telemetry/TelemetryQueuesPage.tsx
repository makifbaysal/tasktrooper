import { useMemo } from "react";
import { DetailFacts, DetailPanel, DetailSection } from "@/components/operations/console/DetailPanel";
import { HealthMark, InfraHealthMark } from "@/components/operations/telemetry/HealthMark";
import { SeriesChartCard } from "@/components/operations/telemetry/SeriesChartCard";
import { TelemetryQueryView } from "@/components/operations/telemetry/TelemetryQueryView";
import { TelemetryCell, TelemetryRow, TelemetryTable } from "@/components/operations/telemetry/TelemetryTable";
import { TelemetryToolbar } from "@/components/operations/telemetry/TelemetryToolbar";
import { Badge } from "@/components/ui/badge";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { useI18n } from "@/hooks/useI18n";
import { useTelemetryQuery } from "@/hooks/useTelemetryQuery";
import { useTelemetryScope, type TelemetryScope } from "@/hooks/useTelemetryScope";
import { useUrlParam } from "@/hooks/useUrlParam";
import { intlLocale } from "@/lib/languages";
import { formatCount, type TelemetrySeriesSet } from "@/lib/telemetryHub";
import { formatDuration, statusRank, type TelemetryBrokerCluster, type TelemetryConsumerGroup, type TelemetryTopic } from "@/lib/telemetryViews";

const TABS = ["groups", "topics", "clusters"] as const;
type Tab = (typeof TABS)[number];

const groupKey = (g: Pick<TelemetryConsumerGroup, "clusterId" | "id">) => `${g.clusterId}/${g.id}`;

function GroupPanel({ group, cluster, scope, onClose }: { group: TelemetryConsumerGroup; cluster?: string; scope: TelemetryScope; onClose: () => void }) {
  const { t, lang } = useI18n();
  const v = (key: string, params?: Record<string, string | number>) => t(`telemetryViews.queues.${key}`, params);
  const locale = intlLocale(lang);
  const lag = useTelemetryQuery<TelemetrySeriesSet>(
    `infra/clusters/${encodeURIComponent(group.clusterId)}/groups/${encodeURIComponent(group.id)}/lag`,
    { range: scope.range, env: scope.env },
  );
  return (
    <DetailPanel open onOpenChange={(open) => !open && onClose()} title={group.id} description={cluster ?? group.clusterId} badges={<HealthMark health={group.health} showLabel />}>
      <DetailFacts
        items={[
          { label: v("columns.service"), value: group.serviceId || group.process || "-" },
          { label: v("columns.lag"), value: formatCount(group.lag, locale) },
          { label: v("columns.members"), value: group.members },
          { label: v("columns.state"), value: group.state || "-" },
          { label: v("rebalances"), value: formatCount(group.rebalancesPerHour, locale) },
          { label: v("trend"), value: group.trend || "-" },
        ]}
      />
      <SeriesChartCard title={v("lagChart")} set={lag.data} scope={scope} loading={lag.data === undefined && !lag.error} formatValue={(n) => formatCount(n, locale)} />
      {(group.reads ?? []).length > 0 && (
        <DetailSection title={v("reads")}>
          <DetailFacts
            items={(group.reads ?? []).map((r) => ({
              label: r.topic,
              value: v("readLine", {
                lag: formatCount((r.partitionLag ?? []).reduce((s, x) => s + x, 0), locale),
                partitions: (r.partitionLag ?? []).length,
                rate: formatCount(r.consumePerSec, locale),
                age: r.oldestAgeS ? formatDuration(r.oldestAgeS) : "-",
              }),
            }))}
          />
        </DetailSection>
      )}
    </DetailPanel>
  );
}

/** `/operations/telemetry/infra/queues`: Kafka / RabbitMQ / cloud queues:
 * consumer groups by lag, topics by inflow, and the broker clusters. */
export default function TelemetryQueuesPage() {
  const { t, lang } = useI18n();
  const v = (key: string, params?: Record<string, string | number>) => t(`telemetryViews.queues.${key}`, params);
  const locale = intlLocale(lang);
  const scope = useTelemetryScope();
  const [tabParam, setTab] = useUrlParam("tab");
  const [open, setOpen] = useUrlParam("group");
  const tab: Tab = TABS.find((x) => x === tabParam) ?? "groups";
  const clusters = useTelemetryQuery<TelemetryBrokerCluster[]>("infra/clusters");
  const groups = useTelemetryQuery<TelemetryConsumerGroup[]>("infra/groups");
  const topics = useTelemetryQuery<TelemetryTopic[]>("infra/topics", undefined, { enabled: tab === "topics" });
  const clusterName = useMemo(() => new Map((clusters.data ?? []).map((c) => [c.id, c.name])), [clusters.data]);
  const sortedGroups = useMemo(() => [...(groups.data ?? [])].sort((a, b) => statusRank(a.health) - statusRank(b.health) || b.lag - a.lag), [groups.data]);
  const selected = sortedGroups.find((g) => groupKey(g) === open);

  return (
    <div className="space-y-4">
      <TelemetryToolbar env={false} />
      <Tabs value={tab} onValueChange={(x) => setTab(x === "groups" ? null : x)}>
        <TabsList aria-label={v("sections")}>
          {TABS.map((x) => (
            <TabsTrigger key={x} value={x}>
              {v(`tabs.${x}`)}
            </TabsTrigger>
          ))}
        </TabsList>
        <TabsContent value="groups" className="pt-4">
          <TelemetryQueryView query={groups} isEmpty={(d) => d.length === 0} emptyTitle={v("empty")} emptyDescription={v("emptyHint")}>
            {() => (
              <TelemetryTable
                label={v("groupsLabel")}
                columns={[
                  { key: "health", label: v("columns.health"), className: "w-12" },
                  { key: "group", label: v("columns.group") },
                  { key: "service", label: v("columns.service") },
                  { key: "topics", label: v("columns.topics") },
                  { key: "lag", label: v("columns.lag"), numeric: true },
                  { key: "members", label: v("columns.members"), numeric: true },
                  { key: "state", label: v("columns.state") },
                ]}
              >
                {sortedGroups.map((g) => (
                  <TelemetryRow key={groupKey(g)} onOpen={() => setOpen(groupKey(g))} selected={groupKey(g) === open}>
                    <TelemetryCell>
                      <HealthMark health={g.health} />
                    </TelemetryCell>
                    <TelemetryCell>
                      <span className="font-medium">{g.id}</span>
                      <p className="text-micro text-muted-foreground">{clusterName.get(g.clusterId) ?? g.clusterId}</p>
                    </TelemetryCell>
                    <TelemetryCell>{g.serviceId || g.process || "-"}</TelemetryCell>
                    <TelemetryCell className="max-w-xs truncate">{(g.reads ?? []).map((r) => r.topic).join(", ") || "-"}</TelemetryCell>
                    <TelemetryCell numeric>{formatCount(g.lag, locale)}</TelemetryCell>
                    <TelemetryCell numeric>{g.members}</TelemetryCell>
                    <TelemetryCell>{g.state ? <Badge variant="secondary">{g.state}</Badge> : "-"}</TelemetryCell>
                  </TelemetryRow>
                ))}
              </TelemetryTable>
            )}
          </TelemetryQueryView>
        </TabsContent>
        <TabsContent value="topics" className="pt-4">
          <TelemetryQueryView query={topics} isEmpty={(d) => d.length === 0} emptyTitle={v("noTopics")}>
            {(rows) => (
              <TelemetryTable
                label={v("tabs.topics")}
                columns={[
                  { key: "health", label: v("columns.health"), className: "w-12" },
                  { key: "topic", label: v("columns.topic") },
                  { key: "partitions", label: v("columns.partitions"), numeric: true },
                  { key: "in", label: v("columns.inPerSec"), numeric: true },
                  { key: "lag", label: v("columns.lag"), numeric: true },
                  { key: "producers", label: v("columns.producers") },
                  { key: "retention", label: v("columns.retention") },
                ]}
              >
                {[...rows]
                  .sort((a, b) => statusRank(a.health) - statusRank(b.health) || b.inPerSec - a.inPerSec)
                  .map((tp) => (
                    <TelemetryRow key={`${tp.clusterId}/${tp.name}`}>
                      <TelemetryCell>
                        <HealthMark health={tp.health} />
                      </TelemetryCell>
                      <TelemetryCell>
                        <span className="font-medium">{tp.name}</span>
                        <p className="text-micro text-muted-foreground">{clusterName.get(tp.clusterId) ?? tp.clusterId}</p>
                      </TelemetryCell>
                      <TelemetryCell numeric>{tp.partitions}</TelemetryCell>
                      <TelemetryCell numeric>{formatCount(tp.inPerSec, locale)}</TelemetryCell>
                      <TelemetryCell numeric>{formatCount(tp.lag, locale)}</TelemetryCell>
                      <TelemetryCell className="max-w-xs truncate">{(tp.producers ?? []).join(", ") || "-"}</TelemetryCell>
                      <TelemetryCell>{tp.retention || "-"}</TelemetryCell>
                    </TelemetryRow>
                  ))}
              </TelemetryTable>
            )}
          </TelemetryQueryView>
        </TabsContent>
        <TabsContent value="clusters" className="pt-4">
          <TelemetryQueryView query={clusters} isEmpty={(d) => d.length === 0} emptyTitle={v("noClusters")}>
            {(rows) => (
              <TelemetryTable
                label={v("tabs.clusters")}
                columns={[
                  { key: "health", label: v("columns.health"), className: "w-12" },
                  { key: "cluster", label: v("columns.cluster") },
                  { key: "kind", label: v("columns.kind") },
                  { key: "brokers", label: v("columns.brokers"), numeric: true },
                  { key: "topics", label: v("columns.topicCount"), numeric: true },
                  { key: "groups", label: v("columns.groupCount"), numeric: true },
                  { key: "lag", label: v("columns.lag"), numeric: true },
                ]}
              >
                {rows.map((c) => (
                  <TelemetryRow key={c.id}>
                    <TelemetryCell>
                      <InfraHealthMark status={c.missing ? "stale" : c.status} />
                    </TelemetryCell>
                    <TelemetryCell>
                      <span className="font-medium">{c.name}</span>
                      <p className="font-mono text-micro text-muted-foreground">{c.endpoint}</p>
                    </TelemetryCell>
                    <TelemetryCell>
                      <Badge variant="secondary">{[c.kind, c.version].filter(Boolean).join(" ")}</Badge>
                    </TelemetryCell>
                    <TelemetryCell numeric>{(c.nodes ?? []).length}</TelemetryCell>
                    <TelemetryCell numeric>{c.topics}</TelemetryCell>
                    <TelemetryCell numeric>{c.groups}</TelemetryCell>
                    <TelemetryCell numeric>{formatCount(c.totalLag, locale)}</TelemetryCell>
                  </TelemetryRow>
                ))}
              </TelemetryTable>
            )}
          </TelemetryQueryView>
        </TabsContent>
      </Tabs>
      {selected && <GroupPanel group={selected} cluster={clusterName.get(selected.clusterId)} scope={scope} onClose={() => setOpen(null)} />}
    </div>
  );
}
