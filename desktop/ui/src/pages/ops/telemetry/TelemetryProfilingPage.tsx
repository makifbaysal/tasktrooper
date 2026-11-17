import { Flame } from "lucide-react";
import { useMemo } from "react";
import { FlameGraph } from "@/components/operations/telemetry/FlameGraph";
import { SeriesChartCard } from "@/components/operations/telemetry/SeriesChartCard";
import { ServicePicker } from "@/components/operations/telemetry/ServicePicker";
import { TelemetryQueryView } from "@/components/operations/telemetry/TelemetryQueryView";
import { TelemetryCell, TelemetryRow, TelemetryTable } from "@/components/operations/telemetry/TelemetryTable";
import { TelemetryToolbar } from "@/components/operations/telemetry/TelemetryToolbar";
import { EmptyState } from "@/components/ui/empty-state";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { useI18n } from "@/hooks/useI18n";
import { useTelemetryQuery } from "@/hooks/useTelemetryQuery";
import { useTelemetryScope } from "@/hooks/useTelemetryScope";
import { useUrlParam } from "@/hooks/useUrlParam";
import { intlLocale } from "@/lib/languages";
import { formatRatio, type TelemetrySeriesSet } from "@/lib/telemetryHub";
import { formatProfileValue, PROFILE_TYPES, type TelemetryFlamegraph, type TelemetryProfileTypes, type TelemetryTopFunctions } from "@/lib/telemetryViews";

/** `/operations/telemetry/profiling`: continuous profiles of one service: the
 * functions that cost the most, and the merged flame graph to see from where. */
export default function TelemetryProfilingPage() {
  const { t, lang } = useI18n();
  const v = (key: string, params?: Record<string, string | number>) => t(`telemetryViews.profiling.${key}`, params);
  const locale = intlLocale(lang);
  const scope = useTelemetryScope();
  const [service, setService] = useUrlParam("service");
  const [typeParam, setType] = useUrlParam("type");
  const q = { service, range: scope.range, env: scope.env };
  const types = useTelemetryQuery<TelemetryProfileTypes>("profiles/types", q, { enabled: !!service });
  const available = useMemo(() => (types.data?.types ?? []).map((x) => x.type), [types.data]);
  const type = available.includes(typeParam) ? typeParam : (available.find((x) => x === "cpu") ?? available[0] ?? "");
  const ready = !!service && !!type;
  const query = { ...q, type };
  const top = useTelemetryQuery<TelemetryTopFunctions>("profiles/top", { ...query, limit: 50 }, { enabled: ready });
  const flame = useTelemetryQuery<TelemetryFlamegraph>("profiles/flamegraph", query, { enabled: ready });
  const series = useTelemetryQuery<TelemetrySeriesSet>("profiles/series", query, { enabled: ready });
  const typeLabel = (x: string) => ((PROFILE_TYPES as readonly string[]).includes(x) ? v(`types.${x}`) : x);
  const unit = types.data?.types?.find((x) => x.type === type)?.unit ?? "";

  return (
    <div className="space-y-4">
      <TelemetryToolbar>
        <ServicePicker value={service} onChange={setService} label={v("service")} />
        {available.length > 0 && (
          <Select value={type} onValueChange={(x) => setType(x)}>
            <SelectTrigger className="h-8 w-52" aria-label={v("type")}>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {available.map((x) => (
                <SelectItem key={x} value={x}>
                  {typeLabel(x)}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        )}
      </TelemetryToolbar>
      {!service ? (
        <EmptyState icon={Flame} title={v("pickService")} description={v("pickServiceHint")} />
      ) : (
        <TelemetryQueryView query={types} isEmpty={(d) => (d.types ?? []).length === 0} emptyTitle={v("empty")} emptyDescription={v("emptyHint")}>
          {() => (
            <div className="space-y-4">
              <SeriesChartCard
                title={type === "cpu" || type === "wall" ? v("coresChart") : v("valueChart", { type: typeLabel(type) })}
                set={series.data}
                scope={scope}
                loading={series.data === undefined && !series.error}
              />
              <TelemetryQueryView query={top} isEmpty={(d) => (d.functions ?? []).length === 0} emptyTitle={v("noFunctions")}>
                {(d) => (
                  <TelemetryTable
                    label={v("topLabel")}
                    columns={[
                      { key: "fn", label: v("columns.function") },
                      { key: "self", label: v("columns.self"), numeric: true },
                      { key: "selfShare", label: v("columns.selfShare"), numeric: true },
                      { key: "total", label: v("columns.total"), numeric: true },
                    ]}
                  >
                    {(d.functions ?? []).map((f) => (
                      <TelemetryRow key={f.name}>
                        <TelemetryCell className="max-w-xl">
                          <span className="block truncate font-mono text-micro" title={f.name}>
                            {f.name}
                          </span>
                        </TelemetryCell>
                        <TelemetryCell numeric>{formatProfileValue(f.self, d.unit || unit, locale)}</TelemetryCell>
                        <TelemetryCell numeric>{formatRatio(d.total ? f.self / d.total : 0, locale)}</TelemetryCell>
                        <TelemetryCell numeric>{formatProfileValue(f.total, d.unit || unit, locale)}</TelemetryCell>
                      </TelemetryRow>
                    ))}
                  </TelemetryTable>
                )}
              </TelemetryQueryView>
              <TelemetryQueryView query={flame} isEmpty={(d) => !d.root || d.root.value === 0} emptyTitle={v("noFunctions")}>
                {(d) => <FlameGraph root={d.root} unit={d.unit || unit} />}
              </TelemetryQueryView>
            </div>
          )}
        </TelemetryQueryView>
      )}
    </div>
  );
}
