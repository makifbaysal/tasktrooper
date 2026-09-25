import { ExternalLink } from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import { toast } from "sonner";
import { api, type DeployWatchState, type Release, type ReleaseStatus } from "@/api";
import { type BadgeProps, Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Separator } from "@/components/ui/separator";
import { Skeleton } from "@/components/ui/skeleton";
import { Textarea } from "@/components/ui/textarea";
import { useI18n } from "@/hooks/useI18n";
import { formatDate, formatRelativeDate } from "@/lib/utils";

export const RELEASE_STATUS_VARIANT: Record<ReleaseStatus, NonNullable<BadgeProps["variant"]>> = {
  draft: "secondary",
  pending: "secondary",
  deploying: "info",
  verifying: "info",
  rolling_back: "info",
  awaiting_verdict: "warning",
  released: "success",
  rolled_back: "destructive",
  failed: "destructive",
  superseded: "secondary",
};

/** A release the sweeper is actively moving; ReleasesCard polls while one is
 * on screen instead of leaving a stale status up. */
export const RELEASE_ACTIVE_STATUSES: ReleaseStatus[] = ["deploying", "verifying", "rolling_back", "awaiting_verdict"];

const DEPLOY_STATE_VARIANT: Record<DeployWatchState, NonNullable<BadgeProps["variant"]>> = {
  pending: "info",
  success: "success",
  failure: "destructive",
  no_signal: "secondary",
  unknown: "secondary",
};

interface ReleaseDrawerProps {
  releaseId: string;
  repositoryName: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** Fires after any action that changed the release, so a list behind this
   * drawer (ReleasesCard, the task's compact row) can refresh. */
  onChanged?: () => void;
}

type PendingAction = "deploy" | "finish" | "rollback" | null;

/**
 * One release's full record: what shipped, how it was deployed, what
 * production looked like, and the verdict — plus the human actions the
 * server allows for its current status. Actions never move faster than the
 * server: the button set is a hint, the 409 the server answers when a status
 * raced past it is what actually governs.
 */
export function ReleaseDrawer({ releaseId, repositoryName, open, onOpenChange, onChanged }: ReleaseDrawerProps) {
  const { t } = useI18n();
  const navigate = useNavigate();
  const [release, setRelease] = useState<Release | null>(null);
  const [loading, setLoading] = useState(true);
  const [pending, setPending] = useState<PendingAction>(null);
  const [note, setNote] = useState("");
  const [typed, setTyped] = useState("");
  const [busy, setBusy] = useState(false);

  const load = useCallback(async () => {
    try {
      setRelease(await api.getRelease(releaseId));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("release.taskDetail.loadFailed"));
    } finally {
      setLoading(false);
    }
  }, [releaseId, t]);

  useEffect(() => {
    if (!open) return;
    setLoading(true);
    void load();
  }, [open, load]);

  // The sweeper moves a watched release without anyone in this drawer acting
  // on it; poll so the status shown here does not go stale while it is open.
  useEffect(() => {
    if (!open || !release || !RELEASE_ACTIVE_STATUSES.includes(release.status)) return;
    const id = setInterval(() => void load(), 15_000);
    return () => clearInterval(id);
  }, [open, release, load]);

  const openAction = (action: PendingAction) => {
    setNote("");
    setTyped("");
    setPending(action);
  };

  const confirmed = typed.trim() === repositoryName;
  const noteOK = pending !== "rollback" || note.trim() !== "";

  const runAction = async () => {
    if (!release || !pending || !confirmed || !noteOK) return;
    setBusy(true);
    try {
      const updated =
        pending === "deploy"
          ? await api.deployRelease(release.id, repositoryName)
          : pending === "finish"
            ? await api.finishRelease(release.id, repositoryName, note.trim())
            : await api.rollbackRelease(release.id, repositoryName, note.trim());
      setRelease(updated);
      toast.success(t(`release.drawer.actions.${pending}Succeeded`));
      setPending(null);
      onChanged?.();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("common.actionFailed"));
    } finally {
      setBusy(false);
    }
  };

  const canDeploy = release?.mode === "dispatch" && release.status === "pending";
  const canFinish = release?.status === "awaiting_verdict" || release?.status === "failed";
  const canRollback = release?.status === "failed" || release?.status === "awaiting_verdict" || release?.status === "released";

  return (
    <>
      <Dialog open={open} onOpenChange={onOpenChange}>
        <DialogContent className="flex h-[85vh] max-w-3xl flex-col gap-0 overflow-hidden p-0">
          {loading || !release ? (
            <div className="space-y-3 p-6">
              <Skeleton className="h-8 w-1/2" />
              <Skeleton className="h-40 w-full" />
            </div>
          ) : (
            <>
              <DialogHeader className="border-b border-border px-6 py-4">
                <div className="flex flex-wrap items-center gap-2 pr-6">
                  <DialogTitle className="text-left font-mono">{release.version}</DialogTitle>
                  <Badge variant={RELEASE_STATUS_VARIANT[release.status]}>{t(`release.statuses.${release.status}`)}</Badge>
                  <Badge variant="outline">{t(`release.modes.${release.mode}`)}</Badge>
                  {release.executor && <Badge variant="outline">{t(`release.executors.${release.executor}`)}</Badge>}
                </div>
                <div className="mt-2 flex flex-wrap items-center gap-3 text-left text-caption text-muted-foreground">
                  {release.commit_sha && (
                    <span>
                      {t("release.drawer.commit")}: <span className="font-mono">{release.commit_sha.slice(0, 12)}</span>
                    </span>
                  )}
                  {release.tag && (
                    <span>
                      {t("release.drawer.tag")}: <span className="font-mono">{release.tag}</span>
                    </span>
                  )}
                </div>
              </DialogHeader>

              <ScrollArea className="min-h-0 flex-1">
                <div className="space-y-6 px-6 py-5">
                  <section className="space-y-2">
                    <Label className="text-muted-foreground">{t("release.drawer.tasks")}</Label>
                    <div className="flex flex-wrap gap-2">
                      {release.tasks.map((task) => (
                        <button
                          key={task.id}
                          type="button"
                          onClick={() => navigate(`/board?task=${task.id}`)}
                          className="rounded-md border border-border px-2 py-1 text-caption transition-colors hover:bg-muted/60"
                        >
                          <span className="font-mono">{task.key ?? task.id.slice(0, 8)}</span>
                          {task.title && <span className="ml-1.5 text-muted-foreground">{task.title}</span>}
                        </button>
                      ))}
                    </div>
                  </section>

                  <Separator />

                  <section className="space-y-2">
                    <Label className="text-muted-foreground">{t("release.drawer.timeline.title")}</Label>
                    <dl className="grid grid-cols-2 gap-x-4 gap-y-1.5 text-caption sm:grid-cols-3">
                      <TimelineRow label={t("release.drawer.timeline.created")} at={release.created_at} />
                      <TimelineRow label={t("release.drawer.timeline.deployStarted")} at={release.deploy_started_at} />
                      <TimelineRow label={t("release.drawer.timeline.deployed")} at={release.deployed_at} />
                      <TimelineRow label={t("release.drawer.timeline.verifyUntil")} at={release.verify_until} />
                      <TimelineRow label={t("release.drawer.timeline.finished")} at={release.finished_at} />
                    </dl>
                  </section>

                  {release.deploy && (
                    <>
                      <Separator />
                      <section className="space-y-2">
                        <Label className="text-muted-foreground">{t("release.drawer.deployStatus.title")}</Label>
                        <div className="flex flex-wrap items-center gap-2">
                          <Badge variant={DEPLOY_STATE_VARIANT[release.deploy.state]}>{release.deploy.state}</Badge>
                          {release.deploy.detail && <span className="text-caption text-muted-foreground">{release.deploy.detail}</span>}
                          {release.deploy.run_url && (
                            <a
                              href={release.deploy.run_url}
                              target="_blank"
                              rel="noreferrer"
                              className="inline-flex items-center gap-1 text-caption text-info hover:underline"
                            >
                              {t("release.drawer.deployStatus.runLink")}
                              <ExternalLink className="h-3 w-3" />
                            </a>
                          )}
                        </div>
                      </section>
                    </>
                  )}

                  <Separator />

                  <section className="space-y-2">
                    <Label className="text-muted-foreground">{t("release.drawer.health.title")}</Label>
                    {(release.checks.health?.length ?? 0) === 0 ? (
                      <p className="text-caption text-muted-foreground">{t("release.drawer.health.empty")}</p>
                    ) : (
                      <>
                        <p className="text-caption text-muted-foreground">
                          {t("release.drawer.health.summary", {
                            ok: release.checks.health!.filter((h) => h.ok).length,
                            failed: release.checks.health!.filter((h) => !h.ok).length,
                          })}
                        </p>
                        <div className="divide-y divide-border rounded-lg border border-border">
                          {[...release.checks.health!].slice(-10).reverse().map((sample, i) => (
                            <div key={i} className="flex items-center gap-3 px-3 py-1.5 text-caption">
                              <span className="text-muted-foreground">{formatRelativeDate(sample.at)}</span>
                              <Badge variant={sample.ok ? "success" : "destructive"} className="shrink-0">
                                {sample.status ?? (sample.ok ? "ok" : "error")}
                              </Badge>
                              {sample.latency_ms !== undefined && <span className="text-muted-foreground">{sample.latency_ms}ms</span>}
                              {sample.error && <span className="truncate text-destructive">{sample.error}</span>}
                            </div>
                          ))}
                        </div>
                      </>
                    )}
                  </section>

                  <Separator />

                  <section className="space-y-2">
                    <Label className="text-muted-foreground">{t("release.drawer.smoke.title")}</Label>
                    {(release.checks.smoke?.length ?? 0) === 0 ? (
                      <p className="text-caption text-muted-foreground">{t("release.drawer.smoke.empty")}</p>
                    ) : (
                      <div className="divide-y divide-border rounded-lg border border-border">
                        {release.checks.smoke!.map((result, i) => (
                          <div key={i} className="flex flex-wrap items-center gap-3 px-3 py-1.5 text-caption">
                            <Badge variant={result.ok ? "success" : "destructive"} className="shrink-0">
                              {result.status ?? (result.ok ? "ok" : "error")}
                            </Badge>
                            <span className="font-mono">
                              {result.check.method ?? "GET"} {result.check.path}
                            </span>
                            {result.latency_ms !== undefined && <span className="text-muted-foreground">{result.latency_ms}ms</span>}
                            <span className="ml-auto text-muted-foreground">{formatRelativeDate(result.at)}</span>
                            {result.error && <span className="w-full truncate text-destructive">{result.error}</span>}
                          </div>
                        ))}
                      </div>
                    )}
                  </section>

                  <Separator />

                  <section className="space-y-2">
                    <Label className="text-muted-foreground">{t("release.drawer.newErrors.title")}</Label>
                    {(release.checks.new_errors?.length ?? 0) === 0 ? (
                      <p className="text-caption text-muted-foreground">{t("release.drawer.newErrors.empty")}</p>
                    ) : (
                      <div className="divide-y divide-border rounded-lg border border-border">
                        {release.checks.new_errors!.map((group) => (
                          <div key={group.fingerprint} className="flex flex-wrap items-center gap-3 px-3 py-1.5 text-caption">
                            <span className="min-w-0 flex-1 truncate font-mono">{group.message}</span>
                            <Badge variant="destructive">{group.count}</Badge>
                            <span className="text-muted-foreground">{t("release.drawer.newErrors.firstSeen", { time: formatRelativeDate(group.first_seen) })}</span>
                            {group.external_url && (
                              <a href={group.external_url} target="_blank" rel="noreferrer" className="text-info hover:underline">
                                <ExternalLink className="h-3 w-3" />
                              </a>
                            )}
                          </div>
                        ))}
                      </div>
                    )}
                  </section>

                  {(release.checks.notes?.length ?? 0) > 0 && (
                    <section className="space-y-2">
                      <Label className="text-muted-foreground">{t("release.drawer.notes")}</Label>
                      <ul className="list-inside list-disc space-y-1 text-caption text-muted-foreground">
                        {release.checks.notes!.map((n, i) => (
                          <li key={i}>{n}</li>
                        ))}
                      </ul>
                    </section>
                  )}

                  {release.checks.early_stop && (
                    <section className="space-y-1">
                      <Label className="text-muted-foreground">{t("release.drawer.earlyStop")}</Label>
                      <p className="text-caption">{release.checks.early_stop}</p>
                    </section>
                  )}

                  {release.verdict && (
                    <section className="space-y-1">
                      <Label className="text-muted-foreground">{t("release.drawer.verdict")}</Label>
                      <p className="text-caption">{release.verdict}</p>
                    </section>
                  )}

                  {release.failure_reason && (
                    <section className="space-y-1">
                      <Label className="text-muted-foreground">{t("release.drawer.failureReason")}</Label>
                      <p className="text-caption text-destructive">{release.failure_reason}</p>
                    </section>
                  )}

                  {release.rollback && (
                    <>
                      <Separator />
                      <section className="space-y-2">
                        <Label className="text-muted-foreground">{t("release.drawer.rollback.title")}</Label>
                        <dl className="grid grid-cols-2 gap-x-4 gap-y-1.5 text-caption sm:grid-cols-3">
                          <div>
                            <dt className="text-muted-foreground">{t("release.drawer.rollback.reason")}</dt>
                            <dd>{t(`release.drawer.rollback.reasons.${release.rollback.reason}`)}</dd>
                          </div>
                          {release.rollback.mechanism && (
                            <div>
                              <dt className="text-muted-foreground">{t("release.drawer.rollback.mechanism")}</dt>
                              <dd>{t(`release.drawer.rollback.mechanisms.${release.rollback.mechanism}`)}</dd>
                            </div>
                          )}
                          {release.rollback.revert_sha && (
                            <div>
                              <dt className="text-muted-foreground">{t("release.drawer.rollback.revertSha")}</dt>
                              <dd className="font-mono">{release.rollback.revert_sha.slice(0, 12)}</dd>
                            </div>
                          )}
                          {release.rollback.restored_ref && (
                            <div>
                              <dt className="text-muted-foreground">{t("release.drawer.rollback.restoredRef")}</dt>
                              <dd className="font-mono">{release.rollback.restored_ref}</dd>
                            </div>
                          )}
                        </dl>
                        {release.rollback.note && <p className="text-caption">{release.rollback.note}</p>}
                        {(release.rollback.manual_steps?.length ?? 0) > 0 && (
                          <div className="space-y-1">
                            <p className="text-caption text-muted-foreground">{t("release.drawer.rollback.manualSteps")}</p>
                            <ul className="list-inside list-disc space-y-1 text-caption">
                              {release.rollback.manual_steps!.map((step, i) => (
                                <li key={i}>{step}</li>
                              ))}
                            </ul>
                          </div>
                        )}
                        {release.rollback.detail && <p className="text-caption text-muted-foreground">{release.rollback.detail}</p>}
                      </section>
                    </>
                  )}

                  <Separator />

                  <section className="flex flex-wrap gap-2">
                    {canDeploy && <Button onClick={() => openAction("deploy")}>{t("release.drawer.actions.deploy")}</Button>}
                    {canFinish && (
                      <Button variant="outline" onClick={() => openAction("finish")}>
                        {t("release.drawer.actions.markReleased")}
                      </Button>
                    )}
                    {canRollback && (
                      <Button variant="destructive" onClick={() => openAction("rollback")}>
                        {t("release.drawer.actions.rollback")}
                      </Button>
                    )}
                  </section>
                </div>
              </ScrollArea>
            </>
          )}
        </DialogContent>
      </Dialog>

      <Dialog open={pending !== null} onOpenChange={(o) => !o && setPending(null)}>
        {pending && release && (
          <DialogContent>
            <DialogHeader>
              <DialogTitle>
                {pending === "deploy"
                  ? t("release.drawer.actions.confirmDeployTitle", { version: release.version })
                  : pending === "finish"
                    ? t("release.drawer.actions.confirmFinishTitle", { version: release.version })
                    : t("release.drawer.actions.confirmRollbackTitle", { version: release.version })}
              </DialogTitle>
              <DialogDescription>
                {pending === "deploy"
                  ? t("release.drawer.actions.confirmDeployDesc", { repository: repositoryName })
                  : pending === "finish"
                    ? t("release.drawer.actions.confirmFinishDesc")
                    : t("release.drawer.actions.confirmRollbackDesc", { repository: repositoryName })}
              </DialogDescription>
            </DialogHeader>

            {pending !== "deploy" && (
              <div className="space-y-1.5">
                <Label htmlFor="release-action-note">{t("release.drawer.actions.note")}</Label>
                <Textarea
                  id="release-action-note"
                  value={note}
                  onChange={(e) => setNote(e.target.value)}
                  placeholder={t("release.drawer.actions.notePlaceholder")}
                  rows={3}
                />
                {pending === "rollback" && !noteOK && (
                  <p className="text-micro text-destructive">{t("release.drawer.actions.noteRequired")}</p>
                )}
              </div>
            )}

            <div className="space-y-1.5">
              <Label htmlFor="release-action-confirm">{t("frame.ui.confirm.typeToConfirm", { phrase: repositoryName })}</Label>
              <Input
                id="release-action-confirm"
                value={typed}
                onChange={(e) => setTyped(e.target.value)}
                placeholder={repositoryName}
                autoComplete="off"
              />
            </div>

            <DialogFooter>
              <Button variant="outline" onClick={() => setPending(null)} disabled={busy}>
                {t("common.cancel")}
              </Button>
              <Button
                variant={pending === "rollback" ? "destructive" : "default"}
                disabled={busy || !confirmed || !noteOK}
                onClick={() => void runAction()}
              >
                {pending === "deploy"
                  ? t("release.drawer.actions.deploy")
                  : pending === "finish"
                    ? t("release.drawer.actions.markReleased")
                    : t("release.drawer.actions.rollback")}
              </Button>
            </DialogFooter>
          </DialogContent>
        )}
      </Dialog>
    </>
  );
}

function TimelineRow({ label, at }: { label: string; at?: string }) {
  if (!at) return null;
  return (
    <div>
      <dt className="text-muted-foreground">{label}</dt>
      <dd title={formatDate(at)}>{formatRelativeDate(at)}</dd>
    </div>
  );
}
