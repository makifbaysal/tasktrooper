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

/** The exact contents, and its top frame — never an iframe or another view. */
function topFrameOf(event: GuardedEvent, contents: GuardedContents | null): GuardedEvent["senderFrame"] {
  if (!contents || contents.isDestroyed() || event.sender !== contents) return null;
  const frame = event.senderFrame;
  const main = contents.mainFrame;
  if (!frame || frame.processId !== main.processId || frame.routingId !== main.routingId) return null;
  return frame;
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
  const frame = topFrameOf(event, contents);
  return !!frame && originOf(frame.url) === trustedOrigin;
}

function withoutFragment(raw: string): string | null {
  try {
    const url = new URL(raw);
    url.hash = "";
    return url.href;
  } catch {
    return null;
  }
}

/**
 * Is this call from one window of this app's own, its top frame, still on the
 * page it was opened on? The API-key window's guard: an origin is not enough
 * there — in local mode the web app's view is `app://tasktrooper` too, and in
 * account mode it is a remote origin — so the question is "this exact
 * WebContents, this exact page", which no other view can answer yes to.
 */
export function isOwnPage(event: GuardedEvent, contents: GuardedContents | null, pageUrl: string): boolean {
  const frame = topFrameOf(event, contents);
  if (!frame) return false;
  const expected = withoutFragment(pageUrl);
  return expected !== null && withoutFragment(frame.url) === expected;
}
