import { useCallback, useEffect, useMemo, useState } from "react";
import { Link, useParams } from "react-router-dom";
import { toast } from "sonner";
import {
  api,
  type Agent,
  type AgentKPI,
  type AgentKPIResult,
  type AgentPerformance,
  type AgentReflection,
  type EvolutionEvent,
} from "@/api";
import { PageContent } from "@/components/layout/PageContent";
import { PageHeader } from "@/components/admin/PageHeader";
import { AgentKPISection } from "@/components/agent/AgentKPISection";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import { useI18n } from "@/hooks/useI18n";
import { usePolling } from "@/hooks/usePolling";

function fmtDate(iso: string) {
  return new Date(iso).toLocaleString("tr-TR", { day: "2-digit", month: "2-digit", hour: "2-digit", minute: "2-digit" });
}

// Single-series score trend; the table below is the accessible data view.
function ScoreSparkline({ values }: { values: number[] }) {
  const { t } = useI18n();
  if (values.length < 2) return null;
  const w = 240;
  const h = 48;
  const pad = 4;
  const min = Math.min(...values);
  const max = Math.max(...values);
  const range = max - min || 1;
  const pts = values.map((v, i) => {
    const x = pad + (i / (values.length - 1)) * (w - pad * 2);
    const y = pad + (1 - (v - min) / range) * (h - pad * 2);
    return `${x.toFixed(1)},${y.toFixed(1)}`;
  });
  const last = pts[pts.length - 1].split(",");
  return (
    <svg
      width={w}
      height={h}
      viewBox={`0 0 ${w} ${h}`}
      role="img"
      aria-label={t("agentArea.perf.sparkline.ariaLabel", { min, max })}
      className="overflow-visible"
    >
      <title>{t("agentArea.perf.sparkline.title", { count: values.length, min, max })}</title>
      <polyline
        points={pts.join(" ")}
        fill="none"
        stroke="currentColor"
        strokeWidth="2"
        strokeLinejoin="round"
        strokeLinecap="round"
        className="text-primary"
      />
      <circle cx={last[0]} cy={last[1]} r="3.5" className="fill-primary" />
    </svg>
  );
}

function KPICard({ kpi, result }: { kpi: AgentKPI; result?: AgentKPIResult }) {
  const { t } = useI18n();
  const attainment = result?.attainment ?? 0;
  const barClass = attainment >= 1 ? "bg-emerald-500" : attainment >= 0.5 ? "bg-amber-500" : "bg-red-500";
  const attLabel =
    attainment >= 1
      ? t("agentArea.perf.attainment.full")
      : attainment >= 0.5
        ? t("agentArea.perf.attainment.half")
        : t("agentArea.perf.attainment.none");
  return (
    <Card className="space-y-2 p-4">
      <div className="flex items-start justify-between gap-2">
        <div className="min-w-0">
          <div className="text-sm font-medium">{kpi.name || kpi.metric_key}</div>
          <div className="text-xs text-muted-foreground">
            {kpi.metric_key} · {kpi.period}
          </div>
        </div>
        <div className="shrink-0 text-right">
          <div className="text-lg font-semibold tabular-nums">
            {result ? `${Math.round(attainment * 100)}%` : "–"}
          </div>
          <div
            className="text-xs text-muted-foreground"
            // A time KPI is left unscored rather than scored zero when the
            // period holds too few clean tasks, so say why the slot is empty.
            title={result ? undefined : t("agentArea.perf.kpiCard.noMeasurementHint")}
          >
            {result ? attLabel : t("agentArea.perf.kpiCard.noMeasurement")}
          </div>
        </div>
      </div>
      <div className="text-xs text-muted-foreground">
        {t("agentArea.perf.kpiCard.measured")}{" "}
        <span className="font-medium text-foreground">{result ? result.measured_value : "–"}</span>{" "}
        {t("agentArea.perf.kpiCard.targets", { full: kpi.target_full, half: kpi.target_half })}
      </div>
      <div className="h-1.5 w-full overflow-hidden rounded-full bg-muted">
        <div className={`h-full rounded-full ${barClass}`} style={{ width: `${Math.max(attainment * 100, 4)}%` }} />
      </div>
    </Card>
  );
}

export function AgentPerformancePage() {
  const { t } = useI18n();
  const { agentId } = useParams();
  const [agent, setAgent] = useState<Agent | null>(null);
  const [perf, setPerf] = useState<AgentPerformance | null>(null);
  const [events, setEvents] = useState<EvolutionEvent[]>([]);
  const [reflections, setReflections] = useState<AgentReflection[]>([]);
  const [memoryCount, setMemoryCount] = useState(0);
  const [loading, setLoading] = useState(true);
  const [reflecting, setReflecting] = useState(false);
  const [expandedEvent, setExpandedEvent] = useState<string | null>(null);

  const eventTypeLabels: Record<string, string> = {
    task_completed: t("agentArea.perf.eventType.task_completed"),
    task_released: t("agentArea.perf.eventType.task_released"),
    revision_requested: t("agentArea.perf.eventType.revision_requested"),
    pm_uat_failed: t("agentArea.perf.eventType.pm_uat_failed"),
    human_uat_failed: t("agentArea.perf.eventType.human_uat_failed"),
    qa_bug_found: t("agentArea.perf.eventType.qa_bug_found"),
    qa_valid_scenario_confirmed: t("agentArea.perf.eventType.qa_valid_scenario_confirmed"),
    qa_invalid_scenario_confirmed: t("agentArea.perf.eventType.qa_invalid_scenario_confirmed"),
    qa_task_tested: t("agentArea.perf.eventType.qa_task_tested"),
    pm_uat_completed: t("agentArea.perf.eventType.pm_uat_completed"),
  };

  const changeTypeLabels: Record<string, string> = {
    skill_created: t("agentArea.perf.changeType.skill_created"),
    skill_updated: t("agentArea.perf.changeType.skill_updated"),
    skill_deleted: t("agentArea.perf.changeType.skill_deleted"),
    rule_created: t("agentArea.perf.changeType.rule_created"),
    rule_updated: t("agentArea.perf.changeType.rule_updated"),
    rule_deleted: t("agentArea.perf.changeType.rule_deleted"),
    memory_created: t("agentArea.perf.changeType.memory_created"),
    memory_deleted: t("agentArea.perf.changeType.memory_deleted"),
    revert: t("agentArea.perf.changeType.revert"),
  };

  const impactMeta: Record<string, { label: string; className: string }> = {
    effective: { label: t("agentArea.perf.impact.effective"), className: "bg-emerald-500/15 text-emerald-600 dark:text-emerald-400" },
    regressed: { label: t("agentArea.perf.impact.regressed"), className: "bg-red-500/15 text-red-600 dark:text-red-400" },
    neutral: { label: t("agentArea.perf.impact.neutral"), className: "bg-muted text-muted-foreground" },
    pending: { label: t("agentArea.perf.impact.pending"), className: "bg-amber-500/15 text-amber-600 dark:text-amber-400" },
    insufficient_data: { label: t("agentArea.perf.impact.insufficient_data"), className: "bg-muted text-muted-foreground" },
  };

  const triggerLabels: Record<string, string> = {
    periodic: t("agentArea.perf.trigger.periodic"),
    revision: t("agentArea.perf.trigger.revision"),
    manual: t("agentArea.perf.trigger.manual"),
  };

  const load = useCallback(async () => {
    if (!agentId) return;
    try {
      const [agentData, perfData, evData, refData, memData] = await Promise.all([
        api.getAgent(agentId).catch(() => null),
        api.getAgentPerformance(agentId).catch(() => null),
        api.listAgentEvolutionEvents(agentId).catch(() => ({ events: [] })),
        api.listAgentReflections(agentId).catch(() => ({ reflections: [] })),
        api.listAgentMemories(agentId, "agent").catch(() => ({ memories: [] })),
      ]);
      setAgent(agentData);
      setPerf(perfData);
      setEvents(evData.events ?? []);
      setReflections(refData.reflections ?? []);
      setMemoryCount((memData.memories ?? []).length);
    } finally {
      setLoading(false);
    }
  }, [agentId]);

  useEffect(() => {
    void load();
  }, [load]);

  const hasRunning = reflections.some((r) => r.status === "running");

  // Poll only while a reflection is actually running — and, via usePolling,
  // only while the tab is visible.
  usePolling(load, 5000, hasRunning);

  const scoreSeries = useMemo(() => {
    const evs = perf?.events ?? [];
    return [...evs].reverse().map((e) => e.score_after);
  }, [perf?.events]);

  const kpiResultByID = useMemo(() => {
    const map = new Map<string, AgentKPIResult>();
    for (const r of perf?.kpi_results ?? []) map.set(r.kpi_id, r);
    return map;
  }, [perf?.kpi_results]);

  const handleReflect = async () => {
    if (!agentId) return;
    setReflecting(true);
    try {
      await api.reflectNow(agentId);
      toast.success(t("agentArea.perf.toast.analysisStarted"));
      await load();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("agentArea.perf.toast.analysisFailed"));
    } finally {
      setReflecting(false);
    }
  };

  const toggleSelfEvo = async (enabled: boolean) => {
    if (!agent || !agentId) return;
    try {
      await api.updateAgent(agentId, {
        name: agent.name,
        description: agent.description,
        subagent_type: agent.subagent_type,
        system_prompt: agent.system_prompt,
        provider_type: agent.provider_type ?? "",
        model: agent.model,
        model_heavy: agent.model_heavy ?? "",
        tool_policy: agent.tool_policy ?? {},
        enabled: agent.enabled,
        self_evolution_enabled: enabled,
      });
      setAgent({ ...agent, self_evolution_enabled: enabled });
      toast.success(enabled ? t("agentArea.perf.selfEvo.on") : t("agentArea.perf.selfEvo.off"));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("agentArea.perf.toast.updateFailed"));
    }
  };

  if (!agentId) return null;

  if (loading) {
    return (
      <PageContent className="space-y-4">
        <Skeleton className="h-8 w-64" />
        <Skeleton className="h-40 w-full" />
        <Skeleton className="h-64 w-full" />
      </PageContent>
    );
  }

  const score = perf?.score;
  const enabledKPIs = (perf?.kpis ?? []).filter((k) => k.enabled);

  return (
    <PageContent className="space-y-6 pb-8">
      <PageHeader
        title={t("agentArea.perf.header.title")}
        description={t("agentArea.perf.header.description")}
        action={
          <Button onClick={handleReflect} disabled={reflecting || hasRunning}>
            {hasRunning
              ? t("agentArea.perf.header.reflectRunning")
              : reflecting
                ? t("agentArea.perf.header.reflectStarting")
                : t("agentArea.perf.header.reflectNow")}
          </Button>
        }
      />

      <div className="grid gap-4 lg:grid-cols-3">
        <Card className="space-y-1 p-4">
          <div className="text-xs font-medium tracking-wide text-muted-foreground uppercase">{t("agentArea.perf.score.label")}</div>
          <div className="flex items-end justify-between gap-2">
            <div className="text-3xl font-semibold tabular-nums">{score ? score.score.toFixed(1) : "100.0"}</div>
            <ScoreSparkline values={scoreSeries} />
          </div>
          <div className="text-xs text-muted-foreground">
            {t("agentArea.perf.score.runs", { passed: score?.runs_passed ?? 0, revised: score?.runs_revised ?? 0 })}
          </div>
        </Card>
        <Card className="space-y-1 p-4">
          <div className="text-xs font-medium tracking-wide text-muted-foreground uppercase">{t("agentArea.perf.kpiComposite.label")}</div>
          <div className="text-3xl font-semibold tabular-nums">
            {perf?.kpi_composite != null ? perf.kpi_composite.toFixed(0) : "–"}
            <span className="text-base font-normal text-muted-foreground">/100</span>
          </div>
          <div className="text-xs text-muted-foreground">{t("agentArea.perf.kpiComposite.active", { count: enabledKPIs.length })}</div>
        </Card>
        <Card className="space-y-2 p-4">
          <div className="flex items-center justify-between gap-2">
            <div className="text-xs font-medium tracking-wide text-muted-foreground uppercase">{t("agentArea.perf.selfEvo.label")}</div>
            <Switch
              checked={agent?.self_evolution_enabled ?? false}
              onCheckedChange={(v) => toggleSelfEvo(v)}
              disabled={!agent}
            />
          </div>
          <p className="text-xs text-muted-foreground">{t("agentArea.perf.selfEvo.help")}</p>
          <div className="text-xs text-muted-foreground">
            {reflections.length > 0
              ? t("agentArea.perf.selfEvo.lastAnalysis", {
                  date: fmtDate(reflections[0].created_at),
                  trigger: triggerLabels[reflections[0].trigger] ?? reflections[0].trigger,
                })
              : t("agentArea.perf.selfEvo.noAnalysisYet")}
          </div>
        </Card>
      </div>

      {agentId ? <AgentKPISection agentId={agentId} /> : null}

      {enabledKPIs.length > 0 ? (
        <section className="space-y-2">
          <h3 className="text-sm font-semibold">{t("agentArea.perf.kpiStatus.heading")}</h3>
          <div className="grid gap-3 lg:grid-cols-3">
            {enabledKPIs.map((k) => (
              <KPICard key={k.id} kpi={k} result={kpiResultByID.get(k.id)} />
            ))}
          </div>
        </section>
      ) : null}

      <section className="space-y-2">
        <h3 className="text-sm font-semibold">{t("agentArea.perf.timeline.heading")}</h3>
        {events.length === 0 ? (
          <p className="text-sm text-muted-foreground">{t("agentArea.perf.timeline.empty")}</p>
        ) : (
          <div className="space-y-2">
            {events.map((e) => {
              const impact = impactMeta[e.impact] ?? impactMeta.pending;
              const expanded = expandedEvent === e.id;
              return (
                <Card key={e.id} className="p-3">
                  <button
                    type="button"
                    className="flex w-full items-center justify-between gap-3 text-left"
                    onClick={() => setExpandedEvent(expanded ? null : e.id)}
                  >
                    <div className="min-w-0">
                      <span className="text-sm font-medium">{changeTypeLabels[e.change_type] ?? e.change_type}</span>
                      <span className="ml-2 truncate text-sm text-muted-foreground">{e.target_name}</span>
                    </div>
                    <div className="flex shrink-0 items-center gap-2">
                      <Badge className={impact.className}>{impact.label}</Badge>
                      <span className="text-xs text-muted-foreground">{fmtDate(e.created_at)}</span>
                    </div>
                  </button>
                  {expanded ? (
                    <div className="mt-3 grid gap-2 border-t pt-3 lg:grid-cols-2">
                      <div>
                        <div className="mb-1 text-xs font-medium text-muted-foreground">{t("agentArea.perf.timeline.before")}</div>
                        <pre className="max-h-48 overflow-auto rounded bg-muted p-2 text-xs whitespace-pre-wrap">
                          {e.before ? JSON.stringify(e.before, null, 2) : "—"}
                        </pre>
                      </div>
                      <div>
                        <div className="mb-1 text-xs font-medium text-muted-foreground">{t("agentArea.perf.timeline.after")}</div>
                        <pre className="max-h-48 overflow-auto rounded bg-muted p-2 text-xs whitespace-pre-wrap">
                          {e.after ? JSON.stringify(e.after, null, 2) : "—"}
                        </pre>
                      </div>
                    </div>
                  ) : null}
                </Card>
              );
            })}
          </div>
        )}
      </section>

      <section className="space-y-2">
        <h3 className="text-sm font-semibold">{t("agentArea.perf.reflections.heading")}</h3>
        {reflections.length === 0 ? (
          <p className="text-sm text-muted-foreground">{t("agentArea.perf.reflections.empty")}</p>
        ) : (
          <div className="space-y-2">
            {reflections.map((r) => (
              <Card key={r.id} className="space-y-1 p-3">
                <div className="flex items-center justify-between gap-2">
                  <div className="flex items-center gap-2">
                    <Badge
                      className={
                        r.status === "completed"
                          ? "bg-emerald-500/15 text-emerald-600 dark:text-emerald-400"
                          : r.status === "failed"
                            ? "bg-red-500/15 text-red-600 dark:text-red-400"
                            : "bg-amber-500/15 text-amber-600 dark:text-amber-400"
                      }
                    >
                      {r.status === "completed"
                        ? t("agentArea.perf.reflections.statusCompleted")
                        : r.status === "failed"
                          ? t("agentArea.perf.reflections.statusFailed")
                          : t("agentArea.perf.reflections.statusRunning")}
                    </Badge>
                    <span className="text-xs text-muted-foreground">{triggerLabels[r.trigger] ?? r.trigger}</span>
                  </div>
                  <span className="text-xs text-muted-foreground">{fmtDate(r.created_at)}</span>
                </div>
                {r.summary ? <p className="text-sm whitespace-pre-wrap">{r.summary}</p> : null}
                {r.error ? <p className="text-xs text-red-500">{r.error}</p> : null}
              </Card>
            ))}
          </div>
        )}
      </section>

      <section className="space-y-2">
        <div className="flex items-center justify-between gap-2">
          <h3 className="text-sm font-semibold">{t("agentArea.perf.memory.heading", { count: memoryCount })}</h3>
          <Button variant="outline" size="sm" asChild>
            <Link to={`/agents/${agentId}/memory`}>{t("agentArea.perf.memory.manage")}</Link>
          </Button>
        </div>
        {memoryCount === 0 ? (
          <p className="text-sm text-muted-foreground">{t("agentArea.perf.memory.empty")}</p>
        ) : (
          <p className="text-sm text-muted-foreground">{t("agentArea.perf.memory.summary", { count: memoryCount })}</p>
        )}
      </section>

      <section className="space-y-2">
        <h3 className="text-sm font-semibold">{t("agentArea.perf.scoreEvents.heading")}</h3>
        {(perf?.events ?? []).length === 0 ? (
          <p className="text-sm text-muted-foreground">{t("agentArea.perf.scoreEvents.empty")}</p>
        ) : (
          <div className="overflow-x-auto rounded-md border">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b bg-muted/50 text-left text-xs text-muted-foreground">
                  <th className="p-2 font-medium">{t("agentArea.perf.scoreEvents.colDate")}</th>
                  <th className="p-2 font-medium">{t("agentArea.perf.scoreEvents.colEvent")}</th>
                  <th className="p-2 font-medium text-right">{t("agentArea.perf.scoreEvents.colDelta")}</th>
                  <th className="p-2 font-medium text-right">{t("agentArea.perf.scoreEvents.colScore")}</th>
                  <th className="p-2 font-medium">{t("agentArea.perf.scoreEvents.colReason")}</th>
                </tr>
              </thead>
              <tbody>
                {(perf?.events ?? []).map((e) => (
                  <tr key={e.id} className="border-b last:border-0">
                    <td className="p-2 whitespace-nowrap text-muted-foreground">{fmtDate(e.created_at)}</td>
                    <td className="p-2">{eventTypeLabels[e.event_type] ?? e.event_type}</td>
                    <td className={`p-2 text-right tabular-nums ${e.delta < 0 ? "text-red-500" : "text-emerald-500"}`}>
                      {e.delta > 0 ? `+${e.delta}` : e.delta}
                    </td>
                    <td className="p-2 text-right tabular-nums">{e.score_after}</td>
                    <td className="p-2 text-muted-foreground">{e.reason ?? ""}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>
    </PageContent>
  );
}
