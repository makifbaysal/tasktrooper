import { useCallback, useEffect, useRef, useState } from "react";
import { api, type ReviewHistory } from "@/api";

const REFRESH_MS = 20_000;

/**
 * A task's review history, read when the drawer opens, when the task changes
 * column, and slowly while it stays open. The server reads git and traces to
 * build it, so it is kept out of the drawer's two-second poll.
 */
export function useReviewHistory(repositoryId: string | undefined, taskId: string | null, active: boolean, column: string | undefined) {
  const [history, setHistory] = useState<ReviewHistory | null>(null);
  const current = useRef(taskId);
  current.current = taskId;

  const reload = useCallback(async () => {
    if (!repositoryId || !taskId) return;
    try {
      const next = await api.getReviewHistory(repositoryId, taskId);
      if (current.current === taskId) setHistory(next);
    } catch {
      // The history is a reading aid; a failed read leaves the last one shown.
    }
  }, [repositoryId, taskId]);

  useEffect(() => {
    setHistory(null);
  }, [taskId]);

  useEffect(() => {
    if (!active || !taskId) return;
    void reload();
    const timer = window.setInterval(() => void reload(), REFRESH_MS);
    return () => window.clearInterval(timer);
  }, [active, taskId, column, reload]);

  return { history, reload };
}
