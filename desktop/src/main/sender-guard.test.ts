import { describe, expect, it, vi } from "vitest";

vi.mock("electron", () => ({
  app: { getAppPath: () => "/app", isPackaged: false },
  net: { fetch: () => Promise.resolve(new Response("")) },
  protocol: { registerSchemesAsPrivileged: () => {}, handle: () => {} },
}));

const { isTrustedFrame } = await import("./sender-guard.js");

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
