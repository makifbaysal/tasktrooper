import { Undo2, X } from "lucide-react";
import { useState } from "react";
import { Link } from "react-router-dom";
import { toast } from "sonner";
import { api, ROLLBACK_REASONS, type Release } from "@/api";
import { refusalKey, rollbackSuggestion, withoutProposal } from "@/components/operations/changes/rollbackSuggestion";
import { releaseLabel } from "@/components/operations/console/opsEntities";
import { Button } from "@/components/ui/button";
import { ConfirmDialog } from "@/components/ui/confirm-dialog";
import { Label } from "@/components/ui/label";
import { Notice } from "@/components/ui/notice";
import { Textarea } from "@/components/ui/textarea";
import { useI18n } from "@/hooks/useI18n";
import { formatDate, formatRelativeTime } from "@/lib/utils";

const ACTORS = ["agent", "human", "system"];

interface RollbackSuggestionBannerProps {
  release: Release;
  /** The typed confirmation; without it nothing can be approved. */
  repositoryName?: string;
  /** Where the change itself opens, from a page other than Changes. */
  releaseHref?: string;
  onDone: (release: Release) => void;
}

/** "Rollback suggested", only for the server's own `rollback_proposal`: its
 * reason and who proposed it, Approve rollback (the existing rollback,
 * confirmed with the repository's name typed) while the server allows one,
 * else why it does not, and Dismiss suggestion. Renders nothing without a
 * proposal. */
export function RollbackSuggestionBanner({ release, repositoryName, releaseHref, onDone }: RollbackSuggestionBannerProps) {
  const { t, lang } = useI18n();
  const r = (key: string, params?: Record<string, string | number>) => t(`operations.changes.rollbackSuggestion.${key}`, params);
  const version = releaseLabel(release);
  const [open, setOpen] = useState(false);
  const [typedNote, setTypedNote] = useState("");
  const [busy, setBusy] = useState(false);
  const [refusal, setRefusal] = useState<string | null>(null);
  const [dismissing, setDismissing] = useState(false);
  const [dismissError, setDismissError] = useState<string | null>(null);

  const suggestion = rollbackSuggestion(release);
  if (!suggestion) return null;
  const { proposal } = suggestion;
  const approvable = suggestion.kind === "approvable";
  const who = ACTORS.includes(proposal.by) ? t(`operations.changes.detail.rollback.actors.${proposal.by}`) : proposal.by;
  const reason = ROLLBACK_REASONS.includes(proposal.reason)
    ? t(`operations.changes.detail.rollback.reasons.${proposal.reason}`)
    : proposal.reason;
  const note = proposal.note?.trim() ?? "";
  const known = suggestion.kind === "refused" ? refusalKey(suggestion.refusal) : undefined;
  const why =
    suggestion.kind === "refused"
      ? known
        ? r(`refusals.${known}`)
        : r("refusals.other", { code: suggestion.refusal || "—" })
      : undefined;

  const start = () => {
    setRefusal(null);
    setTypedNote(note || reason);
    setOpen(true);
  };

  const approve = async (typed: string): Promise<boolean> => {
    setBusy(true);
    try {
      const updated = await api.rollbackRelease(release.id, typed, typedNote.trim());
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

  const dismiss = async () => {
    setDismissing(true);
    setDismissError(null);
    try {
      const answer = await api.dismissRollbackProposal(release.id);
      onDone(withoutProposal(release, answer));
      toast.success(r("dismissed"));
    } catch (err) {
      setDismissError(err instanceof Error ? err.message : t("common.actionFailed"));
    } finally {
      setDismissing(false);
    }
  };

  return (
    <>
      <Notice variant={approvable ? "error" : "warning"} role="status" title={r("title", { version })}>
        <div className="space-y-2">
          <p className="text-caption font-medium">{reason}</p>
          {note && <p className="break-words text-caption whitespace-pre-wrap">{note}</p>}
          <p className="text-micro opacity-80">
            {r("proposedBy", { who })}
            {proposal.proposed_at && (
              <span title={formatDate(proposal.proposed_at)}> · {formatRelativeTime(proposal.proposed_at, lang)}</span>
            )}
          </p>
          <p className="text-caption">{approvable ? r("autoOff") : why}</p>
          {dismissError && (
            <p className="break-words text-caption text-destructive" role="alert">
              {r("dismissFailed")}: {dismissError}
            </p>
          )}
          <div className="flex flex-wrap items-center gap-2">
            {approvable && (
              <Button size="sm" variant="destructive" disabled={!repositoryName || dismissing} onClick={start}>
                <Undo2 aria-hidden />
                {r("approve")}
              </Button>
            )}
            <Button size="sm" variant="outline" disabled={dismissing || busy} onClick={() => void dismiss()}>
              <X aria-hidden />
              {dismissing ? r("dismissing") : r("dismiss")}
            </Button>
            {releaseHref && (
              <Button size="sm" variant="outline" asChild>
                <Link to={releaseHref}>{r("openChange", { version })}</Link>
              </Button>
            )}
          </div>
        </div>
      </Notice>
      {approvable && (
        <ConfirmDialog
          open={open}
          onOpenChange={setOpen}
          title={r("confirmTitle", { version })}
          description={t("operations.changes.actions.rollbackDescription")}
          confirmLabel={r("approve")}
          variant="destructive"
          loading={busy}
          confirmPhrase={repositoryName}
          confirmDisabled={!repositoryName || typedNote.trim() === ""}
          error={refusal}
          errorTitle={t("operations.changes.actions.refused")}
          onConfirm={approve}
        >
          <div className="space-y-2">
            <Label htmlFor="rollback-approval-note">{t("operations.changes.actions.noteLabel.rollback")}</Label>
            <Textarea id="rollback-approval-note" value={typedNote} onChange={(e) => setTypedNote(e.target.value)} rows={3} />
          </div>
        </ConfirmDialog>
      )}
    </>
  );
}
