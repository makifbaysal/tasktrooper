import { Link, useParams } from "react-router-dom";
import { useOpsHref } from "@/components/operations/shell/OpsContext";
import { InfraHealthMark } from "@/components/operations/telemetry/HealthMark";
import { SeriesChartCard } from "@/components/operations/telemetry/SeriesChartCard";
import { TelemetryBackLink } from "@/components/operations/telemetry/TelemetryBackLink";
import { TelemetryQueryView } from "@/components/operations/telemetry/TelemetryQueryView";
import { TelemetryCell, TelemetryRow, TelemetryTable } from "@/components/operations/telemetry/TelemetryTable";
import { UsageMeter } from "@/components/operations/telemetry/UsageMeter";
import { Badge } from "@/components/ui/badge";
import { StatTile } from "@/components/ui/stat-tile";
import { useI18n } from "@/hooks/useI18n";
import { useTelemetryQuery } from "@/hooks/useTelemetryQuery";
import { useTelemetryScope } from "@/hooks/useTelemetryScope";
import { intlLocale } from "@/lib/languages";
import { formatBytes, formatCores, formatDuration, type TelemetryHostDetail } from "@/lib/telemetryViews";
import { serviceHref } from "@/pages/ops/telemetry/TelemetryServicesPage";

const pct = (n: number) => `${Math.round(n * 100)}%`;

/** `/operations/telemetry/infra/hosts/:host`: one host's usage over the
 * window, its containers, its busiest processes and the services on it. */
export default function TelemetryHostPage() {
  const { host = "" } = useParams();
  const { t, lang } = useI18n();
  const v = (key: string, params?: Record<string, string | number>) => t(`telemetryViews.hosts.${key}`, params);
  const locale = intlLocale(lang);
  const href = useOpsHref();
  const scope = useTelemetryScope();
  const read = useTelemetryQuery<TelemetryHostDetail>(`infra/hosts/${encodeURIComponent(host)}`, { range: scope.range });
  const loading = read.data === undefined && !read.error;

  return (
    <div className="space-y-4">
      <TelemetryBackLink to="/operations/telemetry/infra/hosts" label={v("back")} />
      <TelemetryQueryView query={read}>
        {(h) => (
          <div className="space-y-4">
            <div className="flex flex-wrap items-center gap-2">
              <InfraHealthMark status={h.status} showLabel />
              <h2 className="text-heading font-semibold">{h.id}</h2>
              {h.os && <Badge variant="secondary">{h.os}</Badge>}
              {h.arch && <Badge variant="outline">{h.arch}</Badge>}
              {h.cluster && <Badge variant="outline">{v("inCluster", { cluster: h.cluster })}</Badge>}
            </div>
            <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
              <StatTile label={v("columns.cpu")} value={pct(h.cpu)} foot={v("cores", { cores: h.cpuCores })} />
              <StatTile label={v("columns.memory")} value={pct(h.memory)} foot={formatBytes(h.memoryBytes, locale)} />
              <StatTile label={v("columns.disk")} value={pct(h.disk)} foot={v("load", { value: h.load1.toFixed(2) })} />
              <StatTile label={v("columns.uptime")} value={formatDuration(h.uptimeS)} foot={v("network", { in: formatBytes(h.netIn, locale), out: formatBytes(h.netOut, locale) })} />
            </div>
            <div className="grid gap-4 lg:grid-cols-2">
              <SeriesChartCard title={v("cpuChart")} set={h.series?.cpu} scope={scope} loading={loading} scale={100} unit="%" syncKey="tel:host" />
              <SeriesChartCard title={v("memoryChart")} set={h.series?.memory} scope={scope} loading={loading} scale={100} unit="%" syncKey="tel:host" />
              <SeriesChartCard title={v("diskChart")} set={h.series?.disk} scope={scope} loading={loading} scale={100} unit="%" syncKey="tel:host" />
              <SeriesChartCard
                title={v("networkChart")}
                set={h.series?.network}
                scope={scope}
                loading={loading}
                formatValue={(n) => `${formatBytes(n, locale)}/s`}
                labels={{ in: v("netIn"), out: v("netOut") }}
                syncKey="tel:host"
              />
            </div>
            {(h.services ?? []).length > 0 && (
              <div className="flex flex-wrap items-center gap-2 text-caption">
                <span className="text-muted-foreground">{v("services")}</span>
                {(h.services ?? []).map((s) => (
                  <Link key={s} to={href(serviceHref(s))} className="hover:underline">
                    <Badge variant="outline">{s}</Badge>
                  </Link>
                ))}
              </div>
            )}
            {(h.containerList ?? []).length > 0 && (
              <TelemetryTable
                label={v("containers")}
                columns={[
                  { key: "health", label: v("columns.health"), className: "w-12" },
                  { key: "name", label: v("containerColumns.name") },
                  { key: "cpu", label: v("containerColumns.cpu"), className: "w-40" },
                  { key: "memory", label: v("containerColumns.memory"), className: "w-40" },
                  { key: "restarts", label: v("containerColumns.restarts"), numeric: true },
                ]}
              >
                {(h.containerList ?? []).map((c) => (
                  <TelemetryRow key={c.id}>
                    <TelemetryCell>
                      <InfraHealthMark status={c.status} />
                    </TelemetryCell>
                    <TelemetryCell className="max-w-xs">
                      <span className="block truncate font-medium">{c.name}</span>
                      <span className="block truncate font-mono text-micro text-muted-foreground">{c.image}</span>
                    </TelemetryCell>
                    <TelemetryCell>
                      <UsageMeter used={c.cpu} limit={c.cpuLimit} label={v("containerColumns.cpu")} caption={c.cpuLimit > 0 ? v("ofLimit", { used: formatCores(c.cpu, locale), limit: formatCores(c.cpuLimit, locale) }) : formatCores(c.cpu, locale)} />
                    </TelemetryCell>
                    <TelemetryCell>
                      <UsageMeter
                        used={c.memoryBytes}
                        limit={c.memoryLimit}
                        label={v("containerColumns.memory")}
                        caption={c.memoryLimit > 0 ? v("ofLimit", { used: formatBytes(c.memoryBytes, locale), limit: formatBytes(c.memoryLimit, locale) }) : formatBytes(c.memoryBytes, locale)}
                      />
                    </TelemetryCell>
                    <TelemetryCell numeric>{c.restarts}</TelemetryCell>
                  </TelemetryRow>
                ))}
              </TelemetryTable>
            )}
            {(h.processes ?? []).length > 0 && (
              <TelemetryTable
                label={v("processes")}
                columns={[
                  { key: "pid", label: v("processColumns.pid"), numeric: true, className: "w-20" },
                  { key: "command", label: v("processColumns.command") },
                  { key: "cpu", label: v("processColumns.cpu"), numeric: true },
                  { key: "memory", label: v("processColumns.memory"), numeric: true },
                  { key: "threads", label: v("processColumns.threads"), numeric: true },
                ]}
              >
                {(h.processes ?? []).map((p) => (
                  <TelemetryRow key={p.pid}>
                    <TelemetryCell numeric>{p.pid}</TelemetryCell>
                    <TelemetryCell className="max-w-md">
                      <span className="block truncate font-mono text-micro" title={p.command}>
                        {p.command || p.exe}
                      </span>
                    </TelemetryCell>
                    <TelemetryCell numeric>{formatCores(p.cpu, locale)}</TelemetryCell>
                    <TelemetryCell numeric>{formatBytes(p.memoryBytes, locale)}</TelemetryCell>
                    <TelemetryCell numeric>{p.threads}</TelemetryCell>
                  </TelemetryRow>
                ))}
              </TelemetryTable>
            )}
          </div>
        )}
      </TelemetryQueryView>
    </div>
  );
}
