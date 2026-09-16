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
