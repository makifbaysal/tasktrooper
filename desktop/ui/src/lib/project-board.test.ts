import { CircleDashed, Clock } from "lucide-react";
import { describe, expect, it } from "vitest";
import type { TaskTypeDef } from "@/api";
import {
  BOARD_ACTIVITY_POLL_ACTIVE_MS,
  BOARD_ACTIVITY_POLL_IDLE_MS,
  BOARD_TASK_POLL_ACTIVE_MS,
  BOARD_TASK_POLL_IDLE_MS,
  beforeDeploySteps,
  boardPollIntervals,
  deployOrderBlockerLabel,
  filterTasksByScope,
  formatColumnAge,
  isBeforeDeployPending,
  isMergeHold,
  pipelineStatusVariant,
  projectScopeCounts,
  runStatusVariant,
  taskBelongsToProject,
  taskCreateDefaults,
  taskHasNoProject,
  taskPipelineCardIcon,
  taskTypeLabel,
  taskTypeOptions,
  workOrderBlockerLabel,
} from "@/lib/project-board";

const ORDER_NOTE =
  "<!-- tt:order -->\n**Release order (generated from this task's relations — do not edit by hand):**\n" +
  "- Ships after: T-100 (CMS API/auth foundation (serverless, GitHub Contents API)).\n<!-- /tt:order -->";

describe("beforeDeploySteps", () => {
  it("drops the generated order note", () => {
    expect(beforeDeploySteps(ORDER_NOTE)).toBe("");
  });

  it("keeps what a human wrote around the order note", () => {
    expect(beforeDeploySteps(`${ORDER_NOTE}\n\nRun the backfill`)).toBe("Run the backfill");
  });

  it("treats an unterminated note as running to the end, like the server", () => {
    expect(beforeDeploySteps("Rotate the key\n<!-- tt:order -->\n- Ships after: T-1")).toBe("Rotate the key");
  });

  it("reads a missing field as no steps", () => {
    expect(beforeDeploySteps(undefined)).toBe("");
  });
});

describe("isBeforeDeployPending", () => {
  it("never asks to confirm an order note alone", () => {
    expect(isBeforeDeployPending({ before_deploy: ORDER_NOTE, before_deploy_confirmed_at: null })).toBe(false);
  });

  it("asks to confirm human steps until they are confirmed", () => {
    const before_deploy = `${ORDER_NOTE}\n\nRun the backfill`;
    expect(isBeforeDeployPending({ before_deploy, before_deploy_confirmed_at: null })).toBe(true);
    expect(isBeforeDeployPending({ before_deploy, before_deploy_confirmed_at: "2026-10-05T15:40:00Z" })).toBe(false);
  });
});

describe("isMergeHold", () => {
  it("knows the three merge holds and nothing else", () => {
    expect(isMergeHold("deploy_order")).toBe(true);
    expect(isMergeHold("before_deploy")).toBe(true);
    expect(isMergeHold("delivery_profile")).toBe(true);
    expect(isMergeHold("work_order")).toBe(false);
    expect(isMergeHold(undefined)).toBe(false);
  });
});

describe("deployOrderBlockerLabel", () => {
  it("names the task the merge waits for, even with parentheses in its title", () => {
    expect(
      deployOrderBlockerLabel("T-100 (CMS API/auth foundation (serverless, GitHub Contents API)) [done]"),
    ).toBe("Merge waits for T-100");
  });

  it("counts the rest", () => {
    expect(deployOrderBlockerLabel("T-100 (CMS) [done], T-99 (Auth) [in_qa]")).toBe("Merge waits for T-100 +1");
  });

  it("falls back to the resource label when the detail is empty", () => {
    expect(deployOrderBlockerLabel("")).toBe("Merge waits for the deploy order");
  });
});

describe("workOrderBlockerLabel", () => {
  it("shows the single blocker's task key", () => {
    expect(workOrderBlockerLabel("waiting for T-12 (API migration) [in_progress] to finish")).toBe("T-12");
  });

  it("shows the first key with a +N count for multiple blockers", () => {
    expect(
      workOrderBlockerLabel(
        "waiting for T-12 (API migration) [in_progress], T-15 (schema) [code_review] to finish",
      ),
    ).toBe("T-12 +1");
  });

  it("falls back to the generic label when blocked_question is empty", () => {
    expect(workOrderBlockerLabel("")).toBe("Waiting for blocking tasks");
  });

  it("does not pick up a key-shaped token from the blocker's title", () => {
    expect(
      workOrderBlockerLabel(
        "waiting for T-8 (Fix: schema drift (T-9 ile ilişkili)) [in_progress] to finish",
      ),
    ).toBe("T-8");
  });

  it("does not miscount blockers when a title contains a key-shaped token", () => {
    expect(
      workOrderBlockerLabel(
        "waiting for T-8 (Fix: schema drift (T-9 ile ilişkili)) [in_progress], T-15 (schema) [code_review] to finish",
      ),
    ).toBe("T-8 +1");
  });

  it("does not miscount a single blocker whose title contains a literal comma", () => {
    expect(
      workOrderBlockerLabel("waiting for T-3 (Refactor, cleanup) [todo] to finish"),
    ).toBe("T-3");
  });

  it("splits blockers correctly when an earlier title contains a literal comma", () => {
    expect(
      workOrderBlockerLabel(
        "waiting for T-3 (Refactor, cleanup) [todo], T-15 (schema) [code_review] to finish",
      ),
    ).toBe("T-3 +1");
  });
});

describe("pipelineStatusVariant", () => {
  it("renders running and pending as the info variant, distinct from a primary action", () => {
    expect(pipelineStatusVariant("running")).toBe("info");
    expect(pipelineStatusVariant("pending")).toBe("info");
  });

  it("still renders success/failed/skipped in their existing variants", () => {
    expect(pipelineStatusVariant("success")).toBe("success");
    expect(pipelineStatusVariant("failed")).toBe("destructive");
    expect(pipelineStatusVariant("skipped")).toBe("secondary");
  });
});

describe("runStatusVariant", () => {
  it("renders running and pending as the info variant, not warning, matching pipelineStatusVariant", () => {
    expect(runStatusVariant("running")).toBe("info");
    expect(runStatusVariant("pending")).toBe("info");
  });

  it("renders completed as success, failed as destructive and cancelled as secondary", () => {
    expect(runStatusVariant("completed")).toBe("success");
    expect(runStatusVariant("failed")).toBe("destructive");
    expect(runStatusVariant("cancelled")).toBe("secondary");
  });

  it("falls back to secondary for an unknown status", () => {
    expect(runStatusVariant("unknown")).toBe("secondary");
  });
});

describe("taskTypeOptions / taskTypeLabel", () => {
  it("falls back to the four built-ins when no task_types list has loaded", () => {
    expect(taskTypeOptions(undefined).map((o) => o.value)).toEqual(["task", "analiz", "bug", "technical"]);
    expect(taskTypeOptions([]).map((o) => o.value)).toEqual(["task", "analiz", "bug", "technical"]);
  });

  it("labels a built-in type from the locale when no list is loaded", () => {
    expect(taskTypeLabel("technical")).toBe("Technical");
  });

  it("orders options by the server list's position and prefers its labels", () => {
    const types: TaskTypeDef[] = [
      {
        key: "bug",
        label: "Bug",
        key_prefix: "B",
        position: 1,
        is_default: false,
        is_defect: true,
        assignee_mode: "none",
        behaviours: [],
        built_in: true,
        task_count: 0,
      },
      {
        key: "task",
        label: "Task",
        key_prefix: "T",
        position: 0,
        is_default: true,
        is_defect: false,
        assignee_mode: "none",
        behaviours: [],
        built_in: true,
        task_count: 0,
      },
      {
        key: "spike",
        label: "Spike",
        key_prefix: "SP",
        position: 2,
        is_default: false,
        is_defect: false,
        assignee_mode: "none",
        behaviours: [],
        built_in: false,
        task_count: 0,
      },
    ];
    expect(taskTypeOptions(types).map((o) => o.value)).toEqual(["task", "bug", "spike"]);
    expect(taskTypeOptions(types).map((o) => o.label)).toEqual(["Task", "Bug", "Spike"]);
  });

  it("prefers the server's custom label over the locale once a built-in type is renamed", () => {
    const types: TaskTypeDef[] = [
      {
        key: "technical",
        label: "Ops",
        key_prefix: "TC",
        position: 3,
        is_default: false,
        is_defect: false,
        assignee_mode: "none",
        behaviours: [],
        built_in: true,
        task_count: 0,
      },
    ];
    expect(taskTypeLabel("technical", types)).toBe("Ops");
  });

  it("translates a built-in type whose server label is still the untouched English default", () => {
    const types: TaskTypeDef[] = [
      {
        key: "technical",
        label: "Technical",
        key_prefix: "TC",
        position: 3,
        is_default: false,
        is_defect: false,
        assignee_mode: "none",
        behaviours: [],
        built_in: true,
        task_count: 0,
      },
    ];
    expect(taskTypeLabel("technical", types)).toBe("Technical");
  });
});

describe("taskPipelineCardIcon", () => {
  it("colors the running/pending board-card icon with the info hue, not warning", () => {
    expect(taskPipelineCardIcon("running")?.className).toBe("text-info");
    expect(taskPipelineCardIcon("pending")?.className).toBe("text-info");
  });

  it("never animates a card icon, and tells waiting from running", () => {
    for (const status of ["pending", "running", "success", "failed", "skipped"]) {
      expect(taskPipelineCardIcon(status)?.className).not.toMatch(/animate-/);
    }
    expect(taskPipelineCardIcon("pending")?.Icon).toBe(Clock);
    expect(taskPipelineCardIcon("running")?.Icon).toBe(CircleDashed);
  });
});

describe("project scope membership", () => {
  const repositories = [
    { id: "repo-shop", project_ids: ["proj-shop"] },
    { id: "repo-shared", project_ids: ["proj-shop", "proj-ops"] },
    { id: "repo-loose", project_ids: [] },
    { id: "repo-bare" },
  ];
  const task = (repository_id: string, initiative_project_id?: string) => ({ repository_id, initiative_project_id });

  it("puts a task in the project it names", () => {
    expect(taskBelongsToProject(task("repo-loose", "proj-shop"), "proj-shop", repositories)).toBe(true);
  });

  it("falls back to the repository's projects when the task names none", () => {
    expect(taskBelongsToProject(task("repo-shop"), "proj-shop", repositories)).toBe(true);
    expect(taskBelongsToProject(task("repo-shared"), "proj-ops", repositories)).toBe(true);
    expect(taskBelongsToProject(task("repo-shop"), "proj-ops", repositories)).toBe(false);
  });

  it("lets a task's own project win over its repository's membership", () => {
    const explicit = task("repo-shop", "proj-ops");
    expect(taskBelongsToProject(explicit, "proj-ops", repositories)).toBe(true);
    expect(taskBelongsToProject(explicit, "proj-shop", repositories)).toBe(false);
  });

  it("counts a task as project-less only when neither it nor its repository has one", () => {
    expect(taskHasNoProject(task("repo-loose"), repositories)).toBe(true);
    expect(taskHasNoProject(task("repo-bare"), repositories)).toBe(true);
    expect(taskHasNoProject(task("repo-unknown"), repositories)).toBe(true);
    expect(taskHasNoProject(task("repo-shop"), repositories)).toBe(false);
    expect(taskHasNoProject(task("repo-loose", "proj-shop"), repositories)).toBe(false);
  });

  it("filters by scope and counts every scope at once", () => {
    const tasks = [
      task("repo-shop"),
      task("repo-shared"),
      task("repo-shop", "proj-ops"),
      task("repo-loose"),
    ];
    expect(filterTasksByScope(tasks, "all", repositories)).toBe(tasks);
    expect(filterTasksByScope(tasks, "proj-shop", repositories)).toEqual([tasks[0], tasks[1]]);
    expect(filterTasksByScope(tasks, "proj-ops", repositories)).toEqual([tasks[1], tasks[2]]);
    expect(filterTasksByScope(tasks, "none", repositories)).toEqual([tasks[3]]);
    expect(projectScopeCounts(tasks, repositories)).toEqual({
      all: 4,
      none: 1,
      byProject: { "proj-shop": 2, "proj-ops": 2 },
    });
  });
});

describe("taskCreateDefaults", () => {
  const repositories = [
    { id: "repo-a", project_ids: [] },
    { id: "repo-b", project_ids: ["proj-1"] },
    { id: "repo-c", project_ids: ["proj-2"] },
    { id: "repo-d", project_ids: ["proj-2"] },
  ];

  it("leaves everything alone outside a project scope", () => {
    for (const scope of ["all", "none"]) {
      const defaults = taskCreateDefaults(repositories, scope, "repo-a");
      expect(defaults.repositories).toBe(repositories);
      expect(defaults.repositoryId).toBe("repo-a");
      expect(defaults.initiativeProjectId).toBeUndefined();
    }
  });

  it("preselects the project and its only repository", () => {
    const defaults = taskCreateDefaults(repositories, "proj-1", "repo-a");
    expect(defaults.initiativeProjectId).toBe("proj-1");
    expect(defaults.repositoryId).toBe("repo-b");
    expect(defaults.repositories.map((r) => r.id)).toEqual(["repo-b", "repo-a", "repo-c", "repo-d"]);
  });

  it("puts a multi-repository project's repositories first", () => {
    const defaults = taskCreateDefaults(repositories, "proj-2", "repo-a");
    expect(defaults.repositoryId).toBe("repo-c");
    expect(defaults.repositories.map((r) => r.id)).toEqual(["repo-c", "repo-d", "repo-a", "repo-b"]);
  });

  it("keeps the page's repository for a project that has none", () => {
    const defaults = taskCreateDefaults(repositories, "proj-empty", "repo-a");
    expect(defaults.initiativeProjectId).toBe("proj-empty");
    expect(defaults.repositoryId).toBe("repo-a");
    expect(defaults.repositories).toBe(repositories);
  });
});

describe("boardPollIntervals", () => {
  const card = (column: string, agent_running?: boolean) => ({ column, agent_running });
  const none = new Set<string>();

  it("polls slowly when nothing on the board is moving", () => {
    expect(boardPollIntervals([card("todo"), card("done", false)], none)).toEqual({
      tasksMs: BOARD_TASK_POLL_IDLE_MS,
      activityMs: BOARD_ACTIVITY_POLL_IDLE_MS,
    });
    expect(BOARD_TASK_POLL_IDLE_MS).toBe(15000);
    expect(BOARD_ACTIVITY_POLL_IDLE_MS).toBe(15000);
  });

  it("keeps the cards fast while one is in progress, without speeding up the activity feed", () => {
    expect(boardPollIntervals([card("todo"), card("in_progress")], none)).toEqual({
      tasksMs: BOARD_TASK_POLL_ACTIVE_MS,
      activityMs: BOARD_ACTIVITY_POLL_IDLE_MS,
    });
    expect(BOARD_TASK_POLL_ACTIVE_MS).toBe(5000);
  });

  it("goes fast on both while the activity feed shows a running agent", () => {
    expect(boardPollIntervals([card("todo")], new Set(["t-1"]))).toEqual({
      tasksMs: BOARD_TASK_POLL_ACTIVE_MS,
      activityMs: BOARD_ACTIVITY_POLL_ACTIVE_MS,
    });
    expect(BOARD_ACTIVITY_POLL_ACTIVE_MS).toBe(2000);
  });

  it("goes fast on both while the task list says an agent is running", () => {
    expect(boardPollIntervals([card("review", true)], none)).toEqual({
      tasksMs: BOARD_TASK_POLL_ACTIVE_MS,
      activityMs: BOARD_ACTIVITY_POLL_ACTIVE_MS,
    });
  });
});

describe("formatColumnAge", () => {
  const entered = "2026-10-01T10:00:00Z";
  const at = (minutes: number) => Date.parse(entered) + minutes * 60000;

  it("reads minutes, then hours, then days", () => {
    expect(formatColumnAge(entered, at(0))).toBe("0m");
    expect(formatColumnAge(entered, at(59))).toBe("59m");
    expect(formatColumnAge(entered, at(60))).toBe("1h");
    expect(formatColumnAge(entered, at(47 * 60))).toBe("47h");
    expect(formatColumnAge(entered, at(48 * 60))).toBe("2d");
  });

  it("keeps the same text across a minute tick once past the first hour", () => {
    expect(formatColumnAge(entered, at(180))).toBe(formatColumnAge(entered, at(181)));
  });
});
