import { useEffect, useMemo, useState } from "react";
import { useParams } from "react-router-dom";
import { TimeSeriesChart } from "@/components/operations/charts/TimeSeriesChart";
import { LoadRunBadge } from "@/components/operations/telemetry/LoadRunBadge";
import { TelemetryBackLink } from "@/components/operations/telemetry/TelemetryBackLink";
import { TelemetryQueryView } from "@/components/operations/telemetry/TelemetryQueryView";
import { TelemetryCell, TelemetryRow, TelemetryTable } from "@/components/operations/telemetry/TelemetryTable";
import { Badge } from "@/components/ui/badge";
import { Card } from "@/components/ui/card";
import { Notice } from "@/components/ui/notice";
import { Progress } from "@/components/ui/progress";
import { StatTile } from "@/components/ui/stat-tile";
import { useI18n } from "@/hooks/useI18n";
import { useTelemetryQuery } from "@/hooks/useTelemetryQuery";
import { useTelemetryScope } from "@/hooks/useTelemetryScope";
import { intlLocale } from "@/lib/languages";
import { formatCount, formatMs, formatRatio } from "@/lib/telemetryHub";
import { formatDuration, isLiveRun, runSeriesPoints, type TelemetryLoadRun, type TelemetryLoadScenario, type TelemetryRunSeries } from "@/lib/telemetryViews";

function RunChart({ title, points, formatValue, unit, colorIndex }: { title: string; points: Array<[number, number | null]>; formatValue?: (n: number) => string; unit?: string; colorIndex: number }) {
  const { t } = useI18n();
  return (
    <Card className="min-w-0 p-4">
      <h3 className="mb-2 text-body font-medium">{title}</h3>
      <TimeSeriesChart
        series={[{ id: title, label: title, points, colorIndex }]}
        ariaLabel={title}
        formatValue={formatValue}
        unit={unit}
        emptyLabel={t("telemetryHub.charts.empty")}
        syncKey="tel:loadrun"
        area={false}
        height={160}
      />
    </Card>
  );
}

/** `/operations/telemetry/load-tests/:id`: one run's load, throughput,
 * latency and errors over its timeline, thresholds and per-step results. */
export default function TelemetryLoadTestRunPage() {
  const { id = "" } = useParams();
  const { t, lang } = useI18n();
  const v = (key: string, params?: Record<string, string | number>) => t(`telemetryViews.loadTests.${key}`, params);
  const locale = intlLocale(lang);
  useTelemetryScope({ time: false });
  const base = `loadtests/runs/${encodeURIComponent(id)}`;
  const [live, setLive] = useState(false);
  const run = useTelemetryQuery<TelemetryLoadRun>(base, undefined, { intervalMs: live ? 5_000 : 30_000 });
  useEffect(() => setLive(run.data ? isLiveRun(run.data.state) : false), [run.data]);
  const series = useTelemetryQuery<TelemetryRunSeries>(`${base}/series`, undefined, { enabled: !!run.data, intervalMs: live ? 5_000 : 0 });
  const scenarios = useTelemetryQuery<TelemetryLoadScenario[]>("loadtests/scenarios", undefined, { intervalMs: 0 });
  const scenario = scenarios.data?.find((s) => s.id === run.data?.scenarioId);
  const time = new Intl.DateTimeFormat(intlLocale(lang), { dateStyle: "medium", timeStyle: "medium" });
  const points = useMemo(
    () => ({
      vus: runSeriesPoints(series.data, "vus"),
      rps: runSeriesPoints(series.data, "rps"),
      p95: runSeriesPoints(series.data, "p95"),
      errorRate: runSeriesPoints(series.data, "errorRate", 100),
    }),
    [series.data],
  );

  return (
    <div className="space-y-4">
      <TelemetryBackLink to="/operations/telemetry/load-tests" label={v("back")} />
      <TelemetryQueryView query={run}>
        {(r) => {
          const codes = Object.entries(r.statusCodes ?? {}).sort((a, b) => b[1] - a[1]);
          return (
            <div className="space-y-4">
              <div className="flex flex-wrap items-center gap-2">
                <LoadRunBadge state={r.state} />
                <h2 className="text-heading font-semibold">{scenario?.name ?? r.scenarioId}</h2>
                {r.region && <Badge variant="outline">{r.region}</Badge>}
                <span className="text-caption text-muted-foreground">
                  {time.format(r.startedAt)}
                  {r.finishedAt ? ` · ${formatDuration((r.finishedAt - r.startedAt) / 1000)}` : ""}
                </span>
              </div>
              {r.state === "running" && <Progress value={r.progress * 100} label={v("progress")} />}
              {r.error && (
                <Notice variant="error" title={v("runError")}>
                  {r.error}
                </Notice>
              )}
              <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
                <StatTile label={v("columns.vus")} value={formatCount(r.vus, locale)} />
                <StatTile label={v("columns.rps")} value={formatCount(r.rps, locale)} />
                <StatTile label={v("columns.p95")} value={formatMs(r.p95, locale)} />
                <StatTile label={v("columns.errorRate")} value={formatRatio(r.errorRate, locale)} tone={r.errorRate >= 0.01 ? "warning" : "default"} />
              </div>
              <div className="grid gap-4 lg:grid-cols-2">
                <RunChart title={v("charts.vus")} points={points.vus} colorIndex={0} />
                <RunChart title={v("charts.rps")} points={points.rps} colorIndex={1} />
                <RunChart title={v("charts.p95")} points={points.p95} colorIndex={2} formatValue={(n) => formatMs(n, locale)} />
                <RunChart title={v("charts.errorRate")} points={points.errorRate} colorIndex={8} unit="%" />
              </div>
              {(r.thresholds ?? []).length > 0 && (
                <Card className="p-4">
                  <h3 className="mb-2 text-body font-medium">{v("columns.thresholds")}</h3>
                  <ul className="space-y-1 text-caption">
                    {(r.thresholds ?? []).map((th) => (
                      <li key={th.name} className="flex items-center gap-2">
                        <Badge variant={th.passed ? "success" : "destructive"}>{th.passed ? v("passed") : v("failed")}</Badge>
                        <span className="font-mono text-micro">{th.name}</span>
                      </li>
                    ))}
                  </ul>
                </Card>
              )}
              {(r.steps ?? []).length > 0 && (
                <TelemetryTable
                  label={v("steps")}
                  columns={[
                    { key: "step", label: v("stepColumns.step") },
                    { key: "count", label: v("stepColumns.requests"), numeric: true },
                    { key: "rps", label: v("columns.rps"), numeric: true },
                    { key: "p50", label: "p50", numeric: true },
                    { key: "p95", label: "p95", numeric: true },
                    { key: "p99", label: "p99", numeric: true },
                    { key: "err", label: v("columns.errorRate"), numeric: true },
                  ]}
                >
                  {(r.steps ?? []).map((s) => (
                    <TelemetryRow key={s.key}>
                      <TelemetryCell className="max-w-sm truncate font-mono text-micro">{s.key}</TelemetryCell>
                      <TelemetryCell numeric>{formatCount(s.count, locale)}</TelemetryCell>
                      <TelemetryCell numeric>{formatCount(s.rps, locale)}</TelemetryCell>
                      <TelemetryCell numeric>{formatMs(s.p50, locale)}</TelemetryCell>
                      <TelemetryCell numeric>{formatMs(s.p95, locale)}</TelemetryCell>
                      <TelemetryCell numeric>{formatMs(s.p99, locale)}</TelemetryCell>
                      <TelemetryCell numeric>{formatRatio(s.errorRate, locale)}</TelemetryCell>
                    </TelemetryRow>
                  ))}
                </TelemetryTable>
              )}
              {codes.length > 0 && (
                <div className="flex flex-wrap items-center gap-2 text-caption">
                  <span className="text-muted-foreground">{v("statusCodes")}</span>
                  {codes.map(([code, n]) => (
                    <Badge key={code} variant={code.startsWith("5") ? "destructive" : code.startsWith("4") ? "warning" : "secondary"}>
                      {`${code} × ${formatCount(n, locale)}`}
                    </Badge>
                  ))}
                </div>
              )}
            </div>
          );
        }}
      </TelemetryQueryView>
    </div>
  );
}
