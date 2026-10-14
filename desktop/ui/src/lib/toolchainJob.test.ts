import { describe, expect, it } from "vitest";
import type { ToolchainJobState, ToolchainJobStep } from "@/lib/desktop-bridge";
import {
  ALREADY_INSTALLED,
  idsInFlight,
  isDeclined,
  jobOutcome,
  jobStepFor,
  jobSummary,
  jobTitleKey,
  stepSatisfied,
} from "@/lib/toolchainJob";

describe("toolchain job helpers", () => {
  const state: ToolchainJobState = {
    phase: "running",
    percent: 50,
    steps: [
      { id: "homebrew", label: "Homebrew", state: "done", installKind: "interactive" },
      { id: "node", label: "Node.js", state: "failed", installKind: "auto", error: "brew exited 1" },
      { id: "jdk", label: "JDK 17", state: "running", installKind: "auto" },
      { id: "android-sdk", label: "Android SDK", state: "queued", installKind: "auto" },
      { id: "xcode-clt", label: "Xcode CLT", state: "waiting", installKind: "interactive" },
      { id: "git", label: "Git", state: "skipped", installKind: "auto", error: ALREADY_INSTALLED },
      { id: "appium", label: "Appium", state: "skipped", installKind: "auto", error: "Needs Node.js, which did not install." },
    ],
  };

  it("counts finished, failed, incomplete and remaining steps", () => {
    expect(jobSummary(state)).toEqual({
      total: 7,
      done: 1,
      failed: 1,
      skipped: 2,
      cancelled: 0,
      incomplete: 1,
      remaining: 3,
      anyFailed: true,
    });
    expect(jobSummary(null)).toMatchObject({ total: 0, remaining: 0, anyFailed: false });
  });

  it("counts a step as satisfied only when its tool is now there", () => {
    const [homebrew, node, , , , git, appium] = state.steps;
    expect(stepSatisfied(homebrew)).toBe(true);
    expect(stepSatisfied(git)).toBe(true);
    expect(stepSatisfied(node)).toBe(false);
    expect(stepSatisfied(appium)).toBe(false);
  });

  it("knows which ids a running job still has in hand", () => {
    expect([...idsInFlight(state)].sort()).toEqual(["android-sdk", "jdk", "xcode-clt"]);
    expect(idsInFlight({ ...state, phase: "done" }).size).toBe(0);
  });

  it("finds a running job's step for an id, and none once it is done or dismissed", () => {
    expect(jobStepFor(state, "jdk")?.state).toBe("running");
    expect(jobStepFor({ ...state, phase: "done" }, "jdk")).toBeUndefined();
    expect(jobStepFor({ ...state, phase: "idle" }, "jdk")).toBeUndefined();
  });

  it("recognises the shell's declined rejection through Electron's prefix", () => {
    expect(isDeclined(new Error("Error invoking remote method 'cloud:toolchain:install': Error: declined"))).toBe(true);
    expect(isDeclined(new Error("brew exited 1"))).toBe(false);
  });
});

describe("jobOutcome", () => {
  const done = (...steps: Partial<ToolchainJobStep>[]): ToolchainJobState => ({
    phase: "done",
    percent: 100,
    steps: steps.map((step, i) => ({ id: "git", label: `Step ${i}`, state: "done", installKind: "auto", ...step })),
  });

  it("is complete only when every step is done or was already there", () => {
    const state = done({ state: "done" }, { state: "skipped", error: ALREADY_INSTALLED });
    expect(jobOutcome(state)).toBe("complete");
    expect(jobTitleKey(state)).toBe("toolchain.job.title.complete");
  });

  it("puts a failure first", () => {
    expect(jobOutcome(done({ state: "failed" }, { state: "cancelled" }, { state: "skipped", error: "Needs Node.js" }))).toBe(
      "failed",
    );
  });

  it("calls a job with a cancelled step stopped", () => {
    expect(jobOutcome(done({ state: "done" }, { state: "cancelled", detail: "Still finishing in the background…" }))).toBe(
      "stopped",
    );
  });

  it("calls a job with a step skipped for a missing dependency incomplete", () => {
    expect(jobOutcome(done({ state: "done" }, { state: "skipped", error: "Needs Node.js, which did not install." }))).toBe(
      "incomplete",
    );
  });

  it("follows the phase while it is not done", () => {
    expect(jobOutcome({ phase: "running", percent: 0, steps: [] })).toBe("running");
    expect(jobTitleKey({ phase: "running", percent: 0, steps: [] })).toBe("toolchain.job.title.running");
    expect(jobOutcome({ phase: "idle", percent: 0, steps: [] })).toBe("idle");
  });
});
