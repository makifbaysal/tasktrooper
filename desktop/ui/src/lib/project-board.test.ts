import { describe, expect, it } from "vitest";
import { workOrderBlockerLabel } from "@/lib/project-board";

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
});
