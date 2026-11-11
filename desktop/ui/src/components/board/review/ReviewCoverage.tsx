import { EyeOff } from "lucide-react";
import type { ReviewNotReviewed, ReviewerCoverage, RoundCoverage } from "@/api";
import { ExternalLinkAnchor } from "@/components/board/review/ExternalLinkAnchor";
import { Badge } from "@/components/ui/badge";
import { Card } from "@/components/ui/card";
import { Progress } from "@/components/ui/progress";
import { useI18n } from "@/hooks/useI18n";

const base = "boardArea.components.taskDetail.reviewCoverage";

export function coverageRatio(done: number, total: number): number {
  return total > 0 ? Math.round((done / total) * 100) : 0;
}

function notReviewedText(item: ReviewNotReviewed, t: ReturnType<typeof useI18n>["t"]): string {
  switch (item.kind) {
    case "role":
      return t(`${base}.notReviewedKinds.role`, { scope: item.scope, count: item.count ?? 0 });
    case "skipped":
      return t(`${base}.notReviewedKinds.skipped`, { count: item.count ?? 0 });
    case "large":
      return t(`${base}.notReviewedKinds.large`, { count: item.count ?? 0 });
    case "truncated":
      return t(`${base}.notReviewedKinds.truncated`);
    case "check":
      return t(`${base}.notReviewedKinds.check`, { scope: item.scope, reason: item.reason });
    default:
      return t(`${base}.notReviewedKinds.declared`, { scope: item.scope, reason: item.reason });
  }
}

/** What one reviewing agent covered in a round, and what it says (or the records show) it did not. */
export function ReviewerCoverageCard({ coverage }: { coverage: ReviewerCoverage }) {
  const { t } = useI18n();
  const hasFiles = coverage.files_total > 0;
  const hasLines = coverage.lines_total > 0;
  return (
    <Card className="space-y-2 rounded-md p-2 shadow-none" data-testid="reviewer-coverage">
      <div className="flex items-center justify-between gap-2">
        <span className="min-w-0 truncate text-sm font-medium">{coverage.agent_name}</span>
        <span className="flex shrink-0 gap-1">
          {coverage.basis.map((b) => (
            <Badge key={b} variant="outline">
              {t(`${base}.basis.${b}`)}
            </Badge>
          ))}
        </span>
      </div>
      {coverage.aspects.length > 0 && (
        <div className="flex flex-wrap items-center gap-1" aria-label={t(`${base}.aspects`)}>
          {coverage.aspects.map((a) => (
            <Badge key={a} variant="info">
              {a}
            </Badge>
          ))}
        </div>
      )}
      {hasFiles && (
        <div className="space-y-1">
          <Progress
            value={coverageRatio(coverage.files_reviewed, coverage.files_total)}
            tone={coverage.files_reviewed === coverage.files_total ? "success" : "warning"}
            label={t(`${base}.filesOf`, { reviewed: coverage.files_reviewed, total: coverage.files_total })}
          />
          <p className="text-xs text-muted-foreground">
            {t(`${base}.filesOf`, { reviewed: coverage.files_reviewed, total: coverage.files_total })}
            {hasLines && (
              <>
                {" · "}
                {t(`${base}.linesOf`, {
                  reviewed: coverage.lines_reviewed,
                  total: coverage.lines_total,
                  percent: coverageRatio(coverage.lines_reviewed, coverage.lines_total),
                })}
              </>
            )}
          </p>
          {!coverage.diff_known && <p className="text-caption text-muted-foreground">{t(`${base}.diffUnknown`)}</p>}
        </div>
      )}
      {(coverage.criteria_total ?? 0) > 0 && (
        <p className="text-xs text-muted-foreground">
          {t(`${base}.criteria`, { checked: coverage.criteria_checked ?? 0, total: coverage.criteria_total ?? 0 })}
        </p>
      )}
      {(coverage.not_reviewed.length > 0 || coverage.unread_count > 0) && (
        <div className="space-y-1 rounded-md border border-amber-500/40 bg-amber-500/10 p-2" data-testid="not-reviewed">
          <p className="flex items-center gap-1 text-xs font-medium">
            <EyeOff className="h-3 w-3" />
            {t(`${base}.notReviewed`)}
          </p>
          <ul className="space-y-1 text-xs">
            {coverage.not_reviewed.map((item, i) => (
              <li key={`${item.kind}-${item.scope}-${i}`}>
                {item.file_url ? (
                  <ExternalLinkAnchor href={item.file_url} className="inline-flex items-center gap-1 text-primary hover:underline">
                    {notReviewedText(item, t)}
                  </ExternalLinkAnchor>
                ) : (
                  notReviewedText(item, t)
                )}
                {item.files && item.files.length > 0 && (
                  <span className="block break-all font-mono text-caption text-muted-foreground">{item.files.join(", ")}</span>
                )}
              </li>
            ))}
            {coverage.unread_count > 0 && (
              <li>
                {t(`${base}.unreadFiles`, { count: coverage.unread_count })}
                <span className="block break-all font-mono text-caption text-muted-foreground">
                  {coverage.unread_files.slice(0, 8).join(", ")}
                  {coverage.unread_count > 8 ? " …" : ""}
                </span>
              </li>
            )}
          </ul>
        </div>
      )}
    </Card>
  );
}

/** Every reviewer's coverage of one round, and the changed files none of them covered. */
export function RoundCoverageView({ coverage }: { coverage: RoundCoverage }) {
  const { t } = useI18n();
  return (
    <div className="space-y-2" data-testid="round-coverage">
      <p className="text-xs font-medium text-muted-foreground">
        {t(`${base}.title`)}
        {coverage.changed_files > 0 && (
          <span className="font-normal">
            {" · "}
            {t(`${base}.changed`, { files: coverage.changed_files, lines: coverage.changed_lines })}
          </span>
        )}
      </p>
      {coverage.reviewers.map((r) => (
        <ReviewerCoverageCard key={r.agent_id ?? r.agent_name} coverage={r} />
      ))}
      {coverage.nobody_reviewed.length > 0 && (
        <div className="space-y-1 rounded-md border-2 border-amber-500/60 bg-amber-500/10 p-2" data-testid="nobody-reviewed">
          <p className="text-sm font-semibold">{t(`${base}.nobodyTitle`)}</p>
          <p className="text-xs text-muted-foreground">{t(`${base}.nobodyBody`)}</p>
          <ul className="space-y-0.5">
            {coverage.nobody_reviewed.map((f) => (
              <li key={f.path} className="break-all font-mono text-caption">
                {f.file_url ? (
                  <ExternalLinkAnchor href={f.file_url} className="inline-flex items-center gap-1 text-primary hover:underline">
                    {f.path}
                  </ExternalLinkAnchor>
                ) : (
                  f.path
                )}
                {f.lines ? <span className="text-muted-foreground"> · {t(`${base}.lines`, { count: f.lines })}</span> : null}
              </li>
            ))}
          </ul>
        </div>
      )}
    </div>
  );
}
