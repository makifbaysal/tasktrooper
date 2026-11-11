// The last version whose notes this install has been shown (or skipped past).
//
// localStorage of the app's own origin: app://tasktrooper for the local
// desktop app, which is the same origin across updates, so it survives them.

export const WHATS_NEW_SEEN_KEY = "tt.whatsNew.seenVersion";

export function readSeenVersion(): string | null {
  try {
    return window.localStorage.getItem(WHATS_NEW_SEEN_KEY);
  } catch {
    return null;
  }
}

export function writeSeenVersion(version: string): void {
  try {
    window.localStorage.setItem(WHATS_NEW_SEEN_KEY, version);
  } catch {
    /* private-mode storage or a quota error — the notes may show again */
  }
}
