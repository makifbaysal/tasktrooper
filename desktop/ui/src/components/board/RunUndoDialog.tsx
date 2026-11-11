import { Loader2, Undo2 } from "lucide-react";
import { useEffect, useState } from "react";
import { api, ApiError, type RunRevertResult, type RunUndoPreview, type RunUndoResult } from "@/api";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Label } from "@/components/ui/label";
import { Notice } from "@/components/ui/notice";
import { Skeleton } from "@/components/ui/skeleton";
import { useI18n } from "@/hooks/useI18n";
import { shortSha } from "@/lib/runTrace";

interface RunUndoDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  repositoryId: string;
  taskId: string;
  runId: string;
  onDone: () => void;
}

type Outcome = { kind: "reset"; result: RunUndoResult } | { kind: "revert"; result: RunRevertResult };

export function RunUndoDialog({ open, onOpenChange, repositoryId, taskId, runId, onDone }: RunUndoDialogProps) {
  const { t } = useI18n();
  const [preview, setPreview] = useState<RunUndoPreview | null>(null);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [forcePush, setForcePush] = useState(false);
  const [moveBack, setMoveBack] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [outcome, setOutcome] = useState<Outcome | null>(null);

  useEffect(() => {
    if (!open) return;
    let live = true;
    setPreview(null);
    setLoadError(null);
    setOutcome(null);
    setError(null);
    setForcePush(false);
    setMoveBack(true);
    api
      .getRunUndoPreview(repositoryId, taskId, runId)
      .then((res) => live && setPreview(res.preview))
      .catch((e) => live && setLoadError(e instanceof Error ? e.message : String(e)));
    return () => {
      live = false;
    };
  }, [open, repositoryId, taskId, runId]);

  const fail = (e: unknown) => setError(e instanceof ApiError || e instanceof Error ? e.message : t("runUndo.failed"));

  const confirmReset = async () => {
    setBusy(true);
    setError(null);
    try {
      const res = await api.undoRun(repositoryId, taskId, runId, { force_push: forcePush, keep_column: !moveBack });
      setOutcome({ kind: "reset", result: res.result });
      onDone();
    } catch (e) {
      fail(e);
    } finally {
      setBusy(false);
    }
  };

  const confirmRevert = async () => {
    setBusy(true);
    setError(null);
    try {
      const res = await api.revertRun(repositoryId, taskId, runId);
      setOutcome({ kind: "revert", result: res.result });
      onDone();
    } catch (e) {
      fail(e);
    } finally {
      setBusy(false);
    }
  };

  const canReset = !!preview?.supported && preview.mode === "reset";
  const canRevert = !!preview && preview.mode === "revert" && preview.revert_available;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[85vh] max-w-xl overflow-y-auto">
        <DialogHeader>
          <DialogTitle>{t("runUndo.title")}</DialogTitle>
          <DialogDescription>{t("runUndo.description")}</DialogDescription>
        </DialogHeader>

        {loadError && <Notice variant="error" title={t("runUndo.loadFailed")}>{loadError}</Notice>}
        {!preview && !loadError && (
          <div className="space-y-2" aria-busy="true">
            <Skeleton className="h-4 w-2/3" />
            <Skeleton className="h-16 w-full" />
          </div>
        )}

        {outcome ? (
          <OutcomeView outcome={outcome} />
        ) : (
          preview && (
            <div className="min-w-0 space-y-3 text-caption">
              {!preview.supported && preview.reason && (
                <Notice variant={canRevert ? "info" : "warning"} title={t("runUndo.blockedTitle")} role={null}>
                  {t(`runUndo.blocked.${preview.reason}`)}
                </Notice>
              )}

              {canRevert && preview.pull_request && (
                <p className="text-muted-foreground">
                  {t("runUndo.revertBody", { sha: shortSha(preview.pull_request.merge_sha) })}
                </p>
              )}

              {canReset && (
                <>
                  <section className="space-y-1">
                    <p className="font-medium">{t("runUndo.willReset", { branch: preview.branch ?? "", sha: shortSha(preview.base_sha) })}</p>
                    {preview.commits.length > 0 && (
                      <ul className="space-y-0.5 text-micro">
                        {preview.commits.map((c) => (
                          <li key={c.sha} className="[overflow-wrap:anywhere]">
                            <span className="font-mono text-muted-foreground">{shortSha(c.sha)}</span> {c.subject}
                          </li>
                        ))}
                      </ul>
                    )}
                    <p className="text-micro text-muted-foreground">
                      {t("runUndo.files", { count: preview.files.length, uncommitted: preview.uncommitted.length })}
                    </p>
                    {preview.files.length > 0 && (
                      <ul className="max-h-32 overflow-y-auto font-mono text-micro [overflow-wrap:anywhere]">
                        {preview.files.map((f) => (
                          <li key={`${f.status}:${f.path}`}>{f.path}</li>
                        ))}
                      </ul>
                    )}
                  </section>

                  <p className="text-micro text-muted-foreground">
                    {t("runUndo.backup", { ref: preview.backup_ref })}
                  </p>

                  {preview.pushed && (
                    <section className="space-y-1.5">
                      <Notice variant="warning" title={t("runUndo.pushedTitle")} role={null}>
                        {preview.remote_ahead ? t("runUndo.remoteAhead") : t("runUndo.pushedBody")}
                      </Notice>
                      {!preview.remote_ahead && (
                        <div className="flex items-center gap-2">
                          <Checkbox id="undo-force-push" checked={forcePush} onCheckedChange={(v) => setForcePush(v === true)} />
                          <Label htmlFor="undo-force-push" className="font-normal">
                            {t("runUndo.forcePush")}
                          </Label>
                        </div>
                      )}
                    </section>
                  )}

                  {preview.move_to && (
                    <div className="flex items-center gap-2">
                      <Checkbox id="undo-move-back" checked={moveBack} onCheckedChange={(v) => setMoveBack(v === true)} />
                      <Label htmlFor="undo-move-back" className="font-normal">
                        {t("runUndo.moveBack", { column: preview.move_to })}
                      </Label>
                    </div>
                  )}
                </>
              )}

              {preview.non_undoable.length > 0 && (
                <section className="space-y-1 rounded-lg border border-border bg-muted/10 px-3 py-2">
                  <p className="font-medium">{t("runUndo.cannotUndo")}</p>
                  <ul className="space-y-0.5 text-micro">
                    {preview.non_undoable.map((x) => (
                      <li key={x.seq} className="[overflow-wrap:anywhere]">
                        {t(`runTrace.nonUndoable.kinds.${x.kind}`)}
                        <span className="text-muted-foreground"> · {x.tool}{x.target ? ` · ${x.target}` : ""}</span>
                      </li>
                    ))}
                  </ul>
                </section>
              )}

              {error && <Notice variant="error" title={t("runUndo.failed")}>{error}</Notice>}
            </div>
          )
        )}

        <DialogFooter>
          <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
            {outcome ? t("runUndo.close") : t("common.cancel")}
          </Button>
          {!outcome && canReset && (
            <Button type="button" variant="destructive" disabled={busy} onClick={() => void confirmReset()}>
              {busy ? <Loader2 className="animate-spin" /> : <Undo2 />}
              {t("runUndo.confirm")}
            </Button>
          )}
          {!outcome && canRevert && (
            <Button type="button" variant="destructive" disabled={busy} onClick={() => void confirmRevert()}>
              {busy ? <Loader2 className="animate-spin" /> : <Undo2 />}
              {t("runUndo.confirmRevert")}
            </Button>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function OutcomeView({ outcome }: { outcome: Outcome }) {
  const { t } = useI18n();
  if (outcome.kind === "revert") {
    const r = outcome.result;
    return (
      <div className="space-y-2 text-caption">
        <Notice variant="info" title={t("runUndo.revertDone")} role="status">
          {r.pr_url ? t("runUndo.revertPr", { url: r.pr_url }) : t("runUndo.revertBranchOnly", { branch: r.branch })}
        </Notice>
        {r.pr_error && <p className="text-micro text-muted-foreground [overflow-wrap:anywhere]">{r.pr_error}</p>}
      </div>
    );
  }
  const r = outcome.result;
  return (
    <div className="space-y-2 text-caption">
      <Notice variant="info" title={t("runUndo.done")} role="status">
        {t("runUndo.doneBackup", { ref: r.backup_ref })}
      </Notice>
      {r.push_pending && <p className="text-muted-foreground">{t("runUndo.pushPending")}</p>}
      {r.force_pushed && <p className="text-muted-foreground">{t("runUndo.forcePushed")}</p>}
      {r.moved_to && <p className="text-muted-foreground">{t("runUndo.movedTo", { column: r.moved_to })}</p>}
      {r.stale_criteria > 0 && <p className="text-muted-foreground">{t("runUndo.staleCriteria", { count: r.stale_criteria })}</p>}
      {r.left_untracked.length > 0 && (
        <div>
          <p className="text-muted-foreground">{t("runUndo.leftUntracked")}</p>
          <ul className="max-h-24 overflow-y-auto font-mono text-micro [overflow-wrap:anywhere]">
            {r.left_untracked.map((p) => (
              <li key={p}>{p}</li>
            ))}
          </ul>
        </div>
      )}
      {r.non_undoable.length > 0 && <p className="text-muted-foreground">{t("runUndo.stillStands", { count: r.non_undoable.length })}</p>}
    </div>
  );
}
