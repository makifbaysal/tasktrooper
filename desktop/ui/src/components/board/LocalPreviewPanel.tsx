import { ExternalLink, Loader2, Play, Square } from "lucide-react";
import { type MouseEvent, type ReactNode, useCallback, useEffect, useRef, useState } from "react";
import { toast } from "sonner";
import { api, type BoardTask, type LocalPreview } from "@/api";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { useI18n } from "@/hooks/useI18n";
import { usePolling } from "@/hooks/usePolling";
import { useTaskPreviews } from "@/hooks/useTaskPreviews";
import { desktopRunner } from "@/lib/desktop-bridge";

const FAILED_TAIL_LINES = 12;

interface LocalPreviewPanelProps {
  task: BoardTask;
  repositoryId: string;
  /** Other ways to try the task, shown beside "Run locally" (e.g. its hosted preview). */
  actions?: ReactNode;
}

/**
 * "Run it locally" for a human_uat reviewer — a button that checks out the
 * task's branch (the same checkout its agent chat already works in) and runs
 * whatever dev/start command the repository is detected to have, so the
 * approve/decline call can be made against the real thing instead of a
 * reading of the diff. See server's localpreview.Service.
 *
 * Only one preview runs per repository — starting one for this task replaces
 * whatever else was running, which is why the panel also has to render the
 * case where the repository's active preview belongs to a DIFFERENT task.
 */
export function LocalPreviewPanel({ task, repositoryId, actions }: LocalPreviewPanelProps) {
  const { t } = useI18n();
  const [preview, setPreview] = useState<LocalPreview | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  // Set by a click on "Run locally": the reviewer asked to see the site, so
  // it opens once the dev server prints its address, not on a later visit.
  const openWhenReady = useRef(false);

  const refresh = useCallback(async () => {
    try {
      const res = await api.getLocalPreview(repositoryId);
      setPreview(res.active ? (res.preview ?? null) : null);
    } catch {
      /* leave the last known state: a failed poll is not "nothing running" */
    }
  }, [repositoryId]);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  // Only worth polling while something could still change — a stopped or
  // failed preview sits there until the user acts, so there is nothing for a
  // timer to catch.
  const settled = preview === null || preview.status === "stopped" || preview.status === "failed";
  usePolling(refresh, 3_000, !settled);

  const isThisTask = preview !== null && preview.task_id === task.id;
  const live = isThisTask && !settled;
  const failed = isThisTask && preview.status === "failed";
  const otherTaskLive = preview !== null && !isThisTask && !settled;

  // A hosted preview the PR already produced is a better way to look at the
  // task than waiting on a local dev server, so it leads when one is ready;
  // "Run locally" stays available but moves to a secondary spot and is never
  // triggered on its own.
  const { previews: hostedPreviews } = useTaskPreviews(repositoryId, task.id, !!task.pr_url);
  const hasReadyHostedPreview = (hostedPreviews ?? []).some(
    (p) => p.status === "ready" && (p.branch_url || p.url),
  );
  const hostedPreviewLeads = !live && hasReadyHostedPreview;

  const openPreview = useCallback(
    (url: string) => {
      const runner = desktopRunner();
      if (!runner) {
        window.open(url, "_blank", "noopener,noreferrer");
        return;
      }
      void runner.openExternal(url).then((opened) => {
        if (!opened) toast.error(t("boardArea.components.taskDetail.pullRequestLinkFailed"));
      });
    },
    [t],
  );

  useEffect(() => {
    if (!openWhenReady.current || !isThisTask) return;
    if (preview.status === "running" && preview.url) {
      openWhenReady.current = false;
      openPreview(preview.url);
    } else if (settled) {
      openWhenReady.current = false;
    }
  }, [isThisTask, preview, settled, openPreview]);

  // Desktop only: handed to the shell before the anchor can navigate, the
  // same way the task's PR link is (see TaskDetailDrawer).
  const handleLinkClick = (event: MouseEvent<HTMLAnchorElement>) => {
    if (!desktopRunner()) return;
    event.preventDefault();
    openPreview(event.currentTarget.href);
  };

  const start = async () => {
    setBusy(true);
    setError("");
    openWhenReady.current = true;
    try {
      setPreview(await api.startLocalPreview(repositoryId, task.id));
    } catch (e) {
      openWhenReady.current = false;
      setError(e instanceof Error ? e.message : t("boardArea.components.taskDetail.previewStartFailed"));
    } finally {
      setBusy(false);
    }
  };

  const stop = async () => {
    setBusy(true);
    try {
      await api.stopLocalPreview(repositoryId);
      setPreview(null);
    } finally {
      setBusy(false);
    }
  };

  const statusBadge = (status: LocalPreview["status"]) => {
    switch (status) {
      case "running":
        return <Badge variant="success">{t("boardArea.components.taskDetail.previewRunning")}</Badge>;
      case "failed":
        return <Badge variant="destructive">{t("boardArea.components.taskDetail.previewFailed")}</Badge>;
      default:
        return (
          <Badge variant="secondary" className="gap-1">
            <Loader2 className="h-3 w-3 animate-spin" />
            {t("boardArea.components.taskDetail.previewStarting")}
          </Badge>
        );
    }
  };

  return (
    <div className="space-y-2 rounded-lg border border-border bg-background/60 p-3">
      <div className="flex flex-wrap items-center gap-2">
        {live ? (
          <>
            {statusBadge(preview.status)}
            {preview.url && (
              <a
                href={preview.url}
                target="_blank"
                rel="noreferrer"
                onClick={handleLinkClick}
                className="inline-flex items-center gap-1 text-sm text-primary underline-offset-2 hover:underline"
              >
                {preview.url}
                <ExternalLink className="h-3 w-3" />
              </a>
            )}
            <Button size="sm" variant="outline" onClick={() => void stop()} disabled={busy}>
              <Square className="mr-1.5 h-3.5 w-3.5" />
              {t("boardArea.components.taskDetail.stopPreview")}
            </Button>
          </>
        ) : (
          <>
            {hostedPreviewLeads && actions}
            <Button size="sm" variant={hostedPreviewLeads ? "ghost" : "outline"} onClick={() => void start()} disabled={busy}>
              {busy ? (
                <Loader2 className="mr-1.5 h-3.5 w-3.5 animate-spin" />
              ) : (
                <Play className="mr-1.5 h-3.5 w-3.5" />
              )}
              {t("boardArea.components.taskDetail.runLocally")}
            </Button>
            {failed && statusBadge(preview.status)}
            {otherTaskLive && (
              <span className="text-xs text-muted-foreground">{t("boardArea.components.taskDetail.previewOtherTask")}</span>
            )}
          </>
        )}
        {!hostedPreviewLeads && actions}
      </div>
      {failed && (
        <div className="space-y-1">
          {preview.detail && <p className="text-xs text-destructive">{preview.detail}</p>}
          {preview.log_tail && preview.log_tail.length > 0 && (
            <pre className="max-h-48 overflow-auto whitespace-pre-wrap rounded border border-border bg-muted/40 p-2 font-mono text-[11px] leading-snug text-muted-foreground">
              {preview.log_tail.slice(-FAILED_TAIL_LINES).join("\n")}
            </pre>
          )}
        </div>
      )}
      {error && <p className="text-xs text-destructive">{error}</p>}
    </div>
  );
}
