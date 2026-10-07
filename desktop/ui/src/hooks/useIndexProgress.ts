import { useCallback, useEffect, useRef, useState } from "react";
import { api, type WorkspaceIndex } from "@/api";
import { usePolling } from "@/hooks/usePolling";
import { keepEqual } from "@/lib/stableState";

function progressPercent(index: WorkspaceIndex | null): number {
  if (!index) return 0;
  if (index.files_total <= 0) {
    return index.status === "completed" ? 100 : 0;
  }
  return Math.min(100, Math.round((index.files_processed / index.files_total) * 100));
}

function isActiveIndexStatus(status: string | undefined): boolean {
  return status === "running" || status === "pending";
}

function isTerminalIndexStatus(status: string | undefined): boolean {
  return status === "completed" || status === "failed";
}

type UseIndexProgressOptions = {
  pollMs?: number;
  forcePoll?: boolean;
  onIndexingFinished?: () => void;
};

export function useIndexProgress(repositoryId: string | undefined, options: UseIndexProgressOptions = {}) {
  const { pollMs = 1500, forcePoll = false, onIndexingFinished } = options;
  const [index, setIndex] = useState<WorkspaceIndex | null>(null);
  const [loading, setLoading] = useState(false);
  const sawActiveIndexingRef = useRef(false);

  const refresh = useCallback(async () => {
    if (!repositoryId) return null;
    const data = await api.getRepositoryIndexStatus(repositoryId);
    setIndex((prev) => keepEqual(prev, data));
    return data;
  }, [repositoryId]);

  useEffect(() => {
    if (!repositoryId) {
      setIndex(null);
      return;
    }
    setLoading(true);
    refresh()
      .catch(() => setIndex(null))
      .finally(() => setLoading(false));
  }, [repositoryId, refresh]);

  useEffect(() => {
    if (!forcePoll) {
      sawActiveIndexingRef.current = false;
    }
  }, [forcePoll]);

  useEffect(() => {
    const status = index?.status;
    if (isActiveIndexStatus(status)) {
      sawActiveIndexingRef.current = true;
    }
    if (forcePoll && sawActiveIndexingRef.current && isTerminalIndexStatus(status)) {
      sawActiveIndexingRef.current = false;
      onIndexingFinished?.();
    }
  }, [index?.status, forcePoll, onIndexingFinished]);

  const shouldPoll =
    !!repositoryId &&
    (forcePoll || isActiveIndexStatus(index?.status));

  const poll = useCallback(() => refresh().then(() => undefined, () => undefined), [refresh]);
  usePolling(poll, pollMs, shouldPoll, { leading: false });

  return {
    index,
    loading,
    percent: progressPercent(index),
    refresh,
  };
}
