import { useCallback, useEffect, useState } from "react";
import { api, type TaskPreview, type TaskPreviewStatus } from "@/api";
import { usePolling } from "@/hooks/usePolling";
import { keepEqual } from "@/lib/stableState";

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
    const next = res.previews ?? [];
    setPreviews((prev) => keepEqual(prev, next));
  }, [repositoryId, taskId]);

  useEffect(() => {
    setPreviews(null);
    if (!enabled) return;
    load().catch(() => setPreviews([]));
  }, [load, enabled]);

  const building = enabled && !!previews?.some((p) => ACTIVE_PREVIEW_STATUSES.includes(p.status));
  const poll = useCallback(() => load().catch(() => undefined), [load]);
  usePolling(poll, POLL_MS, building, { leading: false });

  return { previews, reload: load };
}
