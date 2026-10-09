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

/**
 * Account mode's version: the page comes from the account's origin, so what
 * failed is the network path to it, and running locally is the other way on.
 */
export function accountUnreachableCopy(origin: string): string {
  return `This computer is signed in to ${origin}, and the window shows TaskTrooper from there. Check the internet connection, VPN or proxy and retry — or sign out and use TaskTrooper on this computer without an account. Nothing stored locally is lost either way.`;
}

/**
 * A plain-language cause for the failures a user can fix themselves, matched
 * on the supervisor's description, which carries the backend's own last line.
 * `undefined` leaves the generic copy in place: guessing a cause for an
 * unrecognized line would send someone to fix the wrong thing.
 */
export function startFailureCopy(description: string | undefined): string | undefined {
  if (!description) return undefined;
  if (/unable to connect to https?:\/\/repo1\.maven\.org|error fetching postgres|download sha256 from/i.test(description)) {
    return "TaskTrooper could not download its database. The first start fetches PostgreSQL (about 30 MB) from Maven Central, and this machine could not reach it. Check the internet connection, VPN, proxy or firewall, then try again. Later starts do not need it.";
  }
  if (/no space left on device|free some disk space/i.test(description)) {
    return "The disk is full. TaskTrooper keeps its database on this machine and could not write to it. Free some space, then try again.";
  }
  if (/could not launch the postgres binary/i.test(description)) {
    return "The operating system would not run TaskTrooper's database (PostgreSQL). Security software quarantining it is the usual cause: allow TaskTrooper in it, then try again.";
  }
  return undefined;
}
