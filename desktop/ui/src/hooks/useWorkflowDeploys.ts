import { useCallback, useEffect, useRef, useState } from "react";
import { api, type DeployEnv, type DeploymentRun, type MatrixView } from "@/api";
import {
  isWorkflowRunMoving,
  WORKFLOW_IDLE_POLL_MS,
  WORKFLOW_LIVE_POLL_MS,
  workflowDeployRows,
  workflowPollMs,
} from "@/components/operations/changes/workflowDeploys";
import { createOpsResource, useOpsResource } from "@/components/operations/shell/useOpsResource";
import { usePolling } from "@/hooks/usePolling";
import { keepEqual } from "@/lib/stableState";

const MATRIX = createOpsResource<MatrixView | null>(() => api.getDeployMatrix());

export const CACHE_DEPLOY_MATRIX = "operations.changes.workflowDeploys";

/** The workflow deploy matrix: every repository × environment with a deploy
 * target, its last run and what a rollback would restore. Read every 15 s
 * while a run is queued or in progress, every minute otherwise. */
export function useDeployMatrix() {
  const [intervalMs, setIntervalMs] = useState(WORKFLOW_IDLE_POLL_MS);
  const state = useOpsResource<MatrixView | null>(MATRIX, CACHE_DEPLOY_MATRIX, null, intervalMs);
  const next = workflowPollMs(workflowDeployRows(state.value));
  useEffect(() => setIntervalMs(next), [next]);
  return state;
}

/** One cell's run history while its panel is open; every 15 s while a run moves. */
export function useDeployRuns(target: { repositoryId: string; env: DeployEnv } | null) {
  const [runs, setRuns] = useState<DeploymentRun[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const seq = useRef(0);
  const repositoryId = target?.repositoryId;
  const env = target?.env;

  const load = useCallback(async () => {
    if (!repositoryId || !env) return;
    const ticket = ++seq.current;
    try {
      const next = (await api.listDeployRuns(repositoryId, env, 20)) ?? [];
      if (ticket !== seq.current) return;
      setRuns((prev) => keepEqual(prev, next));
      setError(null);
    } catch (err) {
      if (ticket !== seq.current) return;
      setError(err instanceof Error ? err.message : String(err));
    }
  }, [repositoryId, env]);

  useEffect(() => {
    seq.current += 1;
    setRuns(null);
    setError(null);
    void load();
  }, [load]);

  const moving = (runs ?? []).some((r) => isWorkflowRunMoving(r));
  usePolling(load, moving ? WORKFLOW_LIVE_POLL_MS : WORKFLOW_IDLE_POLL_MS, target !== null, { leading: false });

  return { runs, error, reload: load };
}
