import { originOf } from "./services/app-scheme.js";

/** The parts of a WebContents and an IPC event the guard reads. */
export interface GuardedContents {
  isDestroyed(): boolean;
  mainFrame: { processId: number; routingId: number };
}

export interface GuardedEvent {
  sender: unknown;
  senderFrame: { processId: number; routingId: number; url: string } | null | undefined;
}

/**
 * Is this call from the web app's view, its top frame, on the one origin the
 * current mode trusts? `trustedOrigin` is `app://tasktrooper` in local mode
 * and the account's origin in account mode — one at a time, so the bundled
 * app's origin holds no powers while an account is signed in and the
 * account's origin holds none in local mode.
 */
export function isTrustedFrame(
  event: GuardedEvent,
  contents: GuardedContents | null,
  trustedOrigin: string,
): boolean {
  if (!contents || contents.isDestroyed() || event.sender !== contents) return false;
  const frame = event.senderFrame;
  const main = contents.mainFrame;
  if (!frame || frame.processId !== main.processId || frame.routingId !== main.routingId) return false;
  return originOf(frame.url) === trustedOrigin;
}
