import { useEffect, useState } from "react";
import { toast } from "sonner";
import { api, type ComponentFinding, type FindingTaskResult } from "@/api";
import { FormDialog } from "@/components/admin/FormDialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { useI18n } from "@/hooks/useI18n";

export type FindingVerdict = "accepted" | "false_positive";

interface VerdictDialogProps {
  finding: ComponentFinding | null;
  verdict: FindingVerdict;
  onOpenChange: (open: boolean) => void;
  onSaved: (finding: ComponentFinding) => void;
}

/** Accept the risk or call a false positive; either stays closed on later scans. */
export function FindingVerdictDialog({ finding, verdict, onOpenChange, onSaved }: VerdictDialogProps) {
  const { t } = useI18n();
  const [reason, setReason] = useState("");
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    if (finding) setReason("");
  }, [finding]);

  const save = async () => {
    if (!finding) return;
    setSaving(true);
    try {
      const updated = await api.updateFinding(finding.id, { status: verdict, reason: reason.trim() || undefined });
      onSaved(updated);
      onOpenChange(false);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("findings.updateFailed"));
    } finally {
      setSaving(false);
    }
  };

  const accept = verdict === "accepted";
  return (
    <FormDialog
      open={Boolean(finding)}
      onOpenChange={onOpenChange}
      title={t(accept ? "findings.verdict.acceptTitle" : "findings.verdict.falsePositiveTitle")}
      description={t(accept ? "findings.verdict.acceptDescription" : "findings.verdict.falsePositiveDescription")}
      footer={
        <>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={saving}>
            {t("common.cancel")}
          </Button>
          <Button onClick={save} disabled={saving}>
            {t("findings.verdict.save")}
          </Button>
        </>
      }
    >
      {finding && <p className="font-medium">{t(`findings.rules.${finding.rule}.title`)}</p>}
      <div className="space-y-2">
        <Label htmlFor="finding-reason">{t("findings.verdict.reasonLabel")}</Label>
        <Textarea
          id="finding-reason"
          value={reason}
          maxLength={500}
          placeholder={t("findings.verdict.reasonPlaceholder")}
          onChange={(e) => setReason(e.target.value)}
        />
      </div>
    </FormDialog>
  );
}

interface TaskDialogProps {
  repositoryId: string;
  findings: ComponentFinding[];
  defaultTitle: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onCreated: (result: FindingTaskResult) => void;
}

/** One board task for the given open findings; the server writes the agent's brief. */
export function FindingTaskDialog({ repositoryId, findings, defaultTitle, open, onOpenChange, onCreated }: TaskDialogProps) {
  const { t } = useI18n();
  const [title, setTitle] = useState("");
  const [notes, setNotes] = useState("");
  const [submitting, setSubmitting] = useState(false);

  useEffect(() => {
    if (!open) return;
    setTitle(defaultTitle);
    setNotes("");
  }, [open, defaultTitle]);

  const submit = async () => {
    if (!title.trim() || findings.length === 0) return;
    setSubmitting(true);
    try {
      const result = await api.createFindingTask(repositoryId, {
        finding_ids: findings.map((f) => f.id),
        title: title.trim(),
        notes: notes.trim() || undefined,
      });
      toast.success(t("findings.task.created", { key: result.task.key }));
      onCreated(result);
      onOpenChange(false);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("findings.task.failed"));
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <FormDialog
      open={open}
      onOpenChange={onOpenChange}
      title={t("findings.task.title")}
      description={t("findings.task.description", { count: findings.length })}
      footer={
        <>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={submitting}>
            {t("common.cancel")}
          </Button>
          <Button onClick={submit} disabled={submitting || !title.trim()}>
            {t("findings.task.submit")}
          </Button>
        </>
      }
    >
      <div className="space-y-2">
        <Label htmlFor="finding-task-title">{t("findings.task.titleLabel")}</Label>
        <Input id="finding-task-title" value={title} maxLength={200} onChange={(e) => setTitle(e.target.value)} />
      </div>
      <ul className="space-y-1 text-caption text-muted-foreground">
        {findings.map((f) => (
          <li key={f.id}>• {t(`findings.rules.${f.rule}.title`)}</li>
        ))}
      </ul>
      <div className="space-y-2">
        <Label htmlFor="finding-task-notes">{t("findings.task.notesLabel")}</Label>
        <Textarea
          id="finding-task-notes"
          value={notes}
          placeholder={t("findings.task.notesPlaceholder")}
          onChange={(e) => setNotes(e.target.value)}
        />
      </div>
    </FormDialog>
  );
}
