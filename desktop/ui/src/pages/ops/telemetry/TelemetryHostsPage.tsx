import { Search } from "lucide-react";
import { useMemo } from "react";
import { Link } from "react-router-dom";
import { Sparkline } from "@/components/operations/charts/Sparkline";
import { useOpsHref } from "@/components/operations/shell/OpsContext";
import { InfraHealthMark } from "@/components/operations/telemetry/HealthMark";
import { TelemetryQueryView } from "@/components/operations/telemetry/TelemetryQueryView";
import { TelemetryCell, TelemetryRow, TelemetryTable } from "@/components/operations/telemetry/TelemetryTable";
import { TelemetryToolbar } from "@/components/operations/telemetry/TelemetryToolbar";
import { UsageMeter } from "@/components/operations/telemetry/UsageMeter";
import { Input } from "@/components/ui/input";
import { useI18n } from "@/hooks/useI18n";
import { useTelemetryQuery } from "@/hooks/useTelemetryQuery";
import { useTelemetryScope } from "@/hooks/useTelemetryScope";
import { useUrlParam } from "@/hooks/useUrlParam";
import { intlLocale } from "@/lib/languages";
import { formatBytes, formatDuration, seriesFromValues, statusRank, type TelemetryHost } from "@/lib/telemetryViews";

export function hostHref(id: string): string {
  return `/operations/telemetry/infra/hosts/${encodeURIComponent(id)}`;
}

const pct = (n: number) => `${Math.round(n * 100)}%`;

/** `/operations/telemetry/infra/hosts`: every VM and Kubernetes node running
 * the agent's host mode, worst first, with CPU, memory and disk. */
export default function TelemetryHostsPage() {
  const { t, lang } = useI18n();
  const v = (key: string, params?: Record<string, string | number>) => t(`telemetryViews.hosts.${key}`, params);
  const locale = intlLocale(lang);
  const href = useOpsHref();
  const scope = useTelemetryScope();
  const [query, setQuery] = useUrlParam("q");
  const read = useTelemetryQuery<TelemetryHost[]>("infra/hosts", { range: scope.range, q: query.trim() || undefined });
  const rows = useMemo(() => [...(read.data ?? [])].sort((a, b) => statusRank(a.status) - statusRank(b.status) || b.cpu - a.cpu), [read.data]);

  return (
    <div className="space-y-4">
      <TelemetryToolbar env={false}>
        <div className="relative w-64 max-w-full">
          <Search className="pointer-events-none absolute top-2.5 left-2.5 h-3.5 w-3.5 text-muted-foreground" aria-hidden />
          <Input className="h-8 pl-8" value={query} onChange={(e) => setQuery(e.target.value)} placeholder={v("search")} aria-label={v("search")} />
        </div>
      </TelemetryToolbar>
      <TelemetryQueryView query={read} isEmpty={(d) => d.length === 0 && !query} emptyTitle={v("empty")} emptyDescription={v("emptyHint")}>
        {() =>
          rows.length === 0 ? (
            <p className="py-8 text-center text-caption text-muted-foreground">{v("noMatch")}</p>
          ) : (
            <TelemetryTable
              label={v("listLabel")}
              columns={[
                { key: "health", label: v("columns.health"), className: "w-12" },
                { key: "host", label: v("columns.host") },
                { key: "cpu", label: v("columns.cpu"), className: "w-36" },
                { key: "trend", label: v("columns.trend") },
                { key: "memory", label: v("columns.memory"), className: "w-36" },
                { key: "disk", label: v("columns.disk"), className: "w-28" },
                { key: "containers", label: v("columns.containers"), numeric: true },
                { key: "uptime", label: v("columns.uptime"), numeric: true },
              ]}
            >
              {rows.map((h) => (
                <TelemetryRow key={h.id}>
                  <TelemetryCell>
                    <InfraHealthMark status={h.status} />
                  </TelemetryCell>
                  <TelemetryCell>
                    <Link to={href(hostHref(h.id))} className="font-medium hover:underline">
                      {h.id}
                    </Link>
                    <p className="text-micro text-muted-foreground">{[h.os, h.arch, h.cluster && v("inCluster", { cluster: h.cluster })].filter(Boolean).join(" · ")}</p>
                  </TelemetryCell>
                  <TelemetryCell>
                    <UsageMeter used={h.cpu} limit={1} label={v("columns.cpu")} caption={v("cpuCaption", { percent: pct(h.cpu), cores: h.cpuCores })} />
                  </TelemetryCell>
                  <TelemetryCell>
                    <Sparkline points={seriesFromValues(h.spark?.cpu)} ariaLabel={v("trendLabel", { name: h.id })} />
                  </TelemetryCell>
                  <TelemetryCell>
                    <UsageMeter used={h.memory} limit={1} label={v("columns.memory")} caption={`${pct(h.memory)} · ${formatBytes(h.memoryBytes, locale)}`} />
                  </TelemetryCell>
                  <TelemetryCell>
                    <UsageMeter used={h.disk} limit={1} label={v("columns.disk")} caption={pct(h.disk)} />
                  </TelemetryCell>
                  <TelemetryCell numeric>{h.containers}</TelemetryCell>
                  <TelemetryCell numeric>{formatDuration(h.uptimeS)}</TelemetryCell>
                </TelemetryRow>
              ))}
            </TelemetryTable>
          )
        }
      </TelemetryQueryView>
    </div>
  );
}
