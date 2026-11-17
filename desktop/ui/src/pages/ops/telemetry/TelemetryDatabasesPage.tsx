import { useMemo } from "react";
import { DetailFacts, DetailPanel, DetailSection } from "@/components/operations/console/DetailPanel";
import { HealthMark, InfraHealthMark } from "@/components/operations/telemetry/HealthMark";
import { TelemetryQueryView } from "@/components/operations/telemetry/TelemetryQueryView";
import { TelemetryCell, TelemetryRow, TelemetryTable } from "@/components/operations/telemetry/TelemetryTable";
import { TelemetryToolbar } from "@/components/operations/telemetry/TelemetryToolbar";
import { UsageMeter } from "@/components/operations/telemetry/UsageMeter";
import { Badge } from "@/components/ui/badge";
import { Notice } from "@/components/ui/notice";
import { useI18n } from "@/hooks/useI18n";
import { useTelemetryQuery } from "@/hooks/useTelemetryQuery";
import { useTelemetryScope, type TelemetryScope } from "@/hooks/useTelemetryScope";
import { useUrlParam } from "@/hooks/useUrlParam";
import { intlLocale } from "@/lib/languages";
import { formatCount, formatMs, formatRatio } from "@/lib/telemetryHub";
import { statusRank, type TelemetryDatabase, type TelemetryDbQuery } from "@/lib/telemetryViews";

const pct = (n: number) => `${Math.round(n * 100)}%`;

function DatabasePanel({ db, scope, onClose }: { db: TelemetryDatabase; scope: TelemetryScope; onClose: () => void }) {
  const { t, lang } = useI18n();
  const v = (key: string, params?: Record<string, string | number>) => t(`telemetryViews.databases.${key}`, params);
  const locale = intlLocale(lang);
  const queries = useTelemetryQuery<TelemetryDbQuery[]>(`infra/databases/${encodeURIComponent(db.id)}/queries`, { range: scope.range, env: scope.env }, { intervalMs: 0 });
  return (
    <DetailPanel open onOpenChange={(open) => !open && onClose()} title={db.name} description={[db.engine, db.version, db.instanceClass].filter(Boolean).join(" · ")} badges={<HealthMark health={db.status} showLabel />}>
      {db.problem && (
        <Notice variant="warning" title={db.problem.title}>
          <p>{db.problem.detail}</p>
          {db.problem.fix && <p className="mt-1">{db.problem.fix}</p>}
        </Notice>
      )}
      <DetailFacts
        items={[
          { label: v("facts.host"), value: <span className="font-mono">{db.port ? `${db.host}:${db.port}` : db.host || "-"}</span> },
          { label: v("facts.environment"), value: db.env || "-" },
          { label: v("facts.source"), value: db.source || "-" },
          { label: v("columns.p95"), value: v("previous", { now: formatMs(db.p95, locale), before: formatMs(db.prevP95, locale) }) },
          { label: v("facts.ops"), value: formatCount(db.opsPerSec, locale) },
          { label: v("facts.slow"), value: formatCount(db.slowPerMin, locale) },
          { label: v("facts.hitRatio"), value: formatRatio(db.hitRatio, locale) },
          { label: v("facts.disk"), value: db.diskGb ? `${pct(db.disk)} · ${formatCount(db.diskGb, locale)} GB` : pct(db.disk) },
        ]}
      />
      {(db.nodes ?? []).length > 0 && (
        <DetailSection title={v("nodes")}>
          <DetailFacts
            items={(db.nodes ?? []).map((n) => ({
              label: `${n.id} (${n.role})`,
              value: v("nodeLine", { cpu: pct(n.cpu), memory: pct(n.memory), connections: n.connections, lag: n.replLagMs ? formatMs(n.replLagMs, locale) : "-" }),
            }))}
          />
        </DetailSection>
      )}
      <DetailSection title={v("queries")}>
        <TelemetryQueryView query={queries} isEmpty={(d) => d.length === 0} emptyTitle={v("noQueries")} skeletonHeight="h-24">
          {(rows) => (
            <ul className="space-y-2">
              {rows.slice(0, 20).map((q) => (
                <li key={q.id} className="rounded-md bg-muted/40 p-2 text-caption">
                  <code className="line-clamp-3 font-mono text-micro break-all">{q.text}</code>
                  <p className="mt-1 text-micro text-muted-foreground">
                    {v("queryLine", { endpoint: [q.serviceId, q.endpoint].filter(Boolean).join(" "), avg: formatMs(q.avgMs, locale), p95: formatMs(q.p95, locale), calls: formatCount(q.callsPerMin, locale), share: formatRatio(q.share, locale) })}
                  </p>
                </li>
              ))}
            </ul>
          )}
        </TelemetryQueryView>
      </DetailSection>
    </DetailPanel>
  );
}

/** `/operations/telemetry/infra/databases`: registered and observed databases
 * with load, connections and latency; a row opens its nodes and top queries. */
export default function TelemetryDatabasesPage() {
  const { t, lang } = useI18n();
  const v = (key: string, params?: Record<string, string | number>) => t(`telemetryViews.databases.${key}`, params);
  const locale = intlLocale(lang);
  const scope = useTelemetryScope();
  const [open, setOpen] = useUrlParam("db");
  const read = useTelemetryQuery<TelemetryDatabase[]>("infra/databases", { range: scope.range, env: scope.env });
  const rows = useMemo(() => [...(read.data ?? [])].sort((a, b) => statusRank(a.status) - statusRank(b.status) || a.name.localeCompare(b.name)), [read.data]);
  const selected = rows.find((d) => d.id === open);

  return (
    <div className="space-y-4">
      <TelemetryToolbar />
      <TelemetryQueryView query={read} isEmpty={(d) => d.length === 0} emptyTitle={v("empty")} emptyDescription={v("emptyHint")}>
        {() => (
          <TelemetryTable
            label={v("listLabel")}
            columns={[
              { key: "health", label: v("columns.health"), className: "w-12" },
              { key: "name", label: v("columns.database") },
              { key: "cpu", label: v("columns.cpu"), className: "w-28" },
              { key: "connections", label: v("columns.connections"), className: "w-36" },
              { key: "p95", label: v("columns.p95"), numeric: true },
              { key: "slow", label: v("columns.slow"), numeric: true },
              { key: "ops", label: v("columns.ops"), numeric: true },
            ]}
          >
            {rows.map((d) => (
              <TelemetryRow key={d.id} onOpen={() => setOpen(d.id)} selected={d.id === open}>
                <TelemetryCell>
                  <InfraHealthMark status={d.noData || d.missing ? "stale" : d.status} />
                </TelemetryCell>
                <TelemetryCell>
                  <span className="font-medium">{d.name}</span>
                  {d.observed && (
                    <Badge variant="outline" className="ml-2">
                      {v("observed")}
                    </Badge>
                  )}
                  <p className="text-micro text-muted-foreground">{[d.engine, d.version, d.env].filter(Boolean).join(" · ")}</p>
                </TelemetryCell>
                <TelemetryCell>
                  <UsageMeter used={d.cpu} limit={1} label={v("columns.cpu")} caption={pct(d.cpu)} />
                </TelemetryCell>
                <TelemetryCell>
                  <UsageMeter used={d.connections} limit={d.maxConnections} label={v("columns.connections")} caption={d.maxConnections > 0 ? `${d.connections}/${d.maxConnections}` : String(d.connections)} />
                </TelemetryCell>
                <TelemetryCell numeric>{formatMs(d.p95, locale)}</TelemetryCell>
                <TelemetryCell numeric>{formatCount(d.slowPerMin, locale)}</TelemetryCell>
                <TelemetryCell numeric>{formatCount(d.opsPerSec, locale)}</TelemetryCell>
              </TelemetryRow>
            ))}
          </TelemetryTable>
        )}
      </TelemetryQueryView>
      {selected && <DatabasePanel db={selected} scope={scope} onClose={() => setOpen(null)} />}
    </div>
  );
}
