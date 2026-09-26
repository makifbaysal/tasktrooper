import { useCallback, useEffect, useState } from "react";
import { api, type TaskPreview, type TaskPreviewStatus } from "@/api";

const POLL_MS = 15_000;

export const ACTIVE_PREVIEW_STATUSES: TaskPreviewStatus[] = ["queued", "building"];

/**
 * The task's per-branch preview deployments. `previews` is null until the
 * first load settles; a failed first load reads as "no previews" so callers
 * render nothing rather than an error for a feature the repo may not use.
 * Polls only while a preview is still being built.
 */
export function useTaskPreviews(repositoryId: string, taskId: string, enabled = true) {
  const [previews, setPreviews] = useState<TaskPreview[] | null>(null);

  const load = useCallback(async () => {
    const res = await api.getTaskPreviews(repositoryId, taskId);
    setPreviews(res.previews ?? []);
  }, [repositoryId, taskId]);

  useEffect(() => {
    setPreviews(null);
    if (!enabled) return;
    load().catch(() => setPreviews([]));
  }, [load, enabled]);

  useEffect(() => {
    if (!enabled || !previews?.some((p) => ACTIVE_PREVIEW_STATUSES.includes(p.status))) return;
    const id = setInterval(() => void load().catch(() => {}), POLL_MS);
    return () => clearInterval(id);
  }, [previews, load, enabled]);

  return { previews, reload: load };
}
