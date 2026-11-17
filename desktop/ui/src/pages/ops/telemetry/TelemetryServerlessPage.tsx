import { Search } from "lucide-react";
import { Link } from "react-router-dom";
import { Sparkline } from "@/components/operations/charts/Sparkline";
import { FacetChips, type FacetChip } from "@/components/operations/console/FacetChips";
import { useOpsHref } from "@/components/operations/shell/OpsContext";
import { InfraHealthMark } from "@/components/operations/telemetry/HealthMark";
import { TelemetryQueryView } from "@/components/operations/telemetry/TelemetryQueryView";
import { TelemetryCell, TelemetryRow, TelemetryTable } from "@/components/operations/telemetry/TelemetryTable";
import { TelemetryToolbar } from "@/components/operations/telemetry/TelemetryToolbar";
import { Input } from "@/components/ui/input";
import { StatTile } from "@/components/ui/stat-tile";
import { useI18n } from "@/hooks/useI18n";
import { useTelemetryQuery } from "@/hooks/useTelemetryQuery";
import { useTelemetryScope } from "@/hooks/useTelemetryScope";
import { useUrlParam } from "@/hooks/useUrlParam";
import { intlLocale } from "@/lib/languages";
import { formatCount, formatMs, formatRatio } from "@/lib/telemetryHub";
import { formatUsd, seriesFromValues, type TelemetryFunction, type TelemetryServerlessSummary } from "@/lib/telemetryViews";

const ALL = "all";
const PROVIDERS = ["aws", "azure"];
export const PROVIDER_LABEL: Record<string, string> = { aws: "AWS Lambda", azure: "Azure Functions" };

export function functionHref(id: string): string {
  return `/operations/telemetry/infra/serverless/${encodeURIComponent(id)}`;
}

/** `/operations/telemetry/infra/serverless`: Lambda functions and Azure
 * Function Apps, most invoked first, with errors, cold starts and cost. */
export default function TelemetryServerlessPage() {
  const { t, lang } = useI18n();
  const v = (key: string, params?: Record<string, string | number>) => t(`telemetryViews.serverless.${key}`, params);
  const locale = intlLocale(lang);
  const href = useOpsHref();
  const scope = useTelemetryScope();
  const [provider, setProvider] = useUrlParam("provider");
  const [query, setQuery] = useUrlParam("q");
  const summary = useTelemetryQuery<TelemetryServerlessSummary>("serverless/summary", { range: scope.range });
  const read = useTelemetryQuery<TelemetryFunction[]>("serverless/functions", { range: scope.range, provider: provider || undefined, q: query.trim() || undefined });
  const byProvider = summary.data?.byProvider ?? [];
  const facets: FacetChip<string>[] = [
    { value: ALL, label: v("allProviders"), count: summary.data?.functions ?? 0 },
    ...PROVIDERS.map((p) => ({ value: p, label: PROVIDER_LABEL[p], count: byProvider.find((b) => b.provider === p)?.functions ?? 0 })),
  ];

  return (
    <div className="space-y-4">
      {summary.data && (
        <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
          <StatTile label={v("invocations")} value={formatCount(summary.data.invocations, locale)} foot={v("functionCount", { count: summary.data.functions })} />
          <StatTile label={v("errorRate")} value={formatRatio(summary.data.errorRate, locale)} tone={summary.data.errorRate >= 0.01 ? "warning" : "default"} foot={v("throttles", { count: formatCount(summary.data.throttles, locale) })} />
          <StatTile label={v("coldStarts")} value={formatCount(summary.data.coldStarts, locale)} />
          <StatTile label={v("cost")} value={formatUsd(summary.data.costUsd, locale)} foot={v("costHint")} />
        </div>
      )}
      <TelemetryToolbar env={false}>
        <div className="relative w-64 max-w-full">
          <Search className="pointer-events-none absolute top-2.5 left-2.5 h-3.5 w-3.5 text-muted-foreground" aria-hidden />
          <Input className="h-8 pl-8" value={query} onChange={(e) => setQuery(e.target.value)} placeholder={v("search")} aria-label={v("search")} />
        </div>
        <FacetChips label={v("providerFilter")} items={facets} active={provider || ALL} onSelect={(x) => setProvider(x === ALL ? null : x)} />
      </TelemetryToolbar>
      <TelemetryQueryView query={read} isEmpty={(d) => d.length === 0} emptyTitle={query || provider ? v("noMatch") : v("empty")} emptyDescription={query || provider ? undefined : v("emptyHint")}>
        {(rows) => (
          <TelemetryTable
            label={v("listLabel")}
            columns={[
              { key: "health", label: v("columns.health"), className: "w-12" },
              { key: "name", label: v("columns.function") },
              { key: "invocations", label: v("columns.invocations"), numeric: true },
              { key: "trend", label: v("columns.trend") },
              { key: "err", label: v("columns.errorRate"), numeric: true },
              { key: "p95", label: v("columns.p95"), numeric: true },
              { key: "cold", label: v("columns.coldStarts"), numeric: true },
              { key: "cost", label: v("columns.cost"), numeric: true },
            ]}
          >
            {rows.map((f) => (
              <TelemetryRow key={f.id}>
                <TelemetryCell>
                  <InfraHealthMark status={f.status} />
                </TelemetryCell>
                <TelemetryCell className="max-w-xs">
                  <Link to={href(functionHref(f.id))} className="block truncate font-medium hover:underline">
                    {f.name}
                  </Link>
                  <p className="truncate text-micro text-muted-foreground">{[f.provider, f.region, f.runtime, f.service].filter(Boolean).join(" · ")}</p>
                </TelemetryCell>
                <TelemetryCell numeric>{formatCount(f.invocations, locale)}</TelemetryCell>
                <TelemetryCell>
                  <Sparkline points={seriesFromValues(f.spark)} ariaLabel={v("trendLabel", { name: f.name })} />
                </TelemetryCell>
                <TelemetryCell numeric>{formatRatio(f.errorRate, locale)}</TelemetryCell>
                <TelemetryCell numeric>{formatMs(f.p95Ms, locale)}</TelemetryCell>
                <TelemetryCell numeric>{formatRatio(f.coldStartRate, locale)}</TelemetryCell>
                <TelemetryCell numeric>{formatUsd(f.costUsd, locale)}</TelemetryCell>
              </TelemetryRow>
            ))}
          </TelemetryTable>
        )}
      </TelemetryQueryView>
    </div>
  );
}
