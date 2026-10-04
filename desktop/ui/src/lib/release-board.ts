import type { Release, ReleaseStatus } from "@/api";

/**
 * Release statuses a merged card in done is still waiting on. Without them on
 * the card, a task that merged and deployed looked stuck in done for the whole
 * verify window, and people moved it to released by hand — which left the
 * release itself without a verdict.
 */
export const BOARD_RELEASE_STATUSES: ReleaseStatus[] = [
  "pending",
  "deploying",
  "verifying",
  "awaiting_verdict",
  "rolling_back",
  "failed",
];

/** The newest open release of each task; `releases` arrives newest first. */
export function openReleaseByTask(releases: Release[]): Map<string, Release> {
  const byTask = new Map<string, Release>();
  for (const release of releases) {
    for (const task of release.tasks ?? []) {
      if (!byTask.has(task.id)) byTask.set(task.id, release);
    }
  }
  return byTask;
}

/** Whole minutes until the verify window closes, never negative; undefined outside verifying. */
export function verifyMinutesLeft(release: Release, now: number = Date.now()): number | undefined {
  if (release.status !== "verifying" || !release.verify_until) return undefined;
  const left = new Date(release.verify_until).getTime() - now;
  return Math.max(0, Math.ceil(left / 60000));
}
