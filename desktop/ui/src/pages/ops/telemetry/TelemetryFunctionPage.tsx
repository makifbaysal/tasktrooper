import { Link, useParams } from "react-router-dom";
import { DetailFacts } from "@/components/operations/console/DetailPanel";
import { useOpsHref } from "@/components/operations/shell/OpsContext";
import { InfraHealthMark } from "@/components/operations/telemetry/HealthMark";
import { SeriesChartCard } from "@/components/operations/telemetry/SeriesChartCard";
import { TelemetryBackLink } from "@/components/operations/telemetry/TelemetryBackLink";
import { TelemetryQueryView } from "@/components/operations/telemetry/TelemetryQueryView";
import { TelemetryCell, TelemetryRow, TelemetryTable } from "@/components/operations/telemetry/TelemetryTable";
import { Badge } from "@/components/ui/badge";
import { Card } from "@/components/ui/card";
import { StatTile } from "@/components/ui/stat-tile";
import { useI18n } from "@/hooks/useI18n";
import { useTelemetryQuery } from "@/hooks/useTelemetryQuery";
import { useTelemetryScope } from "@/hooks/useTelemetryScope";
import { intlLocale } from "@/lib/languages";
import { formatCount, formatMs, formatRatio } from "@/lib/telemetryHub";
import { formatUsd, type TelemetryFunctionDetail } from "@/lib/telemetryViews";
import { functionHref, PROVIDER_LABEL } from "@/pages/ops/telemetry/TelemetryServerlessPage";
import { serviceHref } from "@/pages/ops/telemetry/TelemetryServicesPage";

/** `/operations/telemetry/infra/serverless/:id`: one function's invocations,
 * duration, concurrency and cold starts, its configuration and latest errors. */
export default function TelemetryFunctionPage() {
  const { id = "" } = useParams();
  const { t, lang } = useI18n();
  const v = (key: string, params?: Record<string, string | number>) => t(`telemetryViews.serverless.${key}`, params);
  const locale = intlLocale(lang);
  const href = useOpsHref();
  const scope = useTelemetryScope();
  const read = useTelemetryQuery<TelemetryFunctionDetail>(`serverless/functions/${encodeURIComponent(id)}`, { range: scope.range });
  const loading = read.data === undefined && !read.error;
  const time = new Intl.DateTimeFormat(intlLocale(lang), { dateStyle: "short", timeStyle: "medium" });

  return (
    <div className="space-y-4">
      <TelemetryBackLink to="/operations/telemetry/infra/serverless" label={v("back")} />
      <TelemetryQueryView query={read}>
        {(f) => (
          <div className="space-y-4">
            <div className="flex flex-wrap items-center gap-2">
              <InfraHealthMark status={f.status} showLabel />
              <h2 className="text-heading font-semibold break-all">{f.name}</h2>
              <Badge variant="secondary">{PROVIDER_LABEL[f.provider] ?? f.provider}</Badge>
              {f.runtime && <Badge variant="outline">{f.runtime}</Badge>}
            </div>
            <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
              <StatTile label={v("invocations")} value={formatCount(f.invocations, locale)} foot={v("errorsCount", { count: formatCount(f.errors, locale) })} />
              <StatTile label={v("errorRate")} value={formatRatio(f.errorRate, locale)} tone={f.errorRate >= 0.01 ? "warning" : "default"} foot={v("throttles", { count: formatCount(f.throttles, locale) })} />
              <StatTile label={v("columns.p95")} value={formatMs(f.p95Ms, locale)} foot={v("p99", { value: formatMs(f.p99Ms, locale) })} />
              <StatTile label={v("coldStarts")} value={formatRatio(f.coldStartRate, locale)} foot={v("cost") + ": " + formatUsd(f.costUsd, locale)} />
            </div>
            <Card className="p-4">
              <DetailFacts
                items={[
                  { label: v("facts.region"), value: f.region || "-" },
                  { label: v("facts.app"), value: f.app || "-" },
                  { label: v("facts.memory"), value: f.memoryMb ? `${f.memoryMb} MB` : "-" },
                  { label: v("facts.timeout"), value: f.timeoutS ? `${f.timeoutS} s` : "-" },
                  { label: v("facts.arch"), value: f.arch || "-" },
                  { label: v("facts.triggers"), value: (f.triggers ?? []).join(", ") || "-" },
                  { label: v("facts.concurrency"), value: formatCount(f.concurrencyMax, locale) },
                  { label: v("facts.service"), value: f.service ? <Link className="hover:underline" to={href(serviceHref(f.service))}>{f.service}</Link> : "-" },
                ]}
              />
            </Card>
            <div className="grid gap-4 lg:grid-cols-2">
              <SeriesChartCard title={v("charts.invocations")} set={f.series?.invocations} scope={scope} loading={loading} syncKey="tel:fn" />
              <SeriesChartCard title={v("charts.duration")} set={f.series?.duration} scope={scope} loading={loading} formatValue={(n) => formatMs(n, locale)} syncKey="tel:fn" />
              <SeriesChartCard title={v("charts.concurrency")} set={f.series?.concurrency} scope={scope} loading={loading} syncKey="tel:fn" />
              <SeriesChartCard title={v("charts.coldStarts")} set={f.series?.coldStarts} scope={scope} loading={loading} syncKey="tel:fn" />
            </div>
            {(f.children ?? []).length > 0 && (
              <TelemetryTable
                label={v("children")}
                columns={[
                  { key: "name", label: v("columns.function") },
                  { key: "invocations", label: v("columns.invocations"), numeric: true },
                  { key: "err", label: v("columns.errorRate"), numeric: true },
                  { key: "p95", label: v("columns.p95"), numeric: true },
                ]}
              >
                {(f.children ?? []).map((c) => (
                  <TelemetryRow key={c.id}>
                    <TelemetryCell>
                      <Link to={href(functionHref(c.id))} className="font-medium hover:underline">
                        {c.name}
                      </Link>
                    </TelemetryCell>
                    <TelemetryCell numeric>{formatCount(c.invocations, locale)}</TelemetryCell>
                    <TelemetryCell numeric>{formatRatio(c.errorRate, locale)}</TelemetryCell>
                    <TelemetryCell numeric>{formatMs(c.p95Ms, locale)}</TelemetryCell>
                  </TelemetryRow>
                ))}
              </TelemetryTable>
            )}
            {(f.recentErrors ?? []).length > 0 && (
              <TelemetryTable
                label={v("recentErrors")}
                columns={[
                  { key: "time", label: v("errorColumns.time"), className: "w-40" },
                  { key: "message", label: v("errorColumns.message") },
                ]}
              >
                {(f.recentErrors ?? []).map((e, i) => (
                  <TelemetryRow key={`${e.traceId}:${i}`}>
                    <TelemetryCell className="font-mono text-micro whitespace-nowrap">{time.format(e.ts)}</TelemetryCell>
                    <TelemetryCell>
                      {e.traceId ? (
                        <Link to={href(`/operations/telemetry/traces/${encodeURIComponent(e.traceId)}`)} className="hover:underline">
                          {e.message}
                        </Link>
                      ) : (
                        e.message
                      )}
                    </TelemetryCell>
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
