import { useMemo } from "react";
import { HealthMark } from "@/components/operations/telemetry/HealthMark";
import { TelemetryQueryView } from "@/components/operations/telemetry/TelemetryQueryView";
import { TelemetryCell, TelemetryRow, TelemetryTable } from "@/components/operations/telemetry/TelemetryTable";
import { Badge } from "@/components/ui/badge";
import { useI18n } from "@/hooks/useI18n";
import { useTelemetryQuery } from "@/hooks/useTelemetryQuery";
import { useTelemetryScope } from "@/hooks/useTelemetryScope";
import { intlLocale } from "@/lib/languages";
import { formatRatio, type TelemetryKpi } from "@/lib/telemetryHub";
import { cn } from "@/lib/utils";

const RANK: Record<string, number> = { critical: 0, warning: 1, ok: 2 };

/** `/operations/telemetry/slo`: the product's KPIs: the share of good events
 * against the objective, the error budget left and how fast it burns. */
export default function TelemetrySloPage() {
  const { t, lang } = useI18n();
  const h = (key: string, params?: Record<string, string | number>) => t(`telemetryHub.slo.${key}`, params);
  const locale = intlLocale(lang);
  useTelemetryScope({ time: false });
  const read = useTelemetryQuery<TelemetryKpi[]>("kpis");
  const rows = useMemo(() => [...(read.data ?? [])].sort((a, b) => (RANK[a.status] ?? 3) - (RANK[b.status] ?? 3) || a.name.localeCompare(b.name)), [read.data]);
  const burn = (n: number) => new Intl.NumberFormat(locale, { maximumFractionDigits: 1 }).format(n);

  return (
    <div className="space-y-4">
      <TelemetryQueryView query={read} isEmpty={(d) => d.length === 0} emptyTitle={h("empty")} emptyDescription={h("emptyHint")}>
        {() => (
          <TelemetryTable
            label={h("listLabel")}
            columns={[
              { key: "health", label: h("columns.health"), className: "w-12" },
              { key: "name", label: h("columns.objective") },
              { key: "target", label: h("columns.target") },
              { key: "current", label: h("columns.current"), numeric: true },
              { key: "budget", label: h("columns.budget"), className: "w-48" },
              { key: "burn", label: h("columns.burn"), numeric: true },
            ]}
          >
            {rows.map((k) => {
              const budget = Math.max(0, Math.min(1, k.budgetRemaining));
              return (
                <TelemetryRow key={k.id}>
                  <TelemetryCell>
                    <HealthMark health={k.status} />
                  </TelemetryCell>
                  <TelemetryCell className="max-w-sm">
                    <span className="block truncate font-medium">{k.name}</span>
                    <span className="block truncate text-micro text-muted-foreground">{k.goodWhen}</span>
                  </TelemetryCell>
                  <TelemetryCell>
                    <span className="font-mono">{formatRatio(k.objective, locale)}</span>
                    <Badge variant="outline" className="ml-2">{k.window}</Badge>
                  </TelemetryCell>
                  <TelemetryCell numeric>{k.noData ? h("noData") : formatRatio(k.windowGood, locale)}</TelemetryCell>
                  <TelemetryCell>
                    <div className="h-1.5 w-full overflow-hidden rounded-full bg-muted" role="img" aria-label={h("budgetLabel", { percent: Math.round(budget * 100) })}>
                      <div className={cn("h-full rounded-full", budget < 0.1 ? "bg-destructive" : budget < 0.4 ? "bg-warning" : "bg-success")} style={{ width: `${Math.max(2, budget * 100)}%` }} />
                    </div>
                    <span className="mt-1 block font-mono text-micro text-muted-foreground">{formatRatio(k.budgetRemaining, locale)}</span>
                  </TelemetryCell>
                  <TelemetryCell numeric>{h("burnRates", { short: burn(k.burnRate1h), long: burn(k.burnRate6h) })}</TelemetryCell>
                </TelemetryRow>
              );
            })}
          </TelemetryTable>
        )}
      </TelemetryQueryView>
    </div>
  );
}
