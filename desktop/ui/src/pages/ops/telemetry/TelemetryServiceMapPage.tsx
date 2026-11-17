import { ServiceMapGraph } from "@/components/operations/telemetry/ServiceMapGraph";
import { TelemetryQueryView } from "@/components/operations/telemetry/TelemetryQueryView";
import { TelemetryCell, TelemetryRow, TelemetryTable } from "@/components/operations/telemetry/TelemetryTable";
import { TelemetryToolbar } from "@/components/operations/telemetry/TelemetryToolbar";
import { HealthMark } from "@/components/operations/telemetry/HealthMark";
import { useI18n } from "@/hooks/useI18n";
import { useTelemetryQuery } from "@/hooks/useTelemetryQuery";
import { useTelemetryScope } from "@/hooks/useTelemetryScope";
import { intlLocale } from "@/lib/languages";
import { formatCount, formatMs, formatRatio } from "@/lib/telemetryHub";
import type { TelemetryServiceMap } from "@/lib/telemetryViews";
import { serviceHref } from "@/pages/ops/telemetry/TelemetryServicesPage";

/** `/operations/telemetry/map`: who calls whom in the window, with each
 * call's rate, errors and p95; the same edges as a table under the graph. */
export default function TelemetryServiceMapPage() {
  const { t, lang } = useI18n();
  const v = (key: string) => t(`telemetryViews.map.${key}`);
  const locale = intlLocale(lang);
  const scope = useTelemetryScope();
  const read = useTelemetryQuery<TelemetryServiceMap>("service-map", { range: scope.range, env: scope.env });

  return (
    <div className="space-y-4">
      <TelemetryToolbar />
      <TelemetryQueryView query={read} isEmpty={(d) => (d.nodes ?? []).length === 0} emptyTitle={v("empty")} emptyDescription={v("emptyHint")} skeletonHeight="h-96">
        {(map) => {
          const names = new Map((map.nodes ?? []).map((n) => [n.id, n.name]));
          const edges = [...(map.edges ?? [])].sort((a, b) => b.callsPerMin - a.callsPerMin);
          return (
            <div className="space-y-4">
              <ServiceMapGraph map={map} serviceHref={serviceHref} />
              {edges.length > 0 && (
                <TelemetryTable
                  label={v("callsLabel")}
                  columns={[
                    { key: "health", label: v("columns.health"), className: "w-12" },
                    { key: "from", label: v("columns.from") },
                    { key: "to", label: v("columns.to") },
                    { key: "calls", label: v("columns.calls"), numeric: true },
                    { key: "err", label: v("columns.errorRate"), numeric: true },
                    { key: "p95", label: v("columns.p95"), numeric: true },
                  ]}
                >
                  {edges.map((e) => (
                    <TelemetryRow key={e.id}>
                      <TelemetryCell>
                        <HealthMark health={e.status} />
                      </TelemetryCell>
                      <TelemetryCell>{names.get(e.from) ?? e.from}</TelemetryCell>
                      <TelemetryCell>{names.get(e.to) ?? e.to}</TelemetryCell>
                      <TelemetryCell numeric>{formatCount(e.callsPerMin, locale)}</TelemetryCell>
                      <TelemetryCell numeric>{formatRatio(e.errorRate, locale)}</TelemetryCell>
                      <TelemetryCell numeric>{formatMs(e.p95, locale)}</TelemetryCell>
                    </TelemetryRow>
                  ))}
                </TelemetryTable>
              )}
            </div>
          );
        }}
      </TelemetryQueryView>
    </div>
  );
}
