import { useMemo } from "react";
import { TimeSeriesChart, type DeployMarker } from "@/components/operations/charts/TimeSeriesChart";
import { Card } from "@/components/ui/card";
import { useI18n } from "@/hooks/useI18n";
import { useTelemetryQuery } from "@/hooks/useTelemetryQuery";
import { intlLocale } from "@/lib/languages";
import { formatMs, seriesFromSet, type TelemetryDeploy, type TelemetrySeriesSet } from "@/lib/telemetryHub";
import type { TelemetryScope } from "@/hooks/useTelemetryScope";

interface RedChartsProps {
  /** The product's series path, below /api/v1. */
  path: string;
  scope: TelemetryScope;
  deploys?: TelemetryDeploy[];
  /** Rows of the series set to chart as latency lines. */
  latencyIds?: string[];
}

function ChartCard({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <Card className="min-w-0 p-4">
      <h3 className="mb-2 text-body font-medium">{title}</h3>
      {children}
    </Card>
  );
}

/** Throughput, error rate and latency of one service or endpoint. */
export function RedCharts({ path, scope, deploys, latencyIds = ["p50", "p95", "p99"] }: RedChartsProps) {
  const { t, lang } = useI18n();
  const locale = intlLocale(lang);
  const query = useTelemetryQuery<TelemetrySeriesSet>(path, { range: scope.range, env: scope.env });
  const set = query.data;

  const traffic = useMemo(() => seriesFromSet(set, ["rpm"]).map((s) => ({ ...s, label: t("telemetryHub.charts.rpm") })), [set, t]);
  const errors = useMemo(
    () =>
      seriesFromSet(set, ["errorRate"]).map((s) => ({
        ...s,
        label: t("telemetryHub.charts.errorRate"),
        colorIndex: 8,
        points: s.points.map(([x, y]): [number, number | null] => [x, y === null ? null : y * 100]),
      })),
    [set, t],
  );
  const latency = useMemo(() => seriesFromSet(set, latencyIds).map((s, i) => ({ ...s, colorIndex: i + 1 })), [set, latencyIds]);
  const markers = useMemo<DeployMarker[]>(
    () => (deploys ?? []).filter((d) => d.ts >= scope.from && d.ts <= scope.to).map((d) => ({ at: d.ts, label: d.version })),
    [deploys, scope.from, scope.to],
  );
  const xRange: [number, number] | undefined = traffic[0] ? undefined : [scope.from, scope.to];
  const loading = query.data === undefined && !query.error;
  const empty = query.error ? query.error : t("telemetryHub.charts.empty");
  const common = { loading, emptyLabel: empty, deployMarkers: markers, xRange, syncKey: `tel:${path}`, area: false, height: 180 } as const;

  return (
    <div className="grid gap-4 @container lg:grid-cols-3">
      <ChartCard title={t("telemetryHub.charts.throughput")}>
        <TimeSeriesChart {...common} series={traffic} ariaLabel={t("telemetryHub.charts.throughput")} unit={t("telemetryHub.charts.rpmUnit")} />
      </ChartCard>
      <ChartCard title={t("telemetryHub.charts.errors")}>
        <TimeSeriesChart {...common} series={errors} ariaLabel={t("telemetryHub.charts.errors")} unit="%" />
      </ChartCard>
      <ChartCard title={t("telemetryHub.charts.latency")}>
        <TimeSeriesChart {...common} series={latency} ariaLabel={t("telemetryHub.charts.latency")} formatValue={(v) => formatMs(v, locale)} />
      </ChartCard>
    </div>
  );
}
