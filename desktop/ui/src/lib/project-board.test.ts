import { describe, expect, it } from "vitest";
import {
  pipelineStatusVariant,
  runStatusVariant,
  TASK_TYPE_OPTIONS,
  taskPipelineCardIcon,
  taskTypeLabel,
  workOrderBlockerLabel,
} from "@/lib/project-board";

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

describe("TASK_TYPE_OPTIONS / taskTypeLabel", () => {
  it("includes technical alongside task/analiz/bug", () => {
    expect(TASK_TYPE_OPTIONS.map((o) => o.value)).toEqual(["task", "analiz", "bug", "technical"]);
  });

  it("labels the technical type in the active locale", () => {
    expect(taskTypeLabel("technical")).toBe("Technical");
  });
});

describe("taskPipelineCardIcon", () => {
  it("colors the running/pending board-card icon with the info hue, not warning", () => {
    expect(taskPipelineCardIcon("running")?.className).toBe("text-info animate-spin");
    expect(taskPipelineCardIcon("pending")?.className).toBe("text-info animate-spin");
  });
});
