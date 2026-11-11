import { ChevronDown, ChevronRight, CornerDownRight, History, OctagonAlert, Undo2 } from "lucide-react";
import { useMemo, useState } from "react";
import type {
  ReviewFindingState,
  ReviewHistory,
  ReviewHistoryFinding,
  ReviewHistoryMove,
  ReviewHistoryRound,
  ReviewHistorySummary,
  ReviewRoundResult,
} from "@/api";
import { ExternalLinkAnchor } from "@/components/board/review/ExternalLinkAnchor";
import { RoundCoverageView } from "@/components/board/review/ReviewCoverage";
import { Badge, type BadgeProps } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Checkbox } from "@/components/ui/checkbox";
import { Label } from "@/components/ui/label";
import { useI18n } from "@/hooks/useI18n";
import { formatRelativeTime } from "@/lib/utils";

const base = "boardArea.components.taskDetail.reviewHistory";

const OUTCOME_VARIANT: Record<ReviewRoundResult, BadgeProps["variant"]> = {
  open: "info",
  approved: "success",
  passed: "success",
  changes_requested: "destructive",
  failed: "destructive",
  closed: "outline",
};

const STATUS_VARIANT: Record<ReviewFindingState, BadgeProps["variant"]> = {
  open: "destructive",
  reopened: "destructive",
  fixed: "success",
  withdrawn: "outline",
};

const KNOWN_GATES = new Set(["code_review", "in_qa"]);

export function summaryLine(s: ReviewHistorySummary, t: ReturnType<typeof useI18n>["t"]): string {
  const counted = (name: string, count: number) => t(`${base}.summary.${name}${count === 1 ? "One" : "Many"}`, { count });
  const parts = [
    counted("rounds", s.rounds),
    counted("raised", s.findings_raised),
    t(`${base}.summary.fixed`, { count: s.fixed }),
    t(`${base}.summary.open`, { count: s.open }),
    counted("bounces", s.bounces),
  ];
  if (s.loop_guard_stops > 0) parts.push(counted("loopGuard", s.loop_guard_stops));
  return parts.join(" · ");
}

interface ReviewHistoryPanelProps {
  history: ReviewHistory;
  /** Opens the run's trace in the task's runs section. */
  onOpenRun?: (runId: string) => void;
}

/** The task's review loop as one timeline, grouped by round. */
export function ReviewHistoryPanel({ history, onOpenRun }: ReviewHistoryPanelProps) {
  const { t } = useI18n();
  const [openOnly, setOpenOnly] = useState(false);
  const byId = useMemo(() => new Map(history.findings.map((f) => [f.id, f])), [history.findings]);
  const rounds = useMemo(() => [...history.rounds].sort((a, b) => b.seq - a.seq), [history.rounds]);

  if (history.rounds.length === 0) {
    return <p className="text-xs text-muted-foreground">{t(`${base}.empty`)}</p>;
  }
  return (
    <div className="space-y-3" data-testid="review-history">
      <p className="text-xs font-medium" data-testid="review-history-summary">
        {summaryLine(history.summary, t)}
      </p>
      <div className="flex items-center gap-2">
        <Checkbox id="review-history-open-only" checked={openOnly} onCheckedChange={(v) => setOpenOnly(v === true)} />
        <Label htmlFor="review-history-open-only" className="text-xs text-muted-foreground">
          {t(`${base}.openOnly`)}
        </Label>
      </div>
      {rounds.map((round) => (
        <RoundCard key={round.seq} round={round} byId={byId} openOnly={openOnly} onOpenRun={onOpenRun} />
      ))}
    </div>
  );
}

function RoundCard({
  round,
  byId,
  openOnly,
  onOpenRun,
}: {
  round: ReviewHistoryRound;
  byId: Map<string, ReviewHistoryFinding>;
  openOnly: boolean;
  onOpenRun?: (runId: string) => void;
}) {
  const { t, lang } = useI18n();
  const gate = KNOWN_GATES.has(round.gate) ? t(`${base}.gates.${round.gate}`) : t(`${base}.gates.other`);
  const touched = [...round.raised, ...round.still_open, ...round.reopened, ...round.fixed];
  const findings = touched
    .map((id) => byId.get(id))
    .filter((f): f is ReviewHistoryFinding => !!f)
    .filter((f) => !openOnly || f.status === "open" || f.status === "reopened");
  return (
    <Card className="space-y-2 rounded-md p-2 shadow-none" data-testid="review-round">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <span className="text-sm font-medium">{t(`${base}.round`, { gate, number: round.number })}</span>
        <span className="flex items-center gap-1">
          {round.loop_guard && (
            <Badge variant="warning" className="gap-1">
              <OctagonAlert className="h-3 w-3" />
              {t(`${base}.loopGuard`)}
            </Badge>
          )}
          <Badge variant={OUTCOME_VARIANT[round.outcome]}>{t(`${base}.outcome.${round.outcome}`)}</Badge>
        </span>
      </div>
      <p className="text-caption text-muted-foreground">
        {formatRelativeTime(round.entered_at, lang)}
        {round.head_sha &&
          " · " +
            (round.from_sha
              ? t(`${base}.shaRange`, { from: round.from_sha.slice(0, 7), head: round.head_sha.slice(0, 7) })
              : t(`${base}.shaAt`, { head: round.head_sha.slice(0, 7) }))}
      </p>
      <ul className="space-y-1">
        {round.reviewers.map((rv, i) => (
          <li key={`${rv.agent_id ?? "human"}-${i}`} className="flex items-center justify-between gap-2 text-xs">
            <span className="min-w-0 truncate">{rv.human ? t(`${base}.aPerson`) : rv.agent_name}</span>
            <span className="flex shrink-0 items-center gap-1">
              {rv.run_id && onOpenRun && (
                <Button type="button" variant="ghost" size="sm" className="h-5 px-1 text-caption" onClick={() => onOpenRun(rv.run_id!)}>
                  {t(`${base}.openRun`)}
                </Button>
              )}
              {rv.verdict !== "pending" && (
                <Badge variant={rv.verdict === "approve" ? "success" : "destructive"}>
                  {t(`boardArea.components.taskDetail.codeReview.verdict.${rv.verdict}`)}
                </Badge>
              )}
            </span>
          </li>
        ))}
      </ul>

      {findings.length > 0 ? (
        <ul className="space-y-1.5" data-testid="round-findings">
          {findings.map((f) => (
            <FindingRow key={f.id} finding={f} round={round} />
          ))}
        </ul>
      ) : (
        !openOnly &&
        touched.length === 0 && <p className="text-caption text-muted-foreground">{t(`${base}.noFindings`)}</p>
      )}

      {round.moves.length > 0 && <Moves moves={round.moves} />}

      {round.fix_runs.length > 0 && (
        <div className="space-y-1">
          <p className="text-caption font-medium text-muted-foreground">{t(`${base}.answeredBy`)}</p>
          <ul className="space-y-1">
            {round.fix_runs.map((run) => (
              <li key={run.run_id} className="flex items-center justify-between gap-2 text-xs">
                <span className="min-w-0 truncate" title={run.summary}>
                  {run.agent_name}
                  {run.head_sha && (
                    <span className="font-mono text-caption text-muted-foreground">
                      {" "}
                      {run.base_sha ? `${run.base_sha.slice(0, 7)} → ` : ""}
                      {run.head_sha.slice(0, 7)}
                    </span>
                  )}
                  {run.files_changed ? (
                    <span className="text-caption text-muted-foreground"> · {t(`${base}.filesChanged`, { count: run.files_changed })}</span>
                  ) : null}
                </span>
                {onOpenRun && (
                  <Button type="button" variant="ghost" size="sm" className="h-5 shrink-0 px-1 text-caption" onClick={() => onOpenRun(run.run_id)}>
                    {run.has_trace ? t(`${base}.openTrace`) : t(`${base}.openRun`)}
                  </Button>
                )}
              </li>
            ))}
          </ul>
        </div>
      )}

      {round.coverage && <RoundCoverageView coverage={round.coverage} />}
    </Card>
  );
}

function Moves({ moves }: { moves: ReviewHistoryMove[] }) {
  const { t, lang } = useI18n();
  return (
    <ul className="space-y-0.5 border-l border-border pl-2" data-testid="round-moves">
      {moves.map((m, i) => (
        <li key={`${m.at}-${i}`} className="flex flex-wrap items-center gap-1 text-caption text-muted-foreground">
          {m.bounce ? <Undo2 className="h-3 w-3 text-destructive" /> : <CornerDownRight className="h-3 w-3" />}
          <span className={m.bounce ? "font-medium text-foreground" : undefined}>
            {t(`${base}.move`, { from: m.from, to: m.to })}
          </span>
          <span>
            · {m.actor === "agent" && m.agent_name ? m.agent_name : t(`${base}.actors.${m.actor || "system"}`)}
            {" · "}
            {formatRelativeTime(m.at, lang)}
          </span>
          {m.bounce && <Badge variant="destructive">{t(`${base}.bounce`)}</Badge>}
          {m.loop_guard && <Badge variant="warning">{t(`${base}.loopGuard`)}</Badge>}
        </li>
      ))}
    </ul>
  );
}

function FindingRow({ finding, round }: { finding: ReviewHistoryFinding; round: ReviewHistoryRound }) {
  const { t } = useI18n();
  const [open, setOpen] = useState(false);
  const where = finding.file ? (finding.line ? `${finding.file}:${finding.line}` : finding.file) : "";
  const kind = round.raised.includes(finding.id)
    ? "raised"
    : round.fixed.includes(finding.id)
      ? "fixed"
      : round.reopened.includes(finding.id)
        ? "reopened"
        : "still_open";
  return (
    <li className="rounded-md border border-border bg-card p-1.5" data-testid="review-finding">
      <button
        type="button"
        className="flex w-full items-start gap-1 text-left"
        aria-expanded={open}
        onClick={() => setOpen((v) => !v)}
      >
        {open ? <ChevronDown className="mt-0.5 h-3 w-3 shrink-0" /> : <ChevronRight className="mt-0.5 h-3 w-3 shrink-0" />}
        <span className="min-w-0 flex-1 text-xs">{finding.message}</span>
        <Badge variant={STATUS_VARIANT[finding.status]} className="shrink-0">
          {finding.status === "fixed" && finding.fixed_round
            ? t(`${base}.status.fixedIn`, { round: finding.fixed_round })
            : t(`${base}.status.${finding.status}`)}
        </Badge>
      </button>
      <div className="ml-4 mt-0.5 flex flex-wrap items-center gap-1">
        <Badge variant="outline">{t(`${base}.trail.${kind}`)}</Badge>
        {finding.severity && <Badge variant="outline">{t(`${base}.severity.${finding.severity}`)}</Badge>}
        {finding.category && finding.category !== "acceptance_criterion" && <Badge variant="outline">{finding.category}</Badge>}
        {finding.confidence === "low" && <Badge variant="warning">{t(`${base}.lowConfidence`)}</Badge>}
      </div>
      {open && (
        <div className="ml-4 mt-1 space-y-1 text-xs">
          {finding.criterion_text && <p className="text-muted-foreground">{t(`${base}.criterion`, { text: finding.criterion_text })}</p>}
          {where &&
            (finding.file_url ? (
              <ExternalLinkAnchor href={finding.file_url} className="inline-flex items-center gap-1 break-all font-mono text-caption text-primary hover:underline">
                {where}
              </ExternalLinkAnchor>
            ) : (
              <p className="break-all font-mono text-caption text-muted-foreground">{where}</p>
            ))}
          {finding.agent_name && <p className="text-caption text-muted-foreground">{t(`${base}.raisedBy`, { name: finding.agent_name })}</p>}
          <ol className="space-y-0.5">
            {finding.trail.map((step, i) => (
              <li key={i} className="text-caption text-muted-foreground">
                {t(`${base}.trailStep`, { round: step.round, state: t(`${base}.trail.${step.state}`) })}
              </li>
            ))}
          </ol>
        </div>
      )}
    </li>
  );
}

export function ReviewHistoryToggle({
  history,
  onOpenRun,
}: {
  history: ReviewHistory | null;
  onOpenRun?: (runId: string) => void;
}) {
  const { t } = useI18n();
  const [open, setOpen] = useState(false);
  if (!history || history.rounds.length === 0) return null;
  return (
    <section className="space-y-2" data-testid="review-history-section">
      <Label className="flex items-center gap-2 text-xs text-muted-foreground">
        <History className="h-3.5 w-3.5" />
        {t(`${base}.title`)}
      </Label>
      <p className="text-xs">{summaryLine(history.summary, t)}</p>
      <Button type="button" variant="ghost" size="sm" className="h-6 gap-1 px-1 text-muted-foreground" aria-expanded={open} onClick={() => setOpen((v) => !v)}>
        {open ? <ChevronDown className="h-3 w-3" /> : <ChevronRight className="h-3 w-3" />}
        {open ? t(`${base}.hide`) : t(`${base}.show`)}
      </Button>
      {open && <ReviewHistoryPanel history={history} onOpenRun={onOpenRun} />}
    </section>
  );
}
