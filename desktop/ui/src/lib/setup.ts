// The guided first-run sequence, as a vocabulary and a set of pure rules.
//
// Everything that decides "where is this person" lives here rather than in the
// page, for one reason: the router gate (`ProtectedRoute`) and the page itself
// have to agree, and two copies of "is this finished" is exactly how a user
// ends up bounced between a screen that says it is done and a redirect that
// says it is not.
//
// The one rule that shapes the whole module: **a step's state is never
// stored.** It is read back from the thing the step actually did — is the
// preflight green, is the CLI connected, is GitHub connected, does a project
// with a repository exist. A "step 3 completed" flag written locally is wrong
// the instant someone disconnects GitHub, and nothing would ever notice.

/** The route the sequence lives on. Named so the gate and the links cannot drift. */
export const SETUP_PATH = "/setup";

/** Where the project step is finished: the gate lets it through, the step links to it. */
export const ADD_REPOSITORY_PATH = "/projects/new";

/**
 * The sequence, in order. Each one is gated on the previous being `done` —
 * genuinely done, not merely visited.
 *
 *  1. environment  — the preflight checklist; required items must be `ok`.
 *  2. agent        — connect an agent CLI, or an API provider with your own key.
 *  3. github       — connect the account the agents push with.
 *  4. project      — a project, and the first repository imported into it.
 */
export const SETUP_STEP_IDS = ["environment", "agent", "github", "project"] as const;
export type SetupStepId = (typeof SETUP_STEP_IDS)[number];

/**
 * Three states, and the third one is the point.
 *
 * `unknown` is NOT `todo`. A call that failed tells us nothing about whether
 * the thing is done, and rendering it as an empty checkbox is a lie that sends
 * someone off to redo work they already did. Every screen that shows a step
 * shows `unknown` as "we could not check", with the failure's own sentence.
 */
export type SetupStepState = "done" | "todo" | "unknown";

export interface SetupStep {
  id: SetupStepId;
  state: SetupStepState;
  /**
   * Can this step be PERFORMED on this surface?
   *
   * False in a browser for `environment`: it acts on the user's own Mac
   * through the desktop shell's bridge, and no browser tab has one. The step is still shown — with the download, not with a button that
   * cannot work — and its state may still be readable (a CLI connected from
   * some Mac is visible to every browser hitting this server).
   */
  actionable: boolean;
  /** Present only for `unknown`: the sentence from the call that could not answer. */
  error?: string;
}

export type SetupSteps = Record<SetupStepId, SetupStep>;

/**
 * The step the user is on: the first one that is not `done`.
 *
 * `unknown` counts as not done — it is not something to skip past, it is
 * something to look at — but it is deliberately not the same as `todo`
 * elsewhere in this file. Null when every step is done.
 */
export function activeSetupStep(steps: SetupSteps): SetupStepId | null {
  return SETUP_STEP_IDS.find((id) => steps[id].state !== "done") ?? null;
}

/** Every step done. The only condition under which the sequence stops asking. */
export function setupComplete(steps: SetupSteps): boolean {
  return SETUP_STEP_IDS.every((id) => steps[id].state === "done");
}

/**
 * Is something DEFINITELY undone?
 *
 * The gate below turns on this and not on `!setupComplete`, because a step we
 * could not check is not a reason to drag someone out of what they were doing.
 * A redirect on `unknown` would fire on every failed poll.
 */
export function setupNeedsWork(steps: SetupSteps): boolean {
  return SETUP_STEP_IDS.some((id) => steps[id].state === "todo");
}

/**
 * May the user open this step?
 *
 * Unlocked while no EARLIER step is `todo`. Not a security boundary — every
 * step's own action is refused by the thing behind it if its prerequisite is
 * missing (the shell refuses Connect on a red preflight; the GitHub and
 * project calls need a Mac attached) — but a wizard that lets you click step 4
 * first is a wizard that has no order.
 *
 * `todo` and not "anything other than done", because the third state is the
 * one that would otherwise trap people: an environment probe that failed to
 * run makes step 1 `unknown` for ever, and locking step 2 behind a reading
 * nobody could take is the same mistake as rendering that reading as "not
 * done". A step that is genuinely, knowably undone still stops the sequence.
 */
export function setupStepUnlocked(steps: SetupSteps, id: SetupStepId): boolean {
  const index = SETUP_STEP_IDS.indexOf(id);
  return !SETUP_STEP_IDS.slice(0, index).some((prev) => steps[prev].state === "todo");
}

/**
 * The sessionStorage key behind "I'll do this later".
 *
 * A dismissal, and pointedly not progress: it only stops the automatic
 * redirect, never marks a step done, and the sidebar link and the page keep
 * deriving the truth either way. Session-scoped like the rest of `uiCache`, so
 * reopening the app offers the unfinished sequence again — which is the right
 * answer for a setup nobody finished.
 */
export const SETUP_DISMISSED_KEY = "setup.dismissed";
