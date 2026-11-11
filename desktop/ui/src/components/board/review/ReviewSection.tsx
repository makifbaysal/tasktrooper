import { toast } from "sonner";
import { api, type ReviewFlag, type ReviewHistory } from "@/api";
import { ReviewFlagsCard } from "@/components/board/review/ReviewFlagsCard";
import { ReviewHistoryToggle } from "@/components/board/review/ReviewHistoryPanel";
import { useI18n } from "@/hooks/useI18n";
import { logEvent } from "@/lib/analytics";

interface ReviewFlagsPanelProps {
  repositoryId: string;
  taskId: string;
  history: ReviewHistory | null;
  reload: () => Promise<void> | void;
  prominent?: boolean;
}

/** "Needs your review": the flags card wired to the server. */
export function ReviewFlagsPanel({ repositoryId, taskId, history, reload, prominent }: ReviewFlagsPanelProps) {
  const { t } = useI18n();
  if (!history || history.flags.length === 0) return null;
  const mark = async (flag: ReviewFlag) => {
    try {
      await api.markReviewFlagReviewed(repositoryId, taskId, flag.id);
      logEvent("review_flag_reviewed", { source: flag.source, code: flag.code });
      await reload();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("boardArea.components.taskDetail.reviewFlags.markFailed"));
    }
  };
  return <ReviewFlagsCard flags={history.flags} onMarkReviewed={mark} prominent={prominent} />;
}

interface ReviewHistorySectionProps extends ReviewFlagsPanelProps {
  showFlags: boolean;
  onOpenRun?: (runId: string) => void;
}

/** The task drawer's review area beyond the verdicts: flags, then the history timeline. */
export function ReviewHistorySection({ showFlags, onOpenRun, ...flags }: ReviewHistorySectionProps) {
  return (
    <>
      {showFlags && <ReviewFlagsPanel {...flags} />}
      <ReviewHistoryToggle history={flags.history} onOpenRun={onOpenRun} />
    </>
  );
}
