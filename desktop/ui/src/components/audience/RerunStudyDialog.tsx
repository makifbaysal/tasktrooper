import { Loader2 } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";
import { api, type AudienceStudy } from "@/api";
import { FormDialog } from "@/components/admin/FormDialog";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { useI18n } from "@/hooks/useI18n";

interface RerunStudyDialogProps {
  studyId: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onPlanned: (study: AudienceStudy) => void;
}

/**
 * Plans the same study again after the product changed, so the two reports can
 * be compared. It only plans: the same people are asked once the person has
 * approved the script again and starts it.
 */
export function RerunStudyDialog({ studyId, open, onOpenChange, onPlanned }: RerunStudyDialogProps) {
  const { t } = useI18n();
  const r = (key: string) => t(`audienceHub.rerun.${key}`);
  const [changes, setChanges] = useState("");
  const [saving, setSaving] = useState(false);

  const submit = async () => {
    setSaving(true);
    try {
      const study = await api.rerunAudienceStudy(studyId, changes.trim());
      setChanges("");
      onOpenChange(false);
      onPlanned(study);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : r("failed"));
    } finally {
      setSaving(false);
    }
  };

  return (
    <FormDialog
      open={open}
      onOpenChange={onOpenChange}
      title={r("title")}
      description={r("description")}
      footer={
        <>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={saving}>
            {t("common.cancel")}
          </Button>
          <Button onClick={() => void submit()} disabled={saving}>
            {saving && <Loader2 className="animate-spin" />}
            {r("plan")}
          </Button>
        </>
      }
    >
      <div className="space-y-2">
        <Label htmlFor="rerun-changes">{r("changes")}</Label>
        <Textarea id="rerun-changes" rows={5} value={changes} onChange={(e) => setChanges(e.target.value)} placeholder={r("changesPlaceholder")} />
        <p className="text-caption text-muted-foreground">{r("changesHelp")}</p>
      </div>
    </FormDialog>
  );
}
