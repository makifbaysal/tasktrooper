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
