import { Link } from "react-router-dom";
import type { ProjectOverview } from "@/api";
import { Button } from "@/components/ui/button";
import { Notice } from "@/components/ui/notice";
import { useI18n } from "@/hooks/useI18n";

interface AttentionNoticeProps {
  projects: ProjectOverview[];
}

/** Summed review_count across every project, with the projects involved and
 * a shortcut to the first one's review tab. Renders nothing when nothing
 * needs review. */
export function AttentionNotice({ projects }: AttentionNoticeProps) {
  const { t } = useI18n();
  const withReview = projects.filter((p) => p.review_count > 0);
  const total = withReview.reduce((sum, p) => sum + p.review_count, 0);
  if (total === 0 || withReview.length === 0) return null;

  return (
    <Notice variant="warning" title={t("projectsHub.attention.message", { count: total })} className="mb-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <span>{withReview.map((p) => p.name).join(", ")}</span>
        <Button size="sm" asChild className="shrink-0">
          <Link to={`/projects/${withReview[0].id}?tab=review`}>{t("projectsHub.attention.review")}</Link>
        </Button>
      </div>
    </Notice>
  );
}
