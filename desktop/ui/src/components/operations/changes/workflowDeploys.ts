import type { DeployEnv, DeployEnvironment, DeploymentRun, MatrixCell, MatrixView } from "@/api";
import { environmentFromLabel } from "@/lib/environmentLabel";

/** A deploy target that runs a GitHub Actions workflow straight away,
 * outside release tracking (`/v1/operations/deployments`). */
export interface WorkflowDeployRow {
  key: string;
  repositoryId: string;
  repositoryName: string;
  env: DeployEnv;
  cell: MatrixCell;
}

export const WORKFLOW_LIVE_POLL_MS = 15_000;
export const WORKFLOW_IDLE_POLL_MS = 60_000;

/** Cells worth a row — one a deploy can be dispatched to, or one that ran
 * before — in the console's environment and, when given, one repository. */
export function workflowDeployRows(
  matrix: MatrixView | null | undefined,
  filter: { environment?: DeployEnvironment; repositoryId?: string } = {},
): WorkflowDeployRow[] {
  const envOrder = matrix?.envs ?? [];
  const rows: WorkflowDeployRow[] = [];
  for (const repo of matrix?.repos ?? []) {
    if (filter.repositoryId && repo.id !== filter.repositoryId) continue;
    for (const cell of repo.cells ?? []) {
      if (!cell.dispatchable && !cell.last_run) continue;
      if (filter.environment && environmentFromLabel(cell.env) !== filter.environment) continue;
      rows.push({ key: `${repo.id}:${cell.env}`, repositoryId: repo.id, repositoryName: repo.name, env: cell.env, cell });
    }
  }
  return rows.sort(
    (a, b) => a.repositoryName.localeCompare(b.repositoryName) || envOrder.indexOf(a.env) - envOrder.indexOf(b.env),
  );
}

export type WorkflowRunState = "queued" | "running" | "success" | "failure" | "cancelled" | "unknown";

export function workflowRunState(run: Pick<DeploymentRun, "status" | "conclusion">): WorkflowRunState {
  if (run.status === "queued") return "queued";
  if (run.status === "in_progress") return "running";
  switch (run.conclusion) {
    case "success":
      return "success";
    case "failure":
      return "failure";
    case "cancelled":
      return "cancelled";
    default:
      return "unknown";
  }
}

export function isWorkflowRunMoving(run: Pick<DeploymentRun, "status"> | undefined): boolean {
  return run?.status === "queued" || run?.status === "in_progress";
}

export function workflowPollMs(rows: WorkflowDeployRow[]): number {
  return rows.some((r) => isWorkflowRunMoving(r.cell.last_run)) ? WORKFLOW_LIVE_POLL_MS : WORKFLOW_IDLE_POLL_MS;
}

/** deployops.productionClass: a dispatch to prod needs the typed name; a
 * rollback always does, whatever the environment. */
export function dispatchNeedsPhrase(env: DeployEnv): boolean {
  return env === "prod";
}

/** The server re-dispatches the last successful run before the current one;
 * without one, or without a workflow to dispatch, there is nothing to roll back to. */
export function canRollBack(cell: Pick<MatrixCell, "dispatchable" | "rollback_sha">): boolean {
  return cell.dispatchable && Boolean(cell.rollback_sha);
}

export function runStartedAt(run: Pick<DeploymentRun, "started_at" | "created_at">): string {
  return run.started_at ?? run.created_at;
}

export function runDurationMs(run: Pick<DeploymentRun, "started_at" | "created_at" | "completed_at">): number | undefined {
  if (!run.completed_at) return undefined;
  const ms = Date.parse(run.completed_at) - Date.parse(runStartedAt(run));
  return Number.isFinite(ms) && ms > 0 ? ms : undefined;
}
