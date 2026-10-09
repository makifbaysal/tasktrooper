import { describe, expect, it, vi } from "vitest";

vi.mock("electron", () => ({
  app: { getAppPath: () => "/app", isPackaged: false },
  net: { fetch: () => Promise.resolve(new Response("")) },
  protocol: { registerSchemesAsPrivileged: () => {}, handle: () => {} },
}));

const { isOwnPage, isTrustedFrame } = await import("./sender-guard.js");

const ACCOUNT = "https://app.tasktrooper.ai";
const APP = "app://tasktrooper";

function view(url: string) {
  const contents = { isDestroyed: () => false, mainFrame: { processId: 7, routingId: 1 } };
  const event = { sender: contents, senderFrame: { processId: 7, routingId: 1, url } };
  return { contents, event };
}

/**
 * The bridge can start processes, so which page may call it is the boundary
 * account mode moves: the account's origin in account mode, the bundled app's
 * in local mode, and never both at once.
 */
describe("isTrustedFrame", () => {
  it("accepts the account origin's top frame while the account's origin is the trusted one", () => {
    const { contents, event } = view(`${ACCOUNT}/board`);
    expect(isTrustedFrame(event, contents, ACCOUNT)).toBe(true);
  });

  it("refuses the account origin in local mode, and the bundled app in account mode", () => {
    const remote = view(`${ACCOUNT}/board`);
    expect(isTrustedFrame(remote.event, remote.contents, APP)).toBe(false);
    const local = view(`${APP}/home`);
    expect(isTrustedFrame(local.event, local.contents, ACCOUNT)).toBe(false);
  });

  it("refuses another origin that merely shares a host name or a scheme", () => {
    for (const url of ["http://app.tasktrooper.ai/board", "https://app.tasktrooper.ai.evil.com/", "https://evil.com/"]) {
      const { contents, event } = view(url);
      expect(isTrustedFrame(event, contents, ACCOUNT), url).toBe(false);
    }
  });

  it("refuses an iframe of the trusted origin and a sender that is not the view", () => {
    const { contents, event } = view(`${ACCOUNT}/board`);
    expect(isTrustedFrame({ ...event, senderFrame: { ...event.senderFrame, routingId: 2 } }, contents, ACCOUNT)).toBe(false);
    expect(isTrustedFrame({ ...event, sender: {} }, contents, ACCOUNT)).toBe(false);
    expect(isTrustedFrame(event, null, ACCOUNT)).toBe(false);
  });
});

/**
 * The API-key window is the only sender that may hand this process a key, so
 * its guard is identity — this exact window, its top frame, its own page —
 * and an origin never satisfies it: the web app's view is refused whichever
 * origin it holds, the account's above all.
 */
describe("isOwnPage", () => {
  const KEYS = "file:///Applications/TaskTrooper.app/Contents/Resources/app.asar/dist/renderer/keys.html";

  function keysWindow(url = KEYS) {
    const contents = { isDestroyed: () => false, mainFrame: { processId: 9, routingId: 1 } };
    const event = { sender: contents, senderFrame: { processId: 9, routingId: 1, url } };
    return { contents, event };
  }

  it("accepts the key window's own top frame on its own page", () => {
    const { contents, event } = keysWindow();
    expect(isOwnPage(event, contents, KEYS)).toBe(true);
    expect(isOwnPage({ ...event, senderFrame: { ...event.senderFrame, url: `${KEYS}#add` } }, contents, KEYS)).toBe(true);
  });

  it("refuses the account origin's page, and the local web app's, even though each is a trusted frame of its own", () => {
    const keys = keysWindow();
    for (const url of [`${ACCOUNT}/settings`, `${APP}/settings`]) {
      const page = view(url);
      expect(isTrustedFrame(page.event, page.contents, originOfUrl(url)), url).toBe(true);
      expect(isOwnPage(page.event, keys.contents, KEYS), url).toBe(false);
      expect(isOwnPage(page.event, page.contents, KEYS), url).toBe(false);
    }
  });

  it("refuses an iframe, another sender, a closed window, and a window that left its page", () => {
    const { contents, event } = keysWindow();
    expect(isOwnPage({ ...event, senderFrame: { ...event.senderFrame, routingId: 2 } }, contents, KEYS)).toBe(false);
    expect(isOwnPage({ ...event, sender: {} }, contents, KEYS)).toBe(false);
    expect(isOwnPage(event, null, KEYS)).toBe(false);
    expect(isOwnPage(event, { ...contents, isDestroyed: () => true }, KEYS)).toBe(false);
    const moved = keysWindow("https://evil.example/keys.html");
    expect(isOwnPage(moved.event, moved.contents, KEYS)).toBe(false);
    expect(isOwnPage(event, contents, "")).toBe(false);
  });
});

function originOfUrl(url: string): string {
  return url.startsWith(APP) ? APP : ACCOUNT;
}
