import { useMemo } from "react";
import { TimeSeriesChart } from "@/components/operations/charts/TimeSeriesChart";
import { Card } from "@/components/ui/card";
import { useI18n } from "@/hooks/useI18n";
import type { TelemetryScope } from "@/hooks/useTelemetryScope";
import { seriesFromSet, type TelemetrySeriesSet } from "@/lib/telemetryHub";

interface SeriesChartCardProps {
  title: string;
  set: TelemetrySeriesSet | undefined | null;
  scope: Pick<TelemetryScope, "from" | "to">;
  loading?: boolean;
  /** Rows of the set to draw; every row when unset. */
  ids?: string[];
  /** Applied to every value before it is drawn (a 0..1 share to a percent). */
  scale?: number;
  unit?: string;
  formatValue?: (v: number) => string;
  labels?: Record<string, string>;
  syncKey?: string;
  area?: boolean;
}

/** One series set of the product, already read with its page, as a chart card. */
export function SeriesChartCard({ title, set, scope, loading = false, ids, scale = 1, unit, formatValue, labels, syncKey, area = false }: SeriesChartCardProps) {
  const { t } = useI18n();
  const series = useMemo(
    () =>
      seriesFromSet(set ?? undefined, ids).map((s, i) => ({
        ...s,
        label: labels?.[s.id] ?? s.label ?? s.id,
        colorIndex: i,
        points: scale === 1 ? s.points : s.points.map(([x, y]): [number, number | null] => [x, y === null ? null : y * scale]),
      })),
    [set, ids, scale, labels],
  );
  return (
    <Card className="min-w-0 p-4">
      <h3 className="mb-2 text-body font-medium">{title}</h3>
      <TimeSeriesChart
        series={series}
        ariaLabel={title}
        unit={unit}
        formatValue={formatValue}
        loading={loading}
        emptyLabel={t("telemetryHub.charts.empty")}
        xRange={series[0]?.points.length ? undefined : [scope.from, scope.to]}
        syncKey={syncKey}
        area={area}
        height={180}
      />
    </Card>
  );
}
