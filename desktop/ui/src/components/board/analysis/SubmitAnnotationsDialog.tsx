import { Loader2, Send } from "lucide-react";
import { useEffect, useState } from "react";
import { FormDialog } from "@/components/admin/FormDialog";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { useI18n } from "@/hooks/useI18n";

interface SubmitAnnotationsDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  openCount: number;
  answerCount: number;
  onSubmit: (note: string) => Promise<void>;
}

export function SubmitAnnotationsDialog({
  open,
  onOpenChange,
  openCount,
  answerCount,
  onSubmit,
}: SubmitAnnotationsDialogProps) {
  const { t } = useI18n();
  const [note, setNote] = useState("");
  const [submitting, setSubmitting] = useState(false);

  useEffect(() => {
    if (open) setNote("");
  }, [open]);

  const submit = async () => {
    setSubmitting(true);
    try {
      await onSubmit(note.trim());
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <FormDialog
      open={open}
      onOpenChange={(next) => !submitting && onOpenChange(next)}
      title={
        openCount > 0
          ? t("analysisReview.submit.title", { count: openCount })
          : t("analysisReview.submit.answersOnlyTitle", { count: answerCount })
      }
      description={[
        openCount > 0 ? t("analysisReview.submit.description") : t("analysisReview.submit.answersOnlyDescription"),
        openCount > 0 && answerCount > 0 ? t("analysisReview.submit.answersIncluded", { count: answerCount }) : null,
      ]
        .filter(Boolean)
        .join(" ")}
      footer={
        <>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={submitting}>
            {t("common.cancel")}
          </Button>
          <Button onClick={submit} disabled={submitting || openCount + answerCount === 0}>
            {submitting ? <Loader2 className="animate-spin" /> : <Send />}
            {openCount > 0 ? t("analysisReview.submit.confirm") : t("analysisReview.submit.answersOnlyConfirm")}
          </Button>
        </>
      }
    >
      <div className="space-y-2">
        <Label htmlFor="analysis-submit-note">{t("analysisReview.submit.noteLabel")}</Label>
        <Textarea
          id="analysis-submit-note"
          value={note}
          onChange={(e) => setNote(e.target.value)}
          placeholder={t("analysisReview.submit.notePlaceholder")}
          rows={4}
          disabled={submitting}
        />
      </div>
    </FormDialog>
  );
}
