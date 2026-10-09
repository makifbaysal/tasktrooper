// The two choices the first run asks before the setup sequence: local or
// account, and which team template to start from.
//
// localStorage, not the session-scoped uiCache: both are "asked once" answers
// that must survive a relaunch, or every launch would re-ask them.

export const FIRST_RUN_MODE_KEY = "tt.firstRun.mode";
export const FIRST_RUN_TEAM_DONE_KEY = "tt.firstRun.teamDone";

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
  } catch {
    /* private-mode storage or a quota error — the sequence simply asks again */
  }
}
