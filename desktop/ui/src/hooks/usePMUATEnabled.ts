import { useEffect, useSyncExternalStore } from "react";
import { api } from "@/api";

export const PM_UAT_COLUMN = "pm_uat";

let enabled = true;
let loaded = false;
let inflight: Promise<void> | null = null;
const listeners = new Set<() => void>();

function publish(next: boolean) {
  loaded = true;
  if (next === enabled) return;
  enabled = next;
  listeners.forEach((l) => l());
}

function load() {
  if (loaded || inflight) return;
  inflight = api
    .getSettings()
    .then((s) => publish(s.pm_uat_enabled !== false))
    .catch(() => {
      // Unreadable settings must not hide a stage: stay on the default (on).
    })
    .finally(() => {
      inflight = null;
    });
}

/** Called by the Settings toggle so every mounted board reacts at once. */
export function setPMUATEnabled(next: boolean) {
  publish(next);
}

/** Global "PM UAT step" setting; true until the server says otherwise. */
export function usePMUATEnabled(): boolean {
  useEffect(load, []);
  return useSyncExternalStore(
    (cb) => {
      listeners.add(cb);
      return () => listeners.delete(cb);
    },
    () => enabled,
  );
}
