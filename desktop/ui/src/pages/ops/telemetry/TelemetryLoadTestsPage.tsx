import { useEffect, useMemo, useState } from "react";
import { Link } from "react-router-dom";
import { useOpsHref } from "@/components/operations/shell/OpsContext";
import { LoadRunBadge } from "@/components/operations/telemetry/LoadRunBadge";
import { TelemetryQueryView } from "@/components/operations/telemetry/TelemetryQueryView";
import { TelemetryCell, TelemetryRow, TelemetryTable } from "@/components/operations/telemetry/TelemetryTable";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Progress } from "@/components/ui/progress";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { useI18n } from "@/hooks/useI18n";
import { useTelemetryQuery } from "@/hooks/useTelemetryQuery";
import { useTelemetryScope } from "@/hooks/useTelemetryScope";
import { useUrlParam } from "@/hooks/useUrlParam";
import { intlLocale } from "@/lib/languages";
import { formatCount, formatMs, formatRatio } from "@/lib/telemetryHub";
import { formatDuration, isLiveRun, type TelemetryLoadRun, type TelemetryLoadScenario } from "@/lib/telemetryViews";

const TABS = ["runs", "scenarios"] as const;
type Tab = (typeof TABS)[number];

export function loadRunHref(id: string): string {
  return `/operations/telemetry/load-tests/${encodeURIComponent(id)}`;
}

/** `/operations/telemetry/load-tests`: load-test runs, newest first, and the
 * scenarios they ran. Read-only here: agents start runs. */
export default function TelemetryLoadTestsPage() {
  const { t, lang } = useI18n();
  const v = (key: string, params?: Record<string, string | number>) => t(`telemetryViews.loadTests.${key}`, params);
  const locale = intlLocale(lang);
  const href = useOpsHref();
  useTelemetryScope({ time: false });
  const [tabParam, setTab] = useUrlParam("tab");
  const [scenario, setScenario] = useUrlParam("scenario");
  const tab: Tab = TABS.find((x) => x === tabParam) ?? "runs";
  const scenarios = useTelemetryQuery<TelemetryLoadScenario[]>("loadtests/scenarios");
  const [live, setLive] = useState(false);
  const runs = useTelemetryQuery<TelemetryLoadRun[]>("loadtests/runs", { scenarioId: scenario || undefined }, { intervalMs: live ? 5_000 : 30_000 });
  useEffect(() => setLive((runs.data ?? []).some((r) => isLiveRun(r.state))), [runs.data]);
  const names = useMemo(() => new Map((scenarios.data ?? []).map((s) => [s.id, s.name])), [scenarios.data]);
  const time = new Intl.DateTimeFormat(intlLocale(lang), { dateStyle: "short", timeStyle: "short" });

  return (
    <div className="space-y-4">
      {scenario && (
        <div className="flex items-center gap-2 text-caption">
          <span className="text-muted-foreground">{v("filteredBy")}</span>
          <Badge variant="secondary">{names.get(scenario) ?? scenario}</Badge>
          <Button variant="link" size="sm" onClick={() => setScenario(null)}>
            {v("clearFilter")}
          </Button>
        </div>
      )}
      <Tabs value={tab} onValueChange={(x) => setTab(x === "runs" ? null : x)}>
        <TabsList aria-label={v("sections")}>
          {TABS.map((x) => (
            <TabsTrigger key={x} value={x}>
              {v(`tabs.${x}`)}
            </TabsTrigger>
          ))}
        </TabsList>
        <TabsContent value="runs" className="pt-4">
          <TelemetryQueryView query={runs} isEmpty={(d) => d.length === 0} emptyTitle={v("empty")} emptyDescription={v("emptyHint")}>
            {(rows) => (
              <TelemetryTable
                label={v("runsLabel")}
                columns={[
                  { key: "state", label: v("columns.state"), className: "w-28" },
                  { key: "scenario", label: v("columns.scenario") },
                  { key: "started", label: v("columns.started"), className: "w-36" },
                  { key: "vus", label: v("columns.vus"), numeric: true },
                  { key: "rps", label: v("columns.rps"), numeric: true },
                  { key: "p95", label: v("columns.p95"), numeric: true },
                  { key: "err", label: v("columns.errorRate"), numeric: true },
                  { key: "thresholds", label: v("columns.thresholds") },
                ]}
              >
                {[...rows]
                  .sort((a, b) => b.startedAt - a.startedAt)
                  .map((r) => {
                    const failed = (r.thresholds ?? []).filter((x) => !x.passed).length;
                    return (
                      <TelemetryRow key={r.id}>
                        <TelemetryCell>
                          <LoadRunBadge state={r.state} />
                          {r.state === "running" && <Progress value={r.progress * 100} className="mt-1 h-1" label={v("progress")} />}
                        </TelemetryCell>
                        <TelemetryCell>
                          <Link to={href(loadRunHref(r.id))} className="font-medium hover:underline">
                            {names.get(r.scenarioId) ?? r.scenarioId}
                          </Link>
                          {r.region && <p className="text-micro text-muted-foreground">{r.region}</p>}
                        </TelemetryCell>
                        <TelemetryCell className="font-mono text-micro whitespace-nowrap">{time.format(r.startedAt)}</TelemetryCell>
                        <TelemetryCell numeric>{formatCount(r.vus, locale)}</TelemetryCell>
                        <TelemetryCell numeric>{formatCount(r.rps, locale)}</TelemetryCell>
                        <TelemetryCell numeric>{formatMs(r.p95, locale)}</TelemetryCell>
                        <TelemetryCell numeric>{formatRatio(r.errorRate, locale)}</TelemetryCell>
                        <TelemetryCell>
                          {(r.thresholds ?? []).length === 0 ? (
                            "-"
                          ) : failed > 0 ? (
                            <Badge variant="destructive">{v("thresholdsFailed", { count: failed })}</Badge>
                          ) : (
                            <Badge variant="success">{v("thresholdsPassed")}</Badge>
                          )}
                        </TelemetryCell>
                      </TelemetryRow>
                    );
                  })}
              </TelemetryTable>
            )}
          </TelemetryQueryView>
        </TabsContent>
        <TabsContent value="scenarios" className="pt-4">
          <TelemetryQueryView query={scenarios} isEmpty={(d) => d.length === 0} emptyTitle={v("noScenarios")}>
            {(rows) => (
              <TelemetryTable
                label={v("tabs.scenarios")}
                columns={[
                  { key: "name", label: v("columns.scenario") },
                  { key: "target", label: v("columns.target") },
                  { key: "peak", label: v("columns.peakVus"), numeric: true },
                  { key: "duration", label: v("columns.duration"), numeric: true },
                  { key: "source", label: v("columns.source") },
                ]}
              >
                {rows.map((s) => (
                  <TelemetryRow key={s.id} onOpen={() => (setScenario(s.id), setTab(null))}>
                    <TelemetryCell>
                      <span className="font-medium">{s.name}</span>
                      {s.state === "draft" && (
                        <Badge variant="outline" className="ml-2">
                          {v("draft")}
                        </Badge>
                      )}
                      <p className="text-micro text-muted-foreground">{v("stepsRegions", { steps: s.steps, regions: (s.regions ?? []).join(", ") || "-" })}</p>
                    </TelemetryCell>
                    <TelemetryCell className="max-w-xs truncate font-mono text-micro">{s.target}</TelemetryCell>
                    <TelemetryCell numeric>{formatCount(s.peakVus, locale)}</TelemetryCell>
                    <TelemetryCell numeric>{formatDuration(s.duration)}</TelemetryCell>
                    <TelemetryCell>
                      <Badge variant="secondary">{s.source}</Badge>
                    </TelemetryCell>
                  </TelemetryRow>
                ))}
              </TelemetryTable>
            )}
          </TelemetryQueryView>
        </TabsContent>
      </Tabs>
    </div>
  );
}
