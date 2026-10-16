import { describe, expect, it } from "vitest";
import type { DeploymentRun, MatrixCell, MatrixView } from "@/api";
import {
  canRollBack,
  dispatchNeedsPhrase,
  workflowDeployRows,
  workflowPollMs,
  workflowRunState,
  WORKFLOW_IDLE_POLL_MS,
  WORKFLOW_LIVE_POLL_MS,
} from "@/components/operations/changes/workflowDeploys";

const run = (overrides: Partial<DeploymentRun> = {}): DeploymentRun =>
  ({ id: "run", status: "completed", conclusion: "success", head_sha: "abc1234", created_at: "2026-10-16T10:00:00Z", ...overrides }) as DeploymentRun;

const cell = (env: MatrixCell["env"], overrides: Partial<MatrixCell> = {}): MatrixCell => ({
  env,
  configured: true,
  dispatchable: false,
  provider: "",
  health_url: "",
  auto_rollback: false,
  rollback_sha: "",
  ...overrides,
});

const matrix: MatrixView = {
  envs: ["local", "stage", "preprod", "prod"],
  repos: [
    { id: "r-b", name: "billing", kind: "service", cells: [cell("prod", { dispatchable: true }), cell("stage")] },
    {
      id: "r-a",
      name: "api",
      kind: "service",
      cells: [cell("prod", { last_run: run() }), cell("stage", { dispatchable: true }), cell("preprod", { dispatchable: true })],
    },
  ],
};

describe("workflowDeployRows", () => {
  it("keeps cells that can be dispatched or ran before, by repository then environment order", () => {
    expect(workflowDeployRows(matrix).map((r) => r.key)).toEqual(["r-a:stage", "r-a:preprod", "r-a:prod", "r-b:prod"]);
  });

  it("narrows to the console's environment and one repository", () => {
    expect(workflowDeployRows(matrix, { environment: "staging" }).map((r) => r.key)).toEqual(["r-a:stage", "r-a:preprod"]);
    expect(workflowDeployRows(matrix, { environment: "production", repositoryId: "r-b" }).map((r) => r.key)).toEqual(["r-b:prod"]);
    expect(workflowDeployRows(null)).toEqual([]);
  });
});

describe("the server's rules", () => {
  it("asks the typed name for a prod dispatch only; a rollback always asks", () => {
    expect(dispatchNeedsPhrase("prod")).toBe(true);
    expect(dispatchNeedsPhrase("stage")).toBe(false);
  });

  it("rolls back only to an earlier successful run, through a mapped workflow", () => {
    expect(canRollBack({ dispatchable: true, rollback_sha: "abc" })).toBe(true);
    expect(canRollBack({ dispatchable: true, rollback_sha: "" })).toBe(false);
    expect(canRollBack({ dispatchable: false, rollback_sha: "abc" })).toBe(false);
  });
});

describe("runs", () => {
  it("reads a run's state from its status and conclusion", () => {
    expect(workflowRunState(run({ status: "queued", conclusion: "" }))).toBe("queued");
    expect(workflowRunState(run({ status: "in_progress", conclusion: "" }))).toBe("running");
    expect(workflowRunState(run({ conclusion: "failure" }))).toBe("failure");
    expect(workflowRunState(run({ conclusion: "" }))).toBe("unknown");
  });

  it("polls faster while a listed run moves", () => {
    expect(workflowPollMs(workflowDeployRows(matrix))).toBe(WORKFLOW_IDLE_POLL_MS);
    const moving: MatrixView = { ...matrix, repos: [{ ...matrix.repos![1], cells: [cell("prod", { last_run: run({ status: "in_progress" }) })] }] };
    expect(workflowPollMs(workflowDeployRows(moving))).toBe(WORKFLOW_LIVE_POLL_MS);
  });
});
