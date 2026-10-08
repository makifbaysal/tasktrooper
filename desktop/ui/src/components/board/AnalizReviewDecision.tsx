import { Eye, FileSearch } from "lucide-react";
import { useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { toast } from "sonner";
import { api, type BoardTask, type TaskDocument } from "@/api";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import { Label } from "@/components/ui/label";
import { useI18n } from "@/hooks/useI18n";
import { analysisReviewPath } from "@/lib/analysis-review";
import { isDesignTask } from "@/lib/design-system";

interface AnalizReviewDecisionProps {
  task: BoardTask;
  repositoryId: string;
  onUpdated: () => void;
  documents?: TaskDocument[];
}

// Mirrors HumanUatDecision.tsx's rationale — see that file for why decline is
// rendered inline rather than as a nested Radix Dialog. `analiz_review` and
// `human_uat` are mutually exclusive columns, so at most one of these two
// controls is ever visible on a given task.
export function AnalizReviewDecision({ task, repositoryId, onUpdated, documents = [] }: AnalizReviewDecisionProps) {
  const { t } = useI18n();
  const [saving, setSaving] = useState(false);
  const [declining, setDeclining] = useState(false);
  const [reason, setReason] = useState("");
  const [openComments, setOpenComments] = useState(0);
  const hasDocuments = documents.length > 0;
  const inReview = task.column === "analiz_review";
  const design = isDesignTask(task);

  useEffect(() => {
    setDeclining(false);
    setReason("");
  }, [task.id]);

  useEffect(() => {
    setOpenComments(0);
    if (!hasDocuments || !inReview) return;
    let cancelled = false;
    api
      .listTaskAnnotations(repositoryId, task.id)
      .then((data) => {
        if (!cancelled) setOpenComments((data.annotations ?? []).filter((a) => a.status === "open").length);
      })
      .catch(() => {});
    return () => {
      cancelled = true;
    };
  }, [repositoryId, task.id, hasDocuments, inReview]);

  if (!inReview) return null;

  const approve = async () => {
    setSaving(true);
    try {
      await api.updateRepositoryTask(repositoryId, task.id, { column: "done" });
      onUpdated();
      toast.success(t("boardArea.components.taskDetail.analizReviewApproved"));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("boardArea.components.taskDetail.updateFailed"));
    } finally {
      setSaving(false);
    }
  };

  const cancelDecline = () => {
    if (saving) return;
    setDeclining(false);
    setReason("");
  };

  const submitDecline = async () => {
    const trimmed = reason.trim();
    if (!trimmed) return;
    setSaving(true);
    try {
      await api.createTaskComment(repositoryId, task.id, trimmed);
      await api.updateRepositoryTask(repositoryId, task.id, { column: "need_revision" });
      setDeclining(false);
      setReason("");
      onUpdated();
      toast.success(t("boardArea.components.taskDetail.analizReviewDeclined"));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("boardArea.components.taskDetail.updateFailed"));
    } finally {
      setSaving(false);
    }
  };

  return (
    <section className="space-y-3 rounded-lg border-2 border-primary bg-primary/10 p-4 shadow-sm">
      <div className="flex items-center gap-2 text-primary">
        <Eye className="h-5 w-5 shrink-0" />
        <h3 className="text-base font-semibold text-foreground">
          {t("boardArea.components.taskDetail.analizReviewHeading")}
        </h3>
      </div>
      {design && (
        <p className="text-sm text-muted-foreground">{t("analysisReview.page.designApproveHint")}</p>
      )}
      {!declining ? (
        <div className="flex flex-wrap items-center gap-2">
          <Button onClick={approve} disabled={saving}>
            {t("boardArea.components.taskDetail.analizReviewApprove")}
          </Button>
          <Button variant="destructive" onClick={() => setDeclining(true)} disabled={saving}>
            {t("boardArea.components.taskDetail.analizReviewDecline")}
          </Button>
        </div>
      ) : (
        <div className="space-y-2">
          <Label htmlFor="analiz-review-decline-reason">
            {t("boardArea.components.taskDetail.analizReviewDeclineReasonLabel")}
          </Label>
          <p className="text-sm text-muted-foreground">
            {t("boardArea.components.taskDetail.analizReviewDeclineDescription")}
          </p>
          <Textarea
            id="analiz-review-decline-reason"
            value={reason}
            onChange={(e) => setReason(e.target.value)}
            placeholder={
              design
                ? t("analysisReview.design.declinePlaceholder")
                : t("boardArea.components.taskDetail.analizReviewDeclinePlaceholder")
            }
            rows={4}
            disabled={saving}
            autoFocus
          />
          <div className="flex justify-end gap-2">
            <Button variant="ghost" size="sm" onClick={cancelDecline} disabled={saving}>
              {t("common.cancel")}
            </Button>
            <Button
              variant="destructive"
              size="sm"
              onClick={submitDecline}
              disabled={saving || reason.trim().length === 0}
            >
              {t("boardArea.components.taskDetail.analizReviewDeclineSubmit")}
            </Button>
          </div>
        </div>
      )}
      {hasDocuments && (
        <Button variant="outline" className="gap-2" asChild>
          <Link to={analysisReviewPath(repositoryId, task.id)}>
            <FileSearch />
            {openComments > 0
              ? t(
                  design
                    ? "analysisReview.design.openReviewCount"
                    : "boardArea.components.taskDetail.analizReviewOpenReviewCount",
                  { count: openComments },
                )
              : t(design ? "analysisReview.design.openReview" : "boardArea.components.taskDetail.analizReviewOpenReview")}
          </Link>
        </Button>
      )}
    </section>
  );
}
