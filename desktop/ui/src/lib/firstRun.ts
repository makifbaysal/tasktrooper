// The choices the first run asks before the setup sequence: local or account,
// which team template to start from, which developer tools to install, and
// which integrations to set up.
//
// localStorage, not the session-scoped uiCache: all are "asked once" answers
// that must survive a relaunch, or every launch would re-ask them.

export const FIRST_RUN_MODE_KEY = "tt.firstRun.mode";
export const FIRST_RUN_TEAM_DONE_KEY = "tt.firstRun.teamDone";
export const FIRST_RUN_TOOLCHAIN_DONE_KEY = "tt.firstRun.toolchainDone";
export const FIRST_RUN_INTEGRATIONS_DONE_KEY = "tt.firstRun.integrationsDone";
/**
 * Written with every first-run answer by the flow that has the toolchain and
 * integrations screens — the team one too, because a page that never asks the
 * mode (the account's web app answers it for itself) writes only that.
 */
export const FIRST_RUN_VERSION_KEY = "tt.firstRun.version";
export const FIRST_RUN_VERSION = "2";

export type FirstRunMode = "local" | "account";

export function readFirstRunMode(): FirstRunMode | null {
  try {
    const raw = window.localStorage.getItem(FIRST_RUN_MODE_KEY);
    return raw === "local" || raw === "account" ? raw : null;
  } catch {
    return null;
  }
}

export function writeFirstRunMode(mode: FirstRunMode): void {
  try {
    window.localStorage.setItem(FIRST_RUN_MODE_KEY, mode);
    window.localStorage.setItem(FIRST_RUN_VERSION_KEY, FIRST_RUN_VERSION);
  } catch {
    /* private-mode storage or a quota error — the sequence simply asks again */
  }
}

export function clearFirstRunMode(): void {
  try {
    window.localStorage.removeItem(FIRST_RUN_MODE_KEY);
  } catch {
    /* private-mode storage or a quota error — the sequence simply asks again */
  }
}

export function readFirstRunTeamDone(): boolean {
  try {
    return window.localStorage.getItem(FIRST_RUN_TEAM_DONE_KEY) === "1";
  } catch {
    return false;
  }
}

export function writeFirstRunTeamDone(): void {
  try {
    window.localStorage.setItem(FIRST_RUN_TEAM_DONE_KEY, "1");
    window.localStorage.setItem(FIRST_RUN_VERSION_KEY, FIRST_RUN_VERSION);
  } catch {
    /* private-mode storage or a quota error — the sequence simply asks again */
  }
}

/**
 * An install that answered the first run before the toolchain and integrations
 * screens existed has a mode or a team but no version marker. Its setup regressing later
 * (GitHub disconnected, a project removed) reopens /setup, and that must not
 * walk it through screens it was never part of: they count as answered.
 */
export function migrateLegacyFirstRun(): void {
  try {
    const store = window.localStorage;
    const answered = readFirstRunMode() !== null || readFirstRunTeamDone();
    if (!answered || store.getItem(FIRST_RUN_VERSION_KEY) !== null) return;
    store.setItem(FIRST_RUN_TOOLCHAIN_DONE_KEY, "1");
    store.setItem(FIRST_RUN_INTEGRATIONS_DONE_KEY, "1");
    store.setItem(FIRST_RUN_VERSION_KEY, FIRST_RUN_VERSION);
  } catch {
    /* private-mode storage or a quota error — nothing to carry over */
  }
}

export function readFirstRunToolchainDone(): boolean {
  migrateLegacyFirstRun();
  try {
    return window.localStorage.getItem(FIRST_RUN_TOOLCHAIN_DONE_KEY) === "1";
  } catch {
    return false;
  }
}

export function writeFirstRunToolchainDone(): void {
  try {
    window.localStorage.setItem(FIRST_RUN_TOOLCHAIN_DONE_KEY, "1");
  } catch {
    /* private-mode storage or a quota error — the sequence simply asks again */
  }
}

export function readFirstRunIntegrationsDone(): boolean {
  migrateLegacyFirstRun();
  try {
    return window.localStorage.getItem(FIRST_RUN_INTEGRATIONS_DONE_KEY) === "1";
  } catch {
    return false;
  }
}

export function writeFirstRunIntegrationsDone(): void {
  try {
    window.localStorage.setItem(FIRST_RUN_INTEGRATIONS_DONE_KEY, "1");
  } catch {
    /* private-mode storage or a quota error — the sequence simply asks again */
  }
}
