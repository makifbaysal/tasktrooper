import { Search } from "lucide-react";
import { useMemo } from "react";
import { SeverityBadge, StateBadge } from "@/components/operations/telemetry/TelemetryBadges";
import { TelemetryQueryView } from "@/components/operations/telemetry/TelemetryQueryView";
import { TelemetryCell, TelemetryRow, TelemetryTable } from "@/components/operations/telemetry/TelemetryTable";
import { TelemetryToolbar } from "@/components/operations/telemetry/TelemetryToolbar";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import { useI18n } from "@/hooks/useI18n";
import { useTelemetryQuery } from "@/hooks/useTelemetryQuery";
import { useTelemetryScope } from "@/hooks/useTelemetryScope";
import { useUrlParam } from "@/hooks/useUrlParam";
import { intlLocale } from "@/lib/languages";
import type { TelemetryMonitor } from "@/lib/telemetryHub";

const STATE_RANK: Record<string, number> = { firing: 0, pending: 1, nodata: 2, muted: 3, ok: 4 };

/** `/operations/telemetry/monitors`: the product's alert rules and where each
 * stands now. Creating and editing rules stays in the product. */
export default function TelemetryMonitorsPage() {
  const { t, lang } = useI18n();
  const h = (key: string, params?: Record<string, string | number>) => t(`telemetryHub.monitors.${key}`, params);
  useTelemetryScope({ time: false });
  const [query, setQuery] = useUrlParam("q");
  const time = new Intl.DateTimeFormat(intlLocale(lang), { month: "short", day: "2-digit", hour: "2-digit", minute: "2-digit", hourCycle: "h23" });
  const read = useTelemetryQuery<TelemetryMonitor[]>("monitors");
  const rows = useMemo(() => {
    const needle = query.trim().toLowerCase();
    return (read.data ?? [])
      .filter((m) => !needle || `${m.name} ${m.target} ${m.kind}`.toLowerCase().includes(needle))
      .sort((a, b) => (STATE_RANK[a.state] ?? 5) - (STATE_RANK[b.state] ?? 5) || a.name.localeCompare(b.name));
  }, [read.data, query]);

  return (
    <div className="space-y-4">
      <TelemetryToolbar env={false}>
        <div className="relative w-64 max-w-full">
          <Search className="pointer-events-none absolute top-2.5 left-2.5 h-3.5 w-3.5 text-muted-foreground" aria-hidden />
          <Input className="h-8 pl-8" value={query} onChange={(e) => setQuery(e.target.value)} placeholder={h("search")} aria-label={h("search")} />
        </div>
      </TelemetryToolbar>
      <TelemetryQueryView query={read} isEmpty={(d) => d.length === 0} emptyTitle={h("empty")} emptyDescription={h("emptyHint")}>
        {() =>
          rows.length === 0 ? (
            <p className="py-8 text-center text-caption text-muted-foreground">{h("noMatch")}</p>
          ) : (
            <TelemetryTable
              label={h("listLabel")}
              columns={[
                { key: "state", label: h("columns.state") },
                { key: "name", label: h("columns.monitor") },
                { key: "target", label: h("columns.target") },
                { key: "condition", label: h("columns.condition") },
                { key: "severity", label: h("columns.severity") },
                { key: "changed", label: h("columns.changed") },
              ]}
            >
              {rows.map((m) => (
                <TelemetryRow key={m.id}>
                  <TelemetryCell>
                    <StateBadge state={m.state} />
                    {m.mutedUntil ? <Badge variant="secondary" className="ml-1">{h("muted")}</Badge> : null}
                  </TelemetryCell>
                  <TelemetryCell className="max-w-xs">
                    <span className="block truncate font-medium">{m.name}</span>
                    <span className="block text-micro text-muted-foreground">{[m.kind, m.source].filter(Boolean).join(" · ")}</span>
                  </TelemetryCell>
                  <TelemetryCell className="font-mono text-micro">{m.target}</TelemetryCell>
                  <TelemetryCell className="max-w-xs truncate text-muted-foreground">{m.condition}</TelemetryCell>
                  <TelemetryCell>
                    <SeverityBadge severity={m.severity} />
                  </TelemetryCell>
                  <TelemetryCell className="whitespace-nowrap text-muted-foreground">{m.lastChange ? time.format(m.lastChange) : "-"}</TelemetryCell>
                </TelemetryRow>
              ))}
            </TelemetryTable>
          )
        }
      </TelemetryQueryView>
    </div>
  );
}
