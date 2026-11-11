import { TelemetryQueryView } from "@/components/operations/telemetry/TelemetryQueryView";
import { TelemetryCell, TelemetryRow, TelemetryTable } from "@/components/operations/telemetry/TelemetryTable";
import { Badge } from "@/components/ui/badge";
import { Card } from "@/components/ui/card";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { useI18n } from "@/hooks/useI18n";
import { useTelemetryQuery } from "@/hooks/useTelemetryQuery";
import type { TelemetryScope } from "@/hooks/useTelemetryScope";
import { useUrlParam } from "@/hooks/useUrlParam";
import { intlLocale } from "@/lib/languages";
import { cn } from "@/lib/utils";
import { formatCount, formatMs, formatRatio, type TelemetryEndpointRca } from "@/lib/telemetryHub";

const BASELINES = ["prev", "1d", "7d"] as const;

/** Why an endpoint changed: the window against a baseline, and which
 * dependency took the milliseconds. */
export function RcaPanel({ path, scope }: { path: string; scope: TelemetryScope }) {
  const { t, lang } = useI18n();
  const h = (key: string, params?: Record<string, string | number>) => t(`telemetryHub.rca.${key}`, params);
  const locale = intlLocale(lang);
  const [param, setBaseline] = useUrlParam("baseline");
  const baseline = BASELINES.find((b) => b === param) ?? "prev";
  const read = useTelemetryQuery<TelemetryEndpointRca>(path, { range: scope.range, env: scope.env, baseline });

  return (
    <section className="space-y-3" aria-label={h("title")}>
      <div className="flex flex-wrap items-center gap-2">
        <h3 className="text-title font-semibold">{h("title")}</h3>
        <div className="ml-auto flex items-center gap-2">
          <span className="text-caption text-muted-foreground">{h("comparedWith")}</span>
          <Select value={baseline} onValueChange={(v) => setBaseline(v === "prev" ? null : v)}>
            <SelectTrigger className="h-8 w-48" aria-label={h("comparedWith")}>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {BASELINES.map((b) => (
                <SelectItem key={b} value={b}>
                  {h(`baselines.${b}`)}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
      </div>
      <TelemetryQueryView query={read} skeletonHeight="h-32">
        {(r) => {
          const children = [...(r.children ?? [])].sort((a, b) => b.deltaMsPerReq - a.deltaMsPerReq);
          const maxDelta = Math.max(1, ...children.map((c) => Math.abs(c.deltaMsPerReq)));
          const delta = r.deltaAvgMs;
          const rows: Array<[string, string, string]> = [
            [h("metrics.requests"), formatCount(r.current.requests, locale), formatCount(r.baseline.requests, locale)],
            [h("metrics.avg"), formatMs(r.current.avgMs, locale), formatMs(r.baseline.avgMs, locale)],
            [h("metrics.p50"), formatMs(r.current.p50, locale), formatMs(r.baseline.p50, locale)],
            [h("metrics.p95"), formatMs(r.current.p95, locale), formatMs(r.baseline.p95, locale)],
            [h("metrics.errorRate"), formatRatio(r.current.errorRate, locale), formatRatio(r.baseline.errorRate, locale)],
          ];
          return (
            <div className="space-y-3">
              <Card className="p-4 text-body">
                {delta === 0 || !Number.isFinite(delta)
                  ? h("summaryFlat")
                  : delta > 0
                    ? h("summarySlower", { ms: formatMs(delta, locale) })
                    : h("summaryFaster", { ms: formatMs(-delta, locale) })}
                {r.ownMs.length === 2 && (
                  <span className="text-muted-foreground"> {h("ownCode", { current: formatMs(r.ownMs[0], locale), baseline: formatMs(r.ownMs[1], locale) })}</span>
                )}
              </Card>
              <TelemetryTable
                label={h("title")}
                columns={[
                  { key: "metric", label: h("columns.metric") },
                  { key: "current", label: h("columns.current"), numeric: true },
                  { key: "baseline", label: h("columns.baseline"), numeric: true },
                ]}
              >
                {rows.map(([label, now, before]) => (
                  <TelemetryRow key={label}>
                    <TelemetryCell>{label}</TelemetryCell>
                    <TelemetryCell numeric>{now}</TelemetryCell>
                    <TelemetryCell numeric>{before}</TelemetryCell>
                  </TelemetryRow>
                ))}
              </TelemetryTable>
              {children.length > 0 && (
                <TelemetryTable
                  label={h("dependencies")}
                  columns={[
                    { key: "dep", label: h("columns.dependency") },
                    { key: "kind", label: h("columns.kind") },
                    { key: "delta", label: h("columns.delta"), numeric: true },
                    { key: "bar", label: h("columns.share"), className: "w-48" },
                  ]}
                >
                  {children.map((c) => (
                    <TelemetryRow key={c.dep}>
                      <TelemetryCell className="font-medium">{c.dep}</TelemetryCell>
                      <TelemetryCell>
                        <Badge variant="secondary">{c.kind}</Badge>
                      </TelemetryCell>
                      <TelemetryCell numeric className={cn(c.deltaMsPerReq > 0 ? "text-destructive" : "text-success")}>
                        {c.deltaMsPerReq > 0 ? "+" : ""}
                        {formatMs(c.deltaMsPerReq, locale)}
                      </TelemetryCell>
                      <TelemetryCell>
                        <div className="h-1.5 w-full overflow-hidden rounded-full bg-muted" role="img" aria-label={h("shareLabel", { percent: Math.round(Math.abs(c.share) * 100) })}>
                          <div
                            className={cn("h-full rounded-full", c.deltaMsPerReq > 0 ? "bg-destructive" : "bg-success")}
                            style={{ width: `${Math.max(2, (Math.abs(c.deltaMsPerReq) / maxDelta) * 100)}%` }}
                          />
                        </div>
                      </TelemetryCell>
                    </TelemetryRow>
                  ))}
                </TelemetryTable>
              )}
            </div>
          );
        }}
      </TelemetryQueryView>
    </section>
  );
}
