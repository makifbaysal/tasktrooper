import { Search } from "lucide-react";
import { useMemo } from "react";
import { Link } from "react-router-dom";
import { Sparkline } from "@/components/operations/charts/Sparkline";
import { FacetChips, type FacetChip } from "@/components/operations/console/FacetChips";
import { useOpsHref } from "@/components/operations/shell/OpsContext";
import { HealthMark } from "@/components/operations/telemetry/HealthMark";
import { TelemetryQueryView } from "@/components/operations/telemetry/TelemetryQueryView";
import { TelemetryCell, TelemetryRow, TelemetryTable } from "@/components/operations/telemetry/TelemetryTable";
import { TelemetryToolbar } from "@/components/operations/telemetry/TelemetryToolbar";
import { Input } from "@/components/ui/input";
import { useI18n } from "@/hooks/useI18n";
import { useTelemetryQuery } from "@/hooks/useTelemetryQuery";
import { useTelemetryScope } from "@/hooks/useTelemetryScope";
import { useUrlParam } from "@/hooks/useUrlParam";
import { intlLocale } from "@/lib/languages";
import { formatCount, formatMs, formatRatio, sparkPoints, type TelemetryService } from "@/lib/telemetryHub";

const ALL = "all";
const KNOWN_TYPES = ["http", "worker", "consumer", "web", "mobile"];

export function serviceTypeLabel(t: (key: string) => string, type: string): string {
  return KNOWN_TYPES.includes(type) ? t(`telemetryHub.services.types.${type}`) : type;
}

export function serviceHref(name: string): string {
  return `/operations/telemetry/services/${encodeURIComponent(name)}`;
}

/** `/operations/telemetry/services`: every service the product has data for,
 * worst first, with its request rate, error rate and p95 in the window. */
export default function TelemetryServicesPage() {
  const { t, lang } = useI18n();
  const h = (key: string, params?: Record<string, string | number>) => t(`telemetryHub.services.${key}`, params);
  const locale = intlLocale(lang);
  const href = useOpsHref();
  const scope = useTelemetryScope();
  const [type, setType] = useUrlParam("type");
  const [query, setQuery] = useUrlParam("q");
  const read = useTelemetryQuery<TelemetryService[]>("services", { range: scope.range, env: scope.env });

  const rows = useMemo(() => {
    const all = read.data ?? [];
    const needle = query.trim().toLowerCase();
    const rank = (s: TelemetryService) => (s.status === "critical" ? 0 : s.status === "warning" ? 1 : 2);
    return all
      .filter((s) => (!type || type === ALL || s.type === type) && (!needle || s.name.toLowerCase().includes(needle)))
      .sort((a, b) => rank(a) - rank(b) || b.rpm - a.rpm || a.name.localeCompare(b.name));
  }, [read.data, type, query]);

  const facets = useMemo<FacetChip<string>[]>(() => {
    const all = read.data ?? [];
    const types = [...new Set(all.map((s) => s.type))].sort();
    return [
      { value: ALL, label: h("allTypes"), count: all.length },
      ...types.map((x) => ({ value: x, label: serviceTypeLabel(t, x), count: all.filter((s) => s.type === x).length })),
    ];
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [read.data, t]);

  return (
    <div className="space-y-4">
      <TelemetryToolbar>
        <div className="relative w-64 max-w-full">
          <Search className="pointer-events-none absolute top-2.5 left-2.5 h-3.5 w-3.5 text-muted-foreground" aria-hidden />
          <Input className="h-8 pl-8" value={query} onChange={(e) => setQuery(e.target.value)} placeholder={h("search")} aria-label={h("search")} />
        </div>
        {facets.length > 2 && <FacetChips label={h("typeFilter")} items={facets} active={type || ALL} onSelect={(v) => setType(v === ALL ? null : v)} />}
      </TelemetryToolbar>

      <TelemetryQueryView query={read} isEmpty={(d) => d.length === 0} emptyTitle={h("empty")} emptyDescription={h("emptyHint")}>
        {() =>
          rows.length === 0 ? (
            <p className="py-8 text-center text-caption text-muted-foreground">{h("noMatch")}</p>
          ) : (
            <TelemetryTable
              label={h("listLabel")}
              columns={[
                { key: "health", label: h("columns.health"), className: "w-12" },
                { key: "name", label: h("columns.service") },
                { key: "rpm", label: h("columns.rpm"), numeric: true },
                { key: "trend", label: h("columns.trend") },
                { key: "err", label: h("columns.errorRate"), numeric: true },
                { key: "p95", label: h("columns.p95"), numeric: true },
                { key: "version", label: h("columns.version") },
              ]}
            >
              {rows.map((s) => (
                <TelemetryRow key={s.id}>
                  <TelemetryCell>
                    <HealthMark health={s.status} />
                  </TelemetryCell>
                  <TelemetryCell>
                    <Link to={href(serviceHref(s.name))} className="font-medium hover:underline">
                      {s.name}
                    </Link>
                    <p className="text-micro text-muted-foreground">{[s.type, s.language].filter(Boolean).join(" · ")}</p>
                  </TelemetryCell>
                  <TelemetryCell numeric>{formatCount(s.rpm, locale)}</TelemetryCell>
                  <TelemetryCell>
                    <Sparkline points={sparkPoints(s.spark)} ariaLabel={h("trendLabel", { name: s.name })} />
                  </TelemetryCell>
                  <TelemetryCell numeric>{formatRatio(s.errorRate, locale)}</TelemetryCell>
                  <TelemetryCell numeric>{formatMs(s.p95, locale)}</TelemetryCell>
                  <TelemetryCell className="font-mono text-micro text-muted-foreground">{s.version || "-"}</TelemetryCell>
                </TelemetryRow>
              ))}
            </TelemetryTable>
          )
        }
      </TelemetryQueryView>
    </div>
  );
}
