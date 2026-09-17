import { ExternalLink, Loader2, Play, Square } from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { api, type BoardTask, type LocalPreview } from "@/api";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { useI18n } from "@/hooks/useI18n";
import { usePolling } from "@/hooks/usePolling";

interface LocalPreviewPanelProps {
  task: BoardTask;
  repositoryId: string;
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
export function LocalPreviewPanel({ task, repositoryId }: LocalPreviewPanelProps) {
  const { t } = useI18n();
  const [preview, setPreview] = useState<LocalPreview | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

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

  const start = async () => {
    setBusy(true);
    setError("");
    try {
      setPreview(await api.startLocalPreview(repositoryId, task.id));
    } catch (e) {
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
        {isThisTask ? (
          <>
            {statusBadge(preview.status)}
            {preview.url && (
              <a
                href={preview.url}
                target="_blank"
                rel="noreferrer"
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
            <Button size="sm" variant="outline" onClick={() => void start()} disabled={busy}>
              {busy ? (
                <Loader2 className="mr-1.5 h-3.5 w-3.5 animate-spin" />
              ) : (
                <Play className="mr-1.5 h-3.5 w-3.5" />
              )}
              {t("boardArea.components.taskDetail.runLocally")}
            </Button>
            {preview && <span className="text-xs text-muted-foreground">{t("boardArea.components.taskDetail.previewOtherTask")}</span>}
          </>
        )}
      </div>
      {isThisTask && preview.status === "failed" && preview.detail && (
        <p className="text-xs text-destructive">{preview.detail}</p>
      )}
      {error && <p className="text-xs text-destructive">{error}</p>}
    </div>
  );
}
