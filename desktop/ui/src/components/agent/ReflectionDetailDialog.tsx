import { useState } from "react";
import type {
  AgentKPI,
  AgentReflection,
  EvolutionEvent,
  ProbeVerdict,
  ReflectionChange,
  ReflectionGate,
  ReflectionLesson,
  ReflectionOutcome,
} from "@/api";
import { Accordion, AccordionContent, AccordionItem, AccordionTrigger } from "@/components/ui/accordion";
import { Badge, type BadgeProps } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Notice } from "@/components/ui/notice";
import { ScrollArea } from "@/components/ui/scroll-area";
import { MarkdownContent } from "@/components/markdown/MarkdownContent";
import { useI18n } from "@/hooks/useI18n";
import { reflectionStatusLabel, reflectionStatusVariant, reflectionTriggerLabel } from "@/components/agent/ReflectionHistory";

type TFunc = (key: string, params?: Record<string, string | number>) => string;

function fmtDate(iso: string) {
  return new Date(iso).toLocaleString("tr-TR", { day: "2-digit", month: "2-digit", year: "numeric", hour: "2-digit", minute: "2-digit" });
}

const outcomeVariant: Record<ReflectionOutcome, BadgeProps["variant"]> = {
  applied: "success",
  rolled_back: "destructive",
  failed: "destructive",
  rejected: "warning",
  skipped: "warning",
  not_applied: "warning",
  not_adopted: "secondary",
};

const verdictVariant: Record<ProbeVerdict, BadgeProps["variant"]> = {
  helped: "success",
  hurt: "destructive",
  no_effect: "secondary",
  confounded: "info",
};

function changeKindLabel(t: TFunc, change: ReflectionChange): string {
  const key = `agentArea.perf.detail.kind.${change.kind}_${change.action}`;
  const label = t(key);
  return label === key ? `${change.kind} ${change.action}` : label;
}

interface EventContentShape {
  name?: string;
  content?: string;
  description?: string;
  priority?: number;
}

function asContentShape(v: unknown): EventContentShape | null {
  return v && typeof v === "object" ? (v as EventContentShape) : null;
}

type RowKind = "score" | "percent" | "count";

interface CompareRowSpec {
  key: string;
  label: string;
  before?: number;
  after?: number;
  beforeBudget?: number;
  afterBudget?: number;
  kind: RowKind;
}

function formatCell(v: number | undefined, kind: RowKind, budget?: number): string {
  if (v == null) return "–";
  if (kind === "percent") return `${Math.round(v * 100)}%`;
  if (kind === "score") return v.toFixed(1);
  return budget != null ? `${v} / ${budget}` : `${v}`;
}

function CompareTable({ rows }: { rows: CompareRowSpec[] }) {
  const { t } = useI18n();
  const visible = rows.filter((r) => r.before != null || r.after != null);
  if (visible.length === 0) return null;
  return (
    <div className="overflow-x-auto rounded-md border">
      <table className="w-full text-sm">
        <thead>
          <tr className="border-b bg-muted/50 text-left text-xs text-muted-foreground">
            <th className="p-2 font-medium" />
            <th className="p-2 text-right font-medium">{t("agentArea.perf.detail.colBefore")}</th>
            <th className="p-2 text-right font-medium">{t("agentArea.perf.detail.colAfter")}</th>
            <th className="p-2 text-right font-medium">{t("agentArea.perf.detail.colDelta")}</th>
          </tr>
        </thead>
        <tbody>
          {visible.map((r) => {
            const delta = r.before != null && r.after != null ? r.after - r.before : null;
            // Attainment is already direction-normalized (0..1, higher always
            // better) even when the underlying KPI metric is lower_better, so
            // score/percent deltas can be colored the same way unconditionally.
            const deltaColor =
              delta == null || r.kind === "count"
                ? "text-foreground"
                : delta >= 0
                  ? "text-emerald-600 dark:text-emerald-400"
                  : "text-red-500";
            const deltaText =
              delta == null
                ? "–"
                : r.kind === "percent"
                  ? `${delta >= 0 ? "+" : ""}${Math.round(delta * 100)}pp`
                  : `${delta >= 0 ? "+" : ""}${r.kind === "score" ? delta.toFixed(1) : delta}`;
            return (
              <tr key={r.key} className="border-b last:border-0">
                <td className="p-2">{r.label}</td>
                <td className="p-2 text-right tabular-nums text-muted-foreground">{formatCell(r.before, r.kind, r.beforeBudget)}</td>
                <td className="p-2 text-right tabular-nums">{formatCell(r.after, r.kind, r.afterBudget)}</td>
                <td className={`p-2 text-right tabular-nums font-medium ${deltaColor}`}>{deltaText}</td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}

function ChangeRow({ change, event }: { change: ReflectionChange; event?: EvolutionEvent }) {
  const { t } = useI18n();
  const [expanded, setExpanded] = useState(false);

  const before = asContentShape(event?.before);
  const after = asContentShape(event?.after);
  const hasContent = typeof before?.content === "string" || typeof after?.content === "string";

  return (
    <div className="space-y-2 rounded-md border p-3">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="min-w-0">
          <span className="text-sm font-medium">{changeKindLabel(t, change)}</span>
          <span className="ml-2 truncate text-sm text-muted-foreground">{change.name}</span>
        </div>
        <div className="flex flex-wrap items-center gap-1.5">
          {change.dimension ? (
            <Badge variant="outline">{t(`agentArea.perf.detail.dimension.${change.dimension}`)}</Badge>
          ) : null}
          {change.verdict ? (
            <Badge variant={verdictVariant[change.verdict]} title={t(`agentArea.perf.detail.verdictTitle.${change.verdict}`)}>
              {t(`agentArea.perf.detail.verdict.${change.verdict}`)}
            </Badge>
          ) : null}
          <Badge variant={outcomeVariant[change.outcome]}>{t(`agentArea.perf.detail.outcome.${change.outcome}`)}</Badge>
        </div>
      </div>
      {change.reason ? (
        <p className="text-xs text-muted-foreground">
          <span className="font-medium text-foreground">{t("agentArea.perf.detail.reasonLabel")}</span> {change.reason}
        </p>
      ) : null}
      {change.detail ? (
        <p className="text-xs text-muted-foreground">
          <span className="font-medium text-foreground">{t("agentArea.perf.detail.detailLabel")}</span> {change.detail}
        </p>
      ) : null}
      {change.event_id ? (
        <div>
          <Button variant="ghost" size="sm" className="h-7 px-2 text-xs" onClick={() => setExpanded((v) => !v)}>
            {expanded ? t("agentArea.perf.detail.hideContentDiff") : t("agentArea.perf.detail.showContentDiff")}
          </Button>
          {expanded ? (
            !event ? (
              <p className="text-xs text-muted-foreground">{t("agentArea.perf.detail.noEvolutionEvent")}</p>
            ) : hasContent ? (
              <div className="mt-2 grid gap-2 lg:grid-cols-2">
                <div>
                  <div className="mb-1 text-xs font-medium text-muted-foreground">{t("agentArea.perf.detail.colBefore")}</div>
                  <div className="max-h-48 overflow-auto rounded bg-muted p-2 text-xs whitespace-pre-wrap">
                    {before?.content ?? "—"}
                  </div>
                </div>
                <div>
                  <div className="mb-1 text-xs font-medium text-muted-foreground">{t("agentArea.perf.detail.colAfter")}</div>
                  <div className="max-h-48 overflow-auto rounded bg-muted p-2 text-xs whitespace-pre-wrap">
                    {after?.content ?? "—"}
                  </div>
                </div>
              </div>
            ) : (
              <div className="mt-2 grid gap-2 lg:grid-cols-2">
                <pre className="max-h-48 overflow-auto rounded bg-muted p-2 text-xs whitespace-pre-wrap">
                  {event.before ? JSON.stringify(event.before, null, 2) : "—"}
                </pre>
                <pre className="max-h-48 overflow-auto rounded bg-muted p-2 text-xs whitespace-pre-wrap">
                  {event.after ? JSON.stringify(event.after, null, 2) : "—"}
                </pre>
              </div>
            )
          ) : null}
        </div>
      ) : null}
    </div>
  );
}

function ProbeSection({ gate }: { gate: ReflectionGate }) {
  const { t } = useI18n();
  const variants = gate.variants ?? [];
  const cases = gate.cases ?? [];
  const headline = gate.unmeasured
    ? t("agentArea.perf.detail.probe.unmeasured")
    : gate.adopted
      ? t("agentArea.perf.detail.probe.adopted", { variant: t(`agentArea.perf.detail.probe.variant.${gate.adopted}`) })
      : t("agentArea.perf.detail.probe.nothingAdopted");
  return (
    <section className="space-y-2">
      <h4 className="text-sm font-semibold">{t("agentArea.perf.detail.probe.heading")}</h4>
      <p className="text-sm">{headline}</p>
      {gate.reason ? (
        <p className="text-xs text-muted-foreground">
          <span className="font-medium text-foreground">{t("agentArea.perf.detail.detailLabel")}</span> {gate.reason}
        </p>
      ) : null}
      {variants.length > 0 ? (
        <div className="overflow-x-auto rounded-md border">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b bg-muted/50 text-left text-xs text-muted-foreground">
                <th className="p-2 font-medium">{t("agentArea.perf.detail.probe.colVariant")}</th>
                <th className="p-2 text-right font-medium">{t("agentArea.perf.detail.probe.colChanges")}</th>
                <th className="p-2 text-right font-medium">{t("agentArea.perf.detail.probe.colBetter")}</th>
                <th className="p-2 text-right font-medium">{t("agentArea.perf.detail.probe.colWorse")}</th>
                <th className="p-2 text-right font-medium">{t("agentArea.perf.detail.probe.colEven")}</th>
                <th className="p-2 text-right font-medium">{t("agentArea.perf.detail.probe.colGolden")}</th>
              </tr>
            </thead>
            <tbody>
              {variants.map((v) => (
                <tr key={v.label} className="border-b last:border-0">
                  <td className="p-2">
                    <span className={v.label === gate.adopted ? "font-medium" : undefined}>
                      {t(`agentArea.perf.detail.probe.variant.${v.label}`)}
                    </span>
                    {v.label === gate.adopted ? (
                      <Badge variant="success" className="ml-2">
                        {t("agentArea.perf.detail.probe.adoptedBadge")}
                      </Badge>
                    ) : null}
                  </td>
                  <td className="p-2 text-right tabular-nums">{v.changes}</td>
                  <td className="p-2 text-right tabular-nums text-emerald-600 dark:text-emerald-400">{v.wins}</td>
                  <td className="p-2 text-right tabular-nums text-red-500">{v.losses}</td>
                  <td className="p-2 text-right tabular-nums text-muted-foreground">{v.ties}</td>
                  <td className="p-2 text-right tabular-nums">{v.golden_losses}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : null}
      {cases.length > 0 ? (
        <div className="space-y-1">
          <div className="text-xs font-medium text-muted-foreground">
            {t("agentArea.perf.detail.probe.casesHeading", { count: cases.length })}
          </div>
          <ul className="space-y-1 text-xs text-muted-foreground">
            {cases.map((c, i) => (
              <li key={`${c.kind}-${i}`} className="flex flex-wrap items-center gap-2">
                <Badge variant="outline">{t(`agentArea.perf.detail.probe.caseKind.${c.kind}`)}</Badge>
                <span className="min-w-0 truncate text-foreground">{c.title}</span>
                {!c.measured ? <span>· {t("agentArea.perf.detail.probe.notMeasured")}</span> : null}
              </li>
            ))}
          </ul>
        </div>
      ) : null}
    </section>
  );
}

function LessonSection({ lesson }: { lesson: ReflectionLesson }) {
  const { t } = useI18n();
  return (
    <section className="space-y-1">
      <h4 className="text-sm font-semibold">{t("agentArea.perf.detail.lesson.heading")}</h4>
      <p className="text-sm">
        <span className="font-medium">{t("agentArea.perf.detail.lesson.cue")}</span> {lesson.cue}
      </p>
      <p className="text-sm">{lesson.lesson}</p>
      <p className="text-xs text-muted-foreground">
        <span className="font-medium text-foreground">{t("agentArea.perf.detail.lesson.evidence")}</span> {lesson.evidence}
        {lesson.confidence ? <> · {t("agentArea.perf.detail.lesson.confidence", { level: lesson.confidence })}</> : null}
      </p>
    </section>
  );
}

interface ReflectionDetailDialogProps {
  reflection: AgentReflection;
  events: EvolutionEvent[];
  kpis: AgentKPI[];
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

export function ReflectionDetailDialog({ reflection, events, kpis, open, onOpenChange }: ReflectionDetailDialogProps) {
  const { t } = useI18n();
  const decision = reflection.decision;
  const snapshot = reflection.performance_snapshot;
  const baseline = decision?.baseline;

  const kpiNameByKey = new Map(kpis.map((k) => [k.metric_key, k.name || k.metric_key]));
  const kpiKeys = Array.from(new Set([...Object.keys(baseline?.kpis ?? {}), ...Object.keys(snapshot?.kpis ?? {})]));

  const beforeGoldenRate = decision?.gate?.before_rate ?? snapshot?.golden_pass_rate_before ?? baseline?.golden_pass_rate;
  const afterGoldenRate = decision?.gate?.after_rate ?? snapshot?.golden_pass_rate;

  const rows: CompareRowSpec[] = [
    { key: "score", label: t("agentArea.perf.detail.compareRow.score"), before: baseline?.score, after: snapshot?.score, kind: "score" },
    {
      key: "kpi_composite",
      label: t("agentArea.perf.detail.compareRow.kpiComposite"),
      before: baseline?.kpi_composite,
      after: snapshot?.kpi_composite,
      kind: "score",
    },
    {
      key: "golden_pass_rate",
      label: t("agentArea.perf.detail.compareRow.goldenPassRate"),
      before: beforeGoldenRate,
      after: afterGoldenRate,
      kind: "percent",
    },
    ...kpiKeys.map((key) => ({
      key: `kpi_${key}`,
      label: kpiNameByKey.get(key) ?? key,
      before: baseline?.kpis?.[key],
      after: snapshot?.kpis?.[key],
      kind: "percent" as const,
    })),
    {
      key: "skills",
      label: t("agentArea.perf.detail.compareRow.skills"),
      before: decision?.catalog_before.skills,
      after: decision?.catalog_after.skills,
      beforeBudget: decision?.catalog_before.skill_budget,
      afterBudget: decision?.catalog_after.skill_budget,
      kind: "count",
    },
    {
      key: "rules",
      label: t("agentArea.perf.detail.compareRow.rules"),
      before: decision?.catalog_before.rules,
      after: decision?.catalog_after.rules,
      beforeBudget: decision?.catalog_before.rule_budget,
      afterBudget: decision?.catalog_after.rule_budget,
      kind: "count",
    },
    {
      key: "memories",
      label: t("agentArea.perf.detail.compareRow.memories"),
      before: decision?.catalog_before.memories,
      after: decision?.catalog_after.memories,
      kind: "count",
    },
  ];

  const analysisLong = (decision?.analysis?.length ?? 0) > 500;
  const hasRawOutput = (reflection.raw_output?.length ?? 0) > 0;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="flex h-[85vh] w-[95vw] max-w-4xl flex-col gap-0 overflow-hidden p-0">
        <DialogHeader className="border-b px-6 py-4">
          <DialogTitle>{t("agentArea.perf.detail.title", { date: fmtDate(reflection.created_at) })}</DialogTitle>
          <div className="flex flex-wrap items-center gap-2 text-caption text-muted-foreground">
            <Badge variant={reflectionStatusVariant(reflection.status)}>{reflectionStatusLabel(t, reflection.status)}</Badge>
            <span>{reflectionTriggerLabel(t, reflection.trigger)}</span>
          </div>
        </DialogHeader>
        <ScrollArea className="flex-1">
          <div className="space-y-6 px-6 py-4">
            <section className="space-y-2">
              <h4 className="text-sm font-semibold">{t("agentArea.perf.detail.tabSummary")}</h4>
              {decision?.legacy ? <Notice variant="info" title={t("agentArea.perf.detail.legacyNotice")} /> : null}
              {decision?.self_assessment ? (
                <p className="text-sm whitespace-pre-wrap">{decision.self_assessment}</p>
              ) : reflection.summary ? (
                <p className="text-sm whitespace-pre-wrap">{reflection.summary}</p>
              ) : null}
              {reflection.status === "failed" && reflection.error ? (
                <p className="text-sm text-destructive">{reflection.error}</p>
              ) : null}
              {!decision && reflection.status === "completed" ? (
                <p className="text-xs text-muted-foreground">{t("agentArea.perf.detail.noDecision")}</p>
              ) : null}
            </section>

            {decision ? (
              <>
                <section className="space-y-2">
                  <h4 className="text-sm font-semibold">{t("agentArea.perf.detail.compareHeading")}</h4>
                  <CompareTable rows={rows} />
                </section>

                <section className="space-y-2">
                  <h4 className="text-sm font-semibold">{t("agentArea.perf.detail.decisionsHeading")}</h4>
                  {(decision.changes ?? []).length === 0 ? (
                    <p className="text-sm text-muted-foreground">{t("agentArea.perf.detail.decisionsEmpty")}</p>
                  ) : (
                    <div className="space-y-2">
                      {(decision.changes ?? []).map((c, i) => (
                        <ChangeRow key={`${c.kind}-${c.name}-${i}`} change={c} event={events.find((e) => e.id === c.event_id)} />
                      ))}
                    </div>
                  )}
                </section>

                {decision.gate?.probed ? <ProbeSection gate={decision.gate} /> : null}

                {decision.lesson ? <LessonSection lesson={decision.lesson} /> : null}

                {decision.gate && !decision.gate.probed ? (
                  <section className="space-y-1">
                    <h4 className="text-sm font-semibold">{t("agentArea.perf.detail.gateHeading")}</h4>
                    <p className="text-sm text-muted-foreground">
                      {Math.round((decision.gate.before_rate ?? 0) * 100)}% → {Math.round((decision.gate.after_rate ?? 0) * 100)}% ·{" "}
                      {decision.gate.keep
                        ? t("agentArea.perf.detail.gateKept")
                        : t("agentArea.perf.detail.gateRolledBack", { count: decision.gate.rolled_back })}
                      {decision.gate.reason ? (
                        <>
                          {" "}
                          · <span className="font-medium text-foreground">{t("agentArea.perf.detail.gateReasonLabel")}</span>{" "}
                          {decision.gate.reason}
                        </>
                      ) : null}
                    </p>
                  </section>
                ) : null}

                {decision.analysis ? (
                  <Accordion type="single" defaultValue={analysisLong ? "" : "analysis"}>
                    <AccordionItem value="analysis">
                      <AccordionTrigger>{t("agentArea.perf.detail.analysisHeading")}</AccordionTrigger>
                      <AccordionContent>
                        <MarkdownContent content={decision.analysis} />
                      </AccordionContent>
                    </AccordionItem>
                  </Accordion>
                ) : null}
              </>
            ) : null}

            {hasRawOutput ? (
              <Accordion type="single" defaultValue="">
                <AccordionItem value="raw">
                  <AccordionTrigger>{t("agentArea.perf.detail.rawOutputHeading")}</AccordionTrigger>
                  <AccordionContent>
                    <pre className="max-h-64 overflow-auto rounded bg-muted p-2 font-mono text-xs whitespace-pre-wrap">
                      {reflection.raw_output}
                    </pre>
                  </AccordionContent>
                </AccordionItem>
              </Accordion>
            ) : null}
          </div>
        </ScrollArea>
      </DialogContent>
    </Dialog>
  );
}
