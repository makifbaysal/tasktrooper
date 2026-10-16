import { Undo2 } from "lucide-react";
import { useState } from "react";
import { Link } from "react-router-dom";
import { toast } from "sonner";
import { api, type Release } from "@/api";
import type { RollbackSuggestion } from "@/components/operations/changes/rollbackSuggestion";
import { releaseLabel } from "@/components/operations/console/opsEntities";
import { Button } from "@/components/ui/button";
import { ConfirmDialog } from "@/components/ui/confirm-dialog";
import { Label } from "@/components/ui/label";
import { Notice } from "@/components/ui/notice";
import { Textarea } from "@/components/ui/textarea";
import { useI18n } from "@/hooks/useI18n";

interface RollbackSuggestionBannerProps {
  release: Release;
  suggestion: RollbackSuggestion;
  /** The typed confirmation; without it nothing can be approved. */
  repositoryName?: string;
  /** Where the change itself opens, from a page other than Changes. */
  releaseHref?: string;
  onDone: (release: Release) => void;
}

/** "Rollback suggested": why, and Approve rollback — the existing rollback,
 * confirmed with the repository's name typed, as every release write is. */
export function RollbackSuggestionBanner({ release, suggestion, repositoryName, releaseHref, onDone }: RollbackSuggestionBannerProps) {
  const { t } = useI18n();
  const r = (key: string, params?: Record<string, string | number>) => t(`operations.changes.rollbackSuggestion.${key}`, params);
  const version = releaseLabel(release);
  const [open, setOpen] = useState(false);
  const [note, setNote] = useState("");
  const [busy, setBusy] = useState(false);
  const [refusal, setRefusal] = useState<string | null>(null);

  const reasonText =
    suggestion.reason === "incident"
      ? r("reasons.incident", { title: suggestion.incident?.title ?? "" })
      : suggestion.reason === "verify"
        ? r("reasons.verify")
        : r("reasons.failed");

  const start = () => {
    setRefusal(null);
    setNote(suggestion.detail?.trim() || reasonText);
    setOpen(true);
  };

  const approve = async (typed: string): Promise<boolean> => {
    setBusy(true);
    try {
      const updated = await api.rollbackRelease(release.id, typed, note.trim());
      setRefusal(null);
      onDone(updated);
      toast.success(t("operations.changes.actions.done.rollback"));
      return true;
    } catch (err) {
      setRefusal(err instanceof Error ? err.message : t("common.actionFailed"));
      return false;
    } finally {
      setBusy(false);
    }
  };

  return (
    <>
      <Notice variant="error" role="status" title={r("title", { version })}>
        <div className="space-y-2">
          <p className="text-caption">{reasonText}</p>
          {suggestion.detail && <p className="break-words font-mono text-micro">{suggestion.detail}</p>}
          <p className="text-caption">{r("autoOff")}</p>
          <div className="flex flex-wrap items-center gap-2">
            <Button size="sm" variant="destructive" disabled={!repositoryName} onClick={start}>
              <Undo2 aria-hidden />
              {r("approve")}
            </Button>
            {releaseHref && (
              <Button size="sm" variant="outline" asChild>
                <Link to={releaseHref}>{r("openChange", { version })}</Link>
              </Button>
            )}
          </div>
        </div>
      </Notice>
      <ConfirmDialog
        open={open}
        onOpenChange={setOpen}
        title={r("confirmTitle", { version })}
        description={t("operations.changes.actions.rollbackDescription")}
        confirmLabel={r("approve")}
        variant="destructive"
        loading={busy}
        confirmPhrase={repositoryName}
        confirmDisabled={!repositoryName || note.trim() === ""}
        error={refusal}
        errorTitle={t("operations.changes.actions.refused")}
        onConfirm={approve}
      >
        <div className="space-y-2">
          <Label htmlFor="rollback-approval-note">{t("operations.changes.actions.noteLabel.rollback")}</Label>
          <Textarea id="rollback-approval-note" value={note} onChange={(e) => setNote(e.target.value)} rows={3} />
        </div>
      </ConfirmDialog>
    </>
  );
}
