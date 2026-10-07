import { EventEmitter } from "node:events";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { CloudStatus } from "../ipc/types.js";

/**
 * The shell's one job that a user notices immediately: keeping the hosted app
 * on screen.
 *
 * These tests exist because it stopped doing that. The shell treated
 * `did-start-loading` as "a page is loading", hid the `WebContentsView` until a
 * matching `did-finish-load`, and `web/` is a `BrowserRouter` SPA — so every
 * client-side route change raised `did-start-loading`, was answered by
 * `did-stop-loading`, and left the product hidden behind the shell's own
 * spinner permanently. It hung on first paint, hung again the instant sign-in
 * routed to /board, and a manual reload cleared it every time.
 *
 * So the fake below does NOT emit a convenient abstraction of a navigation. It
 * replays the event sequences Chromium actually produced, in the order and with
 * the arguments captured from the running app — `did-start-loading` included.
 * That is what makes these a regression guard rather than a restatement of the
 * fix: run them against the old shell and the SPA cases fail on the real
 * mechanism.
 */

const ORIGIN = "app://tasktrooper";

class FakeWebContents extends EventEmitter {
  url = "";
  loaded: string[] = [];
  reloads = 0;
  destroyed = false;
  mainFrame = { processId: 1, routingId: 1, url: "" };

  getURL(): string {
    return this.url;
  }
  async loadURL(url: string): Promise<void> {
    this.loaded.push(url);
    this.url = url;
    return Promise.resolve();
  }
  reload(): void {
    this.reloads += 1;
  }
  close(): void {
    this.destroyed = true;
  }
  isDestroyed(): boolean {
    return this.destroyed;
  }
  setWindowOpenHandler(): void {}

  // --- the two sequences, exactly as Chromium emitted them ------------------

  /**
   * A real document load. Captured from a cold start against
   * app://tasktrooper — did-start-loading, a cross-document
   * did-start-navigation, did-navigate with the status code, then
   * did-finish-load and did-stop-loading.
   */
  emitDocumentLoad(url: string, httpCode = 200): void {
    this.emit("did-start-loading");
    this.emit("did-start-navigation", { url, isSameDocument: false, isMainFrame: true });
    this.url = url;
    this.mainFrame.url = url;
    this.emit("did-navigate", {}, url, httpCode, httpCode === 200 ? "" : "Bad Gateway");
    this.emit("dom-ready");
    this.emit("did-finish-load");
    this.emit("did-stop-loading");
  }

  /**
   * A react-router route change. Captured verbatim: the tab's loading state
   * turns on and off around a same-document navigation, and `did-finish-load`
   * is never emitted again because no document was ever loaded.
   */
  emitSpaRouteChange(url: string): void {
    this.emit("did-start-loading");
    this.emit("did-start-navigation", { url, isSameDocument: true, isMainFrame: true });
    this.url = url;
    this.mainFrame.url = url;
    this.emit("did-navigate-in-page", {}, url, true);
    this.emit("did-stop-loading");
  }

  /** A subframe loading a document of its own — an embedded widget, say. */
  emitSubframeLoad(url: string): void {
    this.emit("did-start-loading");
    this.emit("did-start-navigation", { url, isSameDocument: false, isMainFrame: false });
    this.emit("did-stop-loading");
  }
}

class FakeView {
  webContents = new FakeWebContents();
  visible = true;
  constructor(public options: unknown = {}) {}
  bounds: unknown = null;
  background = "";
  setVisible(visible: boolean): void {
    this.visible = visible;
  }
  setBounds(bounds: unknown): void {
    this.bounds = bounds;
  }
  setBackgroundColor(color: string): void {
    this.background = color;
  }
}

const views: FakeView[] = [];

class FakeBrowserWindow extends EventEmitter {
  webContents = new FakeWebContents();
  contentView = { addChildView: () => {}, removeChildView: () => {} };
  destroyed = false;
  loadedFiles: string[] = [];
  constructor(public options: unknown) {
    super();
  }
  isDestroyed(): boolean {
    return this.destroyed;
  }
  getContentSize(): [number, number] {
    return [1180, 800];
  }
  async loadFile(file: string): Promise<void> {
    this.loadedFiles.push(file);
    return Promise.resolve();
  }
  async loadURL(): Promise<void> {
    return Promise.resolve();
  }
  shown = false;
  // Electron raises both events from the calls themselves, and the
  // visibility tests below lean on that.
  show(): void {
    this.shown = true;
    this.emit("show");
  }
  hide(): void {
    this.hidden = true;
    this.emit("hide");
  }
  hidden = false;
  focus(): void {}
  restore(): void {}
  isMinimized(): boolean {
    return false;
  }
  fullScreen = false;
  isFullScreen(): boolean {
    return this.fullScreen;
  }
}

const openedExternally: string[] = [];

vi.mock("electron", () => ({
  app: { getAppPath: () => "/app" },
  shell: {
    openExternal: (url: string) => {
      openedExternally.push(url);
      return Promise.resolve();
    },
  },
  BrowserWindow: class extends FakeBrowserWindow {},
  WebContentsView: class {
    constructor(options: unknown) {
      const view = new FakeView(options);
      views.push(view);
      return view as unknown as this;
    }
  },
}));

const { HOME_ROUTE, Shell, openExternally } = await import("./window.js");

interface Harness {
  shell: InstanceType<typeof Shell>;
  window: FakeBrowserWindow;
  view: FakeView;
  contents: FakeWebContents;
  statuses: CloudStatus[];
  last: () => CloudStatus;
}

function start(
  origin = ORIGIN,
  quitStarted: () => boolean = () => false,
  extra: { hidden?: boolean; onSessionEnd?: () => void } = {},
): Harness {
  const statuses: CloudStatus[] = [];
  const shell = new Shell({
    origin: () => origin,
    onCloudStatus: (s) => statuses.push(s),
    quitStarted,
    ...(extra.onSessionEnd ? { onSessionEnd: extra.onSessionEnd } : {}),
  });
  shell.create({ hidden: extra.hidden ?? false });
  // The web app's view is attached only once the backend answers, which is what
  // the main process signals with serve(). Every case below is about a window
  // that has one.
  shell.serve();
  const view = views.at(-1)!;
  return {
    shell,
    window: shell.window as unknown as FakeBrowserWindow,
    view,
    contents: view.webContents,
    statuses,
    last: () => statuses.at(-1)!,
  };
}

beforeEach(() => {
  views.length = 0;
  openedExternally.length = 0;
});

afterEach(() => {
  vi.restoreAllMocks();
});

const realPlatform = Object.getOwnPropertyDescriptor(process, "platform");
function onPlatform(platform: NodeJS.Platform): void {
  Object.defineProperty(process, "platform", { value: platform, configurable: true });
}
function restorePlatform(): void {
  if (realPlatform) Object.defineProperty(process, "platform", realPlatform);
}

describe("where the web app sits in the window", () => {
  afterEach(restorePlatform);

  it("sits under the title bar on macOS, and flush to the top in full screen where the traffic lights are hidden", () => {
    onPlatform("darwin");
    const { window, view } = start();
    expect(view.bounds).toEqual({ x: 0, y: 44, width: 1180, height: 756 });

    window.fullScreen = true;
    window.emit("enter-full-screen");
    expect(view.bounds).toEqual({ x: 0, y: 0, width: 1180, height: 800 });

    window.fullScreen = false;
    window.emit("leave-full-screen");
    expect(view.bounds).toEqual({ x: 0, y: 44, width: 1180, height: 756 });
  });

  /**
   * Windows and Linux draw a native title bar above the content; the 44px
   * strip kept for macOS's traffic lights would be an empty band under it.
   */
  it("fills the whole content area on Windows and Linux, which have no traffic lights to make room for", () => {
    for (const platform of ["win32", "linux"] as const) {
      onPlatform(platform);
      const { window, view } = start();
      expect(view.bounds).toEqual({ x: 0, y: 0, width: 1180, height: 800 });
      expect(window.options).toMatchObject({ autoHideMenuBar: true });
    }
  });
});

describe("how the window is shown", () => {
  afterEach(restorePlatform);

  it("shows itself once ready, unless the login item started the app hidden", () => {
    const visible = start();
    visible.window.emit("ready-to-show");
    expect(visible.window.shown).toBe(true);

    const hidden = start(ORIGIN, () => false, { hidden: true });
    hidden.window.emit("ready-to-show");
    expect(hidden.window.shown).toBe(false);
  });

  it("starts the drain when Windows ends the session", () => {
    onPlatform("win32");
    const onSessionEnd = vi.fn();
    const { window } = start(ORIGIN, () => false, { onSessionEnd });
    window.emit("session-end", { reasons: ["shutdown"] });
    expect(onSessionEnd).toHaveBeenCalledTimes(1);
  });
});

describe("where the window opens", () => {
  it("opens the product's home route, not the marketing landing page", () => {
    const { contents } = start();
    expect(HOME_ROUTE).toBe("/home");
    expect(contents.loaded).toEqual([`${ORIGIN}/home`]);
  });

  /**
   * The web app's preload reads the API base synchronously, and there is none
   * until the backend has answered. A view attached before that is a page
   * permanently pointed at nothing, so `create()` alone must not make one.
   */
  it("does not load the web app until the backend has answered", () => {
    const shell = new Shell({ origin: () => ORIGIN, onCloudStatus: () => {}, quitStarted: () => false });
    shell.create();
    expect(views).toHaveLength(0);
    expect(shell.cloudContents).toBeNull();

    shell.serve();
    expect(views).toHaveLength(1);
    expect(views[0]!.webContents.loaded).toEqual([`${ORIGIN}/home`]);

    // Called again on every backend restart; it must not stack up views.
    shell.serve();
    expect(views).toHaveLength(1);
  });

  /** The chrome's offline screen is the only thing on screen if the backend never comes up. */
  it("reports an unavailable backend as a failed load, with the sentence that explains it", () => {
    const statuses: CloudStatus[] = [];
    const shell = new Shell({ origin: () => ORIGIN, onCloudStatus: (s) => statuses.push(s), quitStarted: () => false });
    shell.create();
    shell.markUnavailable("The local server did not start.");
    expect(statuses.at(-1)).toMatchObject({ state: "failed", description: "The local server did not start." });
  });
});

describe("keeping the hosted app on screen", () => {
  it("hides the view while the document loads and shows it when it lands", () => {
    const { view, contents, last } = start();

    contents.emit("did-start-loading");
    contents.emit("did-start-navigation", { url: `${ORIGIN}/board`, isSameDocument: false, isMainFrame: true });
    expect(last().state).toBe("loading");
    expect(view.visible).toBe(false);

    contents.emit("did-finish-load");
    expect(last().state).toBe("ready");
    expect(view.visible).toBe(true);
  });

  /**
   * THE regression. Every route change web/ makes is a pushState, and the
   * shell used to answer it by hiding the product forever.
   */
  it("stays visible across a client-side route change", () => {
    const { view, contents, last } = start();
    contents.emitDocumentLoad(`${ORIGIN}/board`);

    contents.emitSpaRouteChange(`${ORIGIN}/login`);

    expect(last().state).toBe("ready");
    expect(view.visible).toBe(true);
    expect(contents.reloads).toBe(0);
  });

  it("survives a run of route changes — sign-in is several in a row", () => {
    const { view, contents, statuses } = start();
    contents.emitDocumentLoad(`${ORIGIN}/board`);
    // /board -> /login (signed out), sign in, /login -> /teams -> /board.
    for (const route of ["/login", "/teams", "/board", "/settings/llm"]) {
      contents.emitSpaRouteChange(`${ORIGIN}${route}`);
      expect(view.visible).toBe(true);
    }
    expect(statuses.filter((s) => s.state === "loading")).toHaveLength(1);
  });

  it("reports the route the user is actually on", () => {
    const { contents, last } = start();
    contents.emitDocumentLoad(`${ORIGIN}/board`);
    contents.emitSpaRouteChange(`${ORIGIN}/settings/llm`);
    expect(last().url).toBe(`${ORIGIN}/settings/llm`);
  });

  it("ignores a subframe loading a document of its own", () => {
    const { view, contents, last } = start();
    contents.emitDocumentLoad(`${ORIGIN}/board`);

    // A page can create one of these lazily, long after it has loaded.
    contents.emitSubframeLoad("https://tasktrooper.firebaseapp.com/__/auth/iframe");

    expect(last().state).toBe("ready");
    expect(view.visible).toBe(true);
  });

  it("hides the view again for a real navigation, and shows it when that lands", () => {
    const { view, contents, statuses } = start();
    contents.emitDocumentLoad(`${ORIGIN}/board`);

    // The tray's "Claude Code settings…" does a genuine loadURL.
    contents.emit("did-start-loading");
    contents.emit("did-start-navigation", { url: `${ORIGIN}/settings/llm`, isSameDocument: false, isMainFrame: true });
    expect(view.visible).toBe(false);
    expect(statuses.at(-1)!.state).toBe("loading");

    contents.emit("did-finish-load");
    expect(view.visible).toBe(true);
  });
});

describe("failures still reach the offline screen", () => {
  it("reports a main-frame load failure", () => {
    const { view, contents, last } = start();
    contents.emit("did-start-loading");
    contents.emit("did-start-navigation", { url: `${ORIGIN}/board`, isSameDocument: false, isMainFrame: true });
    contents.emit("did-fail-load", {}, -105, "ERR_NAME_NOT_RESOLVED", `${ORIGIN}/board`, true);

    expect(last()).toMatchObject({ state: "failed", code: -105, description: "ERR_NAME_NOT_RESOLVED" });
    expect(view.visible).toBe(false);
  });

  it("ignores a subresource that failed", () => {
    const { view, contents, last } = start();
    contents.emitDocumentLoad(`${ORIGIN}/board`);
    contents.emit("did-fail-load", {}, -105, "ERR_NAME_NOT_RESOLVED", `${ORIGIN}/logo.png`, false);
    expect(last().state).toBe("ready");
    expect(view.visible).toBe(true);
  });

  it("treats a gateway error page as a failure even though it loads fine", () => {
    const { view, contents, last } = start();
    contents.emitDocumentLoad(`${ORIGIN}/board`, 502);
    expect(last()).toMatchObject({ state: "failed", code: 502 });
    expect(view.visible).toBe(false);
  });

  it("reloads where the user is rather than throwing away their route", () => {
    const { shell, contents } = start();
    contents.emitDocumentLoad(`${ORIGIN}/board`);
    contents.emitSpaRouteChange(`${ORIGIN}/settings/llm`);

    shell.reloadCloud();

    expect(contents.reloads).toBe(1);
    expect(contents.loaded).toEqual([`${ORIGIN}/home`]);
  });
});

describe("the navigation boundary is unchanged", () => {
  it("sends an off-origin navigation to the real browser", () => {
    const { contents } = start();
    let prevented = false;
    contents.emit("will-navigate", { preventDefault: () => (prevented = true) }, "https://evil.example.com/");
    expect(prevented).toBe(true);
    expect(openedExternally).toEqual(["https://evil.example.com/"]);
  });

  it("bounces a same-origin page that redirected itself off-origin", () => {
    const { contents } = start();
    contents.loaded.length = 0;
    contents.emit("did-navigate", {}, "https://evil.example.com/", 200, "");
    expect(contents.loaded).toEqual([`${ORIGIN}/home`]);
  });

  it("lets a same-origin navigation through", () => {
    const { contents } = start();
    let prevented = false;
    contents.emit("will-navigate", { preventDefault: () => (prevented = true) }, `${ORIGIN}/settings/llm`);
    expect(prevented).toBe(false);
    expect(openedExternally).toEqual([]);
  });
});

describe("closing the window", () => {
  it("hides the window instead of destroying it while quit has not started", () => {
    const { window } = start(ORIGIN, () => false);
    let prevented = false;
    window.emit("close", { preventDefault: () => (prevented = true) });
    expect(prevented).toBe(true);
    expect(window.hidden).toBe(true);
  });

  it("lets the close through once quit.run() has been entered", () => {
    const { window } = start(ORIGIN, () => true);
    let prevented = false;
    window.emit("close", { preventDefault: () => (prevented = true) });
    expect(prevented).toBe(false);
    expect(window.hidden).toBe(false);
  });
});

/**
 * Closing hides the window rather than destroying it, so the page behind it
 * keeps running. It must at least see `visibilityState` "hidden", which is
 * what pauses its polls and animations.
 */
describe("a window nobody can see lets the page sleep", () => {
  it("hides the view when the red button hides the window, and shows it again with the window", () => {
    const { shell, window, view, contents } = start();
    contents.emitDocumentLoad(`${ORIGIN}/board`);
    expect(view.visible).toBe(true);
    expect(shell.pageOnScreen).toBe(true);

    window.emit("close", { preventDefault: () => {} });
    expect(view.visible).toBe(false);
    expect(shell.pageOnScreen).toBe(false);

    shell.show();
    expect(view.visible).toBe(true);
    expect(shell.pageOnScreen).toBe(true);
  });

  it("follows minimize and restore", () => {
    const { window, view, contents } = start();
    contents.emitDocumentLoad(`${ORIGIN}/board`);

    window.emit("minimize");
    expect(view.visible).toBe(false);
    window.emit("restore");
    expect(view.visible).toBe(true);
  });

  /** macOS's minimize animation, as Electron 41 reported it. */
  it("ends hidden after macOS's minimize sequence, which flaps through show", () => {
    const { window, view, contents } = start();
    contents.emitDocumentLoad(`${ORIGIN}/board`);
    for (const event of ["minimize", "hide", "show", "hide"]) window.emit(event);
    expect(view.visible).toBe(false);
  });

  it("does not show a view the window hid when its page finishes loading", () => {
    const { window, view, contents } = start();
    window.hide();
    contents.emitDocumentLoad(`${ORIGIN}/board`);
    expect(view.visible).toBe(false);

    window.show();
    expect(view.visible).toBe(true);
  });

  it("keeps a failed page's view hidden when the window comes back", () => {
    const { window, view, contents } = start();
    contents.emitDocumentLoad(`${ORIGIN}/board`, 502);
    window.hide();
    window.show();
    expect(view.visible).toBe(false);
  });

  it("counts focus as on screen, whatever event was missed", () => {
    const { window, view, contents } = start();
    contents.emitDocumentLoad(`${ORIGIN}/board`);
    window.emit("hide");
    window.emit("focus");
    expect(view.visible).toBe(true);
  });

  it("starts off screen when the login item launched it hidden", () => {
    const { shell, view, contents } = start(ORIGIN, () => false, { hidden: true });
    contents.emitDocumentLoad(`${ORIGIN}/board`);
    expect(view.visible).toBe(false);

    shell.show();
    expect(view.visible).toBe(true);
  });

  it("pins background throttling on for both renderers", () => {
    const { window, view } = start();
    expect(window.options).toMatchObject({ webPreferences: { backgroundThrottling: true } });
    expect(view.options).toMatchObject({ webPreferences: { backgroundThrottling: true } });
  });
});

/**
 * `openExternally`'s return value is what lets the PR-link bridge channel
 * (`cloud:open-external`, `main/ipc.ts`) tell the page a link could not be
 * opened instead of doing nothing and leaving the user wondering whether the
 * click landed.
 */
describe("openExternally", () => {
  it("opens an https URL and reports success", () => {
    expect(openExternally("https://github.com/org/repo/pull/1")).toBe(true);
    expect(openedExternally).toEqual(["https://github.com/org/repo/pull/1"]);
  });

  it("refuses a non-https URL without opening anything", () => {
    expect(openExternally("http://example.com")).toBe(false);
    expect(openExternally("http://localhost.example.com")).toBe(false);
    expect(openExternally("javascript:alert(1)")).toBe(false);
    expect(openExternally("file:///etc/passwd")).toBe(false);
    expect(openedExternally).toEqual([]);
  });

  it("opens a plain-http loopback URL — a task's local preview has no TLS", () => {
    expect(openExternally("http://localhost:3000/")).toBe(true);
    expect(openExternally("http://127.0.0.1:5173")).toBe(true);
    expect(openExternally("http://[::1]:8080")).toBe(true);
    expect(openedExternally).toEqual(["http://localhost:3000/", "http://127.0.0.1:5173", "http://[::1]:8080"]);
  });

  it("refuses a value that is not a URL at all", () => {
    expect(openExternally("not a url")).toBe(false);
    expect(openExternally("")).toBe(false);
    expect(openedExternally).toEqual([]);
  });
});
