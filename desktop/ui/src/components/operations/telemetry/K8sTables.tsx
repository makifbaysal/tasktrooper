import { Link } from "react-router-dom";
import { useOpsHref } from "@/components/operations/shell/OpsContext";
import { InfraHealthMark } from "@/components/operations/telemetry/HealthMark";
import { TelemetryCell, TelemetryRow, TelemetryTable } from "@/components/operations/telemetry/TelemetryTable";
import { UsageMeter } from "@/components/operations/telemetry/UsageMeter";
import { Badge } from "@/components/ui/badge";
import { useI18n } from "@/hooks/useI18n";
import { intlLocale } from "@/lib/languages";
import { formatBytes, formatCores, statusRank, type TelemetryK8sEvent, type TelemetryPod } from "@/lib/telemetryViews";

export function k8sPodHref(uid: string): string {
  return `/operations/telemetry/infra/kubernetes/pods/${encodeURIComponent(uid)}`;
}

/** Pods worst first: phase and the reason it is unhappy, restarts, usage against the limit. */
export function PodsTable({ pods }: { pods: TelemetryPod[] }) {
  const { t, lang } = useI18n();
  const v = (key: string, params?: Record<string, string | number>) => t(`telemetryViews.k8s.${key}`, params);
  const locale = intlLocale(lang);
  const href = useOpsHref();
  const rows = [...pods].sort((a, b) => statusRank(a.status) - statusRank(b.status) || b.restartsInRange - a.restartsInRange);
  return (
    <TelemetryTable
      label={v("tabs.pods")}
      columns={[
        { key: "health", label: v("columns.health"), className: "w-12" },
        { key: "name", label: v("columns.pod") },
        { key: "phase", label: v("columns.phase") },
        { key: "restarts", label: v("columns.restarts"), numeric: true },
        { key: "cpu", label: v("columns.cpu"), className: "w-36" },
        { key: "memory", label: v("columns.memory"), className: "w-36" },
      ]}
    >
      {rows.map((p) => {
        const cpuLimit = p.cpuLimit || p.cpuRequest;
        const memLimit = p.memoryLimit || p.memoryRequest;
        return (
          <TelemetryRow key={p.uid}>
            <TelemetryCell>
              <InfraHealthMark status={p.status} />
            </TelemetryCell>
            <TelemetryCell className="max-w-xs">
              <Link to={href(k8sPodHref(p.uid))} className="block truncate font-medium hover:underline">
                {p.name}
              </Link>
              <p className="truncate text-micro text-muted-foreground">{[p.cluster, p.namespace, p.node].filter(Boolean).join(" · ")}</p>
            </TelemetryCell>
            <TelemetryCell>
              <Badge variant={p.reason ? "destructive" : p.phase === "Running" ? "success" : "secondary"}>{p.reason || p.phase}</Badge>
            </TelemetryCell>
            <TelemetryCell numeric>{p.restartsInRange > 0 ? `${p.restartsInRange} (${p.restarts})` : p.restarts}</TelemetryCell>
            <TelemetryCell>
              <UsageMeter used={p.cpu} limit={cpuLimit} label={v("columns.cpu")} caption={cpuLimit > 0 ? v("ofLimit", { used: formatCores(p.cpu, locale), limit: formatCores(cpuLimit, locale) }) : formatCores(p.cpu, locale)} />
            </TelemetryCell>
            <TelemetryCell>
              <UsageMeter
                used={p.memoryBytes}
                limit={memLimit}
                label={v("columns.memory")}
                caption={memLimit > 0 ? v("ofLimit", { used: formatBytes(p.memoryBytes, locale), limit: formatBytes(memLimit, locale) }) : formatBytes(p.memoryBytes, locale)}
              />
            </TelemetryCell>
          </TelemetryRow>
        );
      })}
    </TelemetryTable>
  );
}

/** Kubernetes Warning events, newest first. */
export function K8sEventsTable({ events }: { events: TelemetryK8sEvent[] }) {
  const { t, lang } = useI18n();
  const v = (key: string) => t(`telemetryViews.k8s.${key}`);
  const time = new Intl.DateTimeFormat(intlLocale(lang), { dateStyle: "short", timeStyle: "medium" });
  return (
    <TelemetryTable
      label={v("tabs.events")}
      columns={[
        { key: "time", label: v("eventColumns.time"), className: "w-40" },
        { key: "reason", label: v("eventColumns.reason") },
        { key: "object", label: v("eventColumns.object") },
        { key: "message", label: v("eventColumns.message") },
        { key: "count", label: v("eventColumns.count"), numeric: true },
      ]}
    >
      {[...events]
        .sort((a, b) => b.ts - a.ts)
        .map((e, i) => (
          <TelemetryRow key={`${e.ts}:${e.objectName}:${i}`}>
            <TelemetryCell className="font-mono text-micro whitespace-nowrap">{time.format(e.ts)}</TelemetryCell>
            <TelemetryCell>
              <Badge variant={e.type === "Warning" ? "warning" : "secondary"}>{e.reason}</Badge>
            </TelemetryCell>
            <TelemetryCell className="max-w-xs">
              <span className="block truncate">{`${e.objectKind}/${e.objectName}`}</span>
              <span className="block truncate text-micro text-muted-foreground">{[e.cluster, e.namespace].filter(Boolean).join(" · ")}</span>
            </TelemetryCell>
            <TelemetryCell className="max-w-md">
              <span className="line-clamp-2 break-words">{e.message}</span>
            </TelemetryCell>
            <TelemetryCell numeric>{e.count}</TelemetryCell>
          </TelemetryRow>
        ))}
    </TelemetryTable>
  );
}
