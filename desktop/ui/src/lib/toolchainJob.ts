// Pure helpers over the toolchain job the shell runs. Kept apart from
// toolchain.ts because the header chip needs them on every page.

import type { ToolchainId, ToolchainJobState, ToolchainJobStep, ToolchainStepState } from "@/lib/desktop-bridge";

export const TOOLCHAIN_SETTINGS_PATH = "/settings/environment";

const FINISHED_STEP_STATES: ReadonlySet<ToolchainStepState> = new Set(["done", "failed", "skipped", "cancelled"]);

export function stepFinished(step: Pick<ToolchainJobStep, "state">): boolean {
  return FINISHED_STEP_STATES.has(step.state);
}

/**
 * The shell's own sentence for a step skipped because the tool was already
 * there. Any other skip (a dependency that did not install) left it missing.
 */
export const ALREADY_INSTALLED = "Already installed.";

/** The step left its tool on this computer: installed now, or already there. */
export function stepSatisfied(step: Pick<ToolchainJobStep, "state" | "error">): boolean {
  return step.state === "done" || (step.state === "skipped" && step.error === ALREADY_INSTALLED);
}

export interface ToolchainJobSummary {
  total: number;
  done: number;
  failed: number;
  skipped: number;
  cancelled: number;
  /** Skipped for any reason but "already installed". */
  incomplete: number;
  /** Queued, running or waiting. */
  remaining: number;
  anyFailed: boolean;
}

export function jobSummary(state: Pick<ToolchainJobState, "steps"> | null | undefined): ToolchainJobSummary {
  const steps = state?.steps ?? [];
  const count = (wanted: ToolchainStepState) => steps.filter((step) => step.state === wanted).length;
  const failed = count("failed");
  return {
    total: steps.length,
    done: count("done"),
    failed,
    skipped: count("skipped"),
    cancelled: count("cancelled"),
    incomplete: steps.filter((step) => step.state === "skipped" && !stepSatisfied(step)).length,
    remaining: steps.filter((step) => !stepFinished(step)).length,
    anyFailed: failed > 0,
  };
}

export type ToolchainJobOutcome = "idle" | "running" | "failed" | "stopped" | "incomplete" | "complete";

/**
 * What a finished job amounts to, worst first: anything failed, else stopped
 * (a cancelled step), else incomplete (a step skipped because a dependency did
 * not install). "complete" only when every step is satisfied.
 */
export function jobOutcome(state: ToolchainJobState): ToolchainJobOutcome {
  if (state.phase !== "done") return state.phase;
  const summary = jobSummary(state);
  if (summary.failed > 0) return "failed";
  if (summary.cancelled > 0) return "stopped";
  if (summary.incomplete > 0) return "incomplete";
  return "complete";
}

/** The header popover's and the Settings job card's heading, following the outcome. */
export function jobTitleKey(state: ToolchainJobState): string {
  const outcome = jobOutcome(state);
  return `toolchain.job.title.${outcome === "idle" ? "complete" : outcome}`;
}

/** Ids a running job has yet to finish — their Install buttons have nothing left to start. */
export function idsInFlight(state: ToolchainJobState | null | undefined): ReadonlySet<ToolchainId> {
  if (!state || state.phase !== "running") return new Set();
  return new Set(state.steps.filter((step) => !stepFinished(step)).map((step) => step.id));
}

/**
 * The running job's step for an id. Only while it runs: the report predates
 * the job's progress until then, and the hook re-detects once it is done.
 */
export function jobStepFor(state: ToolchainJobState | null | undefined, id: ToolchainId): ToolchainJobStep | undefined {
  if (!state || state.phase !== "running") return undefined;
  return state.steps.find((step) => step.id === id);
}

/**
 * The account-mode shell asks before installing; Cancel there rejects with
 * "declined". Electron prefixes the remote error, so match the word.
 */
export function isDeclined(error: unknown): boolean {
  const message = error instanceof Error ? error.message : String(error ?? "");
  return /\bdeclined\b/i.test(message);
}
