import { Check, ChevronDown, ChevronRight, Minus, ShieldCheck, X } from "lucide-react";
import { useState } from "react";
import type { ReviewerStatus, TaskReviewRound, TaskReviews } from "@/api";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Label } from "@/components/ui/label";
import { useI18n } from "@/hooks/useI18n";
import { formatRelativeTime } from "@/lib/utils";

interface CodeReviewApprovalsProps {
  reviews: TaskReviews | null;
  inCodeReview: boolean;
}

const OUTCOME_VARIANT = {
  open: "info",
  approved: "success",
  rejected: "destructive",
  closed: "outline",
} as const;

/**
 * Who approved the task's code review and who asked for changes. Every
 * subscriber of code_review is a required reviewer, so the open round also
 * lists the ones still to decide — the reason a card is sitting there.
 */
export function CodeReviewApprovals({ reviews, inCodeReview }: CodeReviewApprovalsProps) {
  const { t } = useI18n();
  const [showEarlier, setShowEarlier] = useState(false);
  const rounds = reviews?.rounds ?? [];
  if (rounds.length === 0 && !inCodeReview) return null;

  const latest = rounds[rounds.length - 1];
  const earlier = rounds.slice(0, -1).reverse();

  return (
    <section className="space-y-2" data-testid="code-review-approvals">
      <Label className="flex items-center gap-2 text-xs text-muted-foreground">
        <ShieldCheck className="h-3.5 w-3.5" />
        {t("boardArea.components.taskDetail.codeReview.title")}
      </Label>
      {latest ? (
        <ReviewRound round={latest} />
      ) : (
        <p className="text-xs text-muted-foreground">{t("boardArea.components.taskDetail.codeReview.waitingForFirst")}</p>
      )}
      {earlier.length > 0 && (
        <div className="space-y-2">
          <Button
            type="button"
            variant="ghost"
            size="sm"
            className="h-6 gap-1 px-1 text-muted-foreground"
            onClick={() => setShowEarlier((v) => !v)}
            aria-expanded={showEarlier}
          >
            {showEarlier ? <ChevronDown className="h-3 w-3" /> : <ChevronRight className="h-3 w-3" />}
            {t("boardArea.components.taskDetail.codeReview.earlierRounds", { count: earlier.length })}
          </Button>
          {showEarlier && earlier.map((round) => <ReviewRound key={round.round} round={round} compact />)}
        </div>
      )}
    </section>
  );
}

function ReviewRound({ round, compact = false }: { round: TaskReviewRound; compact?: boolean }) {
  const { t } = useI18n();
  return (
    <Card className="space-y-1.5 rounded-md p-2 shadow-none">
      <div className="flex items-center justify-between gap-2">
        <span className="text-xs font-medium">
          {t("boardArea.components.taskDetail.codeReview.round", { round: round.round })}
        </span>
        <Badge variant={OUTCOME_VARIANT[round.outcome]}>
          {t(`boardArea.components.taskDetail.codeReview.outcome.${round.outcome}`)}
        </Badge>
      </div>
      {round.reviewers.length === 0 ? (
        <p className="text-xs text-muted-foreground">{t("boardArea.components.taskDetail.codeReview.noVerdicts")}</p>
      ) : (
        <ul className="space-y-1">
          {round.reviewers.map((reviewer) => (
            <ReviewerRow key={reviewer.agent_id ?? reviewer.agent_name} reviewer={reviewer} compact={compact} />
          ))}
        </ul>
      )}
    </Card>
  );
}

function ReviewerRow({ reviewer, compact }: { reviewer: ReviewerStatus; compact: boolean }) {
  const { t, lang } = useI18n();
  const label = t(`boardArea.components.taskDetail.codeReview.verdict.${reviewer.verdict}`);
  return (
    <li className="flex items-center justify-between gap-2 text-sm">
      <span className="min-w-0 truncate" title={reviewer.agent_name}>
        {reviewer.agent_name}
      </span>
      <span className="flex shrink-0 items-center gap-2">
        {!compact && reviewer.decided_at && (
          <span className="text-xs text-muted-foreground">{formatRelativeTime(reviewer.decided_at, lang)}</span>
        )}
        <VerdictBadge verdict={reviewer.verdict} label={label} />
      </span>
    </li>
  );
}

function VerdictBadge({ verdict, label }: { verdict: ReviewerStatus["verdict"]; label: string }) {
  if (verdict === "approve") {
    return (
      <Badge variant="success" className="gap-1">
        <Check className="h-3 w-3" />
        {label}
      </Badge>
    );
  }
  if (verdict === "reject") {
    return (
      <Badge variant="destructive" className="gap-1">
        <X className="h-3 w-3" />
        {label}
      </Badge>
    );
  }
  return (
    <Badge variant="outline" className="gap-1 text-muted-foreground">
      <Minus className="h-3 w-3" />
      {label}
    </Badge>
  );
}
