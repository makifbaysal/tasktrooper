import type { SupervisorSnapshot } from "@ipc/types.js";

/**
 * The "could not start" screen's body copy. Only macOS runs this app from a
 * menu bar icon; Windows and Linux run it from the system tray, and "this
 * machine" reads true everywhere "this Mac" only reads true on one of the
 * three.
 */
export function unreachableCopy(isMac: boolean): string {
  const tray = isMac ? "menu bar" : "system tray";
  return `Everything runs on this machine, so there is nothing to show until the local server is up. Start it from the TaskTrooper icon in the ${tray}, or retry below.`;
}

export function supervisorLabel(snapshot: SupervisorSnapshot | null, isMac: boolean): string {
  switch (snapshot?.state) {
    case "running":
      return isMac ? "running on this Mac" : "running";
    case "degraded":
      return "degraded";
    case "preflight":
    case "starting":
      return "starting";
    case "stopping":
      return "stopping";
    case "failed":
      return "not running";
    default:
      return "stopped";
  }
}
