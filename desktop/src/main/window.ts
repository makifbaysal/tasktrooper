import path from "node:path";
import { BrowserWindow, WebContentsView, app, shell } from "electron";
import type { CloudStatus } from "../ipc/types.js";
import { originOf } from "./services/app-scheme.js";

/**
 * The window: a thin native chrome, and the bundled web app filling everything
 * below it.
 *
 * There are no tabs, and that is the point. The desktop app is the web app in a
 * window plus a supervisor; a second navigation model in the shell would be a
 * second information architecture for the same product.
 *
 * What the shell owns:
 *
 *  - the title bar, so macOS's traffic lights have somewhere to sit and the
 *    backend's state is glanceable without opening a page;
 *  - the honest failure screen — including the one that is up while the backend
 *    is still starting, which is why the web app's view is not attached until
 *    `serve()` is called;
 *  - navigation policy, which is the boundary that keeps the bridge's powers
 *    attached to one origin.
 */

/** Height of the shell's title bar, in CSS pixels. The view is inset below it. */
export const CHROME_HEIGHT = 44;

/**
 * The route the window opens on.
 *
 * Deliberately not the origin root. `/` is the marketing landing page, which is
 * the right first screen for somebody deciding whether to install this app and
 * the wrong one for somebody who already has.
 *
 * `/board` is where the SPA's own catch-all route sends everything else, so
 * this is the web app's idea of home rather than a second one invented here.
 *
 * The landing page is not unreachable, only un-opened: it is same-origin, so a
 * link to `/` inside the app navigates there normally.
 */
export const HOME_ROUTE = "/board";

export interface WindowDeps {
  /** Where the web app is served from: this app's own scheme. */
  origin: () => string;
  /** Told whenever the web app starts loading, loads, or fails to. */
  onCloudStatus: (status: CloudStatus) => void;
}

export class Shell {
  #window: BrowserWindow | null = null;
  #cloud: WebContentsView | null = null;
  #status: CloudStatus;
  /**
   * False until the backend has answered. The web app cannot be loaded before
   * that: its preload reads the API base synchronously and there is none, so a
   * view attached early would be a page permanently pointed at nothing.
   */
  #ready = false;
  readonly #deps: WindowDeps;

  constructor(deps: WindowDeps) {
    this.#deps = deps;
    this.#status = { state: "loading", url: this.startUrl };
  }

  /** Where the window opens: the trusted origin, at the product's home route. */
  get startUrl(): string {
    const origin = this.#deps.origin();
    try {
      return new URL(HOME_ROUTE, origin).toString();
    } catch {
      return origin;
    }
  }

  get window(): BrowserWindow | null {
    return this.#window;
  }

  get status(): CloudStatus {
    return this.#status;
  }

  /** The web app's webContents, for the main process to message it. */
  get cloudContents(): Electron.WebContents | null {
    return this.#cloud?.webContents ?? null;
  }

  create(): BrowserWindow {
    if (this.#window && !this.#window.isDestroyed()) return this.#window;

    const window = new BrowserWindow({
      width: 1180,
      height: 800,
      minWidth: 900,
      minHeight: 600,
      show: false,
      title: "TaskTrooper",
      ...(process.platform === "darwin" ? { titleBarStyle: "hiddenInset" as const } : {}),
      backgroundColor: "#0b0d13",
      webPreferences: {
        preload: path.join(app.getAppPath(), "dist", "preload", "index.cjs"),
        // The three non-negotiables. A renderer with any of these the other
        // way round can reach `require`, and a renderer that can reach
        // `require` makes every other control in this app decorative.
        contextIsolation: true,
        nodeIntegration: false,
        sandbox: true,
        webSecurity: true,
        // Our own UI has no reason to open a second renderer.
        webviewTag: false,
      },
    });

    window.once("ready-to-show", () => window.show());
    window.on("closed", () => {
      this.#window = null;
      this.#cloud = null;
    });
    window.on("resize", () => this.#layout());

    hardenShellNavigation(window);

    if (process.env.VITE_DEV_SERVER_URL) {
      void window.loadURL(process.env.VITE_DEV_SERVER_URL);
    } else {
      void window.loadFile(path.join(app.getAppPath(), "dist", "renderer", "index.html"));
    }

    this.#window = window;
    if (this.#ready) this.#attachCloud(window);
    return window;
  }

  /**
   * The backend is answering: put the web app on screen.
   *
   * Idempotent, and it has to be — it is called on every `server` event, which
   * includes every restart the backend makes on its own.
   */
  serve(): void {
    this.#ready = true;
    const window = this.#window;
    if (window && !window.isDestroyed() && !this.#cloud) this.#attachCloud(window);
  }

  /**
   * There is no web app to show, and why. The chrome renders its own offline
   * screen from this.
   */
  markUnavailable(description: string): void {
    this.#setStatus({ state: "failed", url: this.startUrl, description });
  }

  show(): void {
    const window = this.#window && !this.#window.isDestroyed() ? this.#window : this.create();
    if (window.isMinimized()) window.restore();
    window.show();
    window.focus();
  }

  /** Retry after a failed load, or refresh on demand. */
  reloadCloud(): void {
    const view = this.#cloud;
    if (!view) return;
    const start = this.startUrl;
    this.#setStatus({ state: "loading", url: view.webContents.getURL() || start });
    if (view.webContents.getURL() === "") void view.webContents.loadURL(start);
    else view.webContents.reload();
  }

  /**
   * Send the web app to one of its own routes — used by the tray, so
   * "Claude Code settings…" lands where the local controls are instead of
   * wherever the user left the app.
   */
  navigate(route: string): void {
    const view = this.#cloud;
    if (!view) return;
    const target = new URL(route, this.#deps.origin()).toString();
    void view.webContents.loadURL(target);
  }

  #attachCloud(window: BrowserWindow): void {
    const view = new WebContentsView({
      webPreferences: {
        preload: path.join(app.getAppPath(), "dist", "preload", "cloud.cjs"),
        // Same three settings as the shell window, and here they are load
        // bearing rather than hygiene: this view's preload is the one that can
        // start processes.
        contextIsolation: true,
        nodeIntegration: false,
        sandbox: true,
        webSecurity: true,
        webviewTag: false,
      },
    });
    view.setBackgroundColor("#0b0d13");

    // Development only: the page's failures, where they can be read.
    //
    // A renderer's console and its network errors do not reach this process,
    // so "Failed to fetch" in the window is a dead end from here — the one
    // thing that matters, which request died and why, is exactly what is
    // missing. Chromium's own error code (net::ERR_*) says it in one line.
    // `session?` because this is a convenience, not a feature: a WebContents
    // without one — the fake these tests drive — must lose the logging, not the
    // window.
    if (!app.isPackaged && view.webContents.session?.webRequest) {
      view.webContents.session.webRequest.onErrorOccurred((details) => {
        console.warn(`[net] ${details.method} ${details.url} → ${details.error}`);
      });
      view.webContents.on("console-message", (event) => {
        const { level, message, lineNumber, sourceId } = event as unknown as {
          level: string;
          message: string;
          lineNumber: number;
          sourceId: string;
        };
        if (level === "error" || level === "warning") {
          console.warn(`[page:${level}] ${message} (${sourceId}:${lineNumber})`);
        }
      });
    }

    this.#cloud = view;

    const origin = this.#deps.origin();
    const startUrl = this.startUrl;
    const contents = view.webContents;

    // `did-finish-load` fires for Chromium's own error page too — the error
    // page loads perfectly well — so a failure has to be remembered for the
    // rest of the navigation or it is immediately overwritten by "ready".
    let failed = false;

    hardenCloudNavigation(view, origin, startUrl, (url, code, statusText) => {
      failed = true;
      this.#setStatus({ state: "failed", url, code, description: statusText });
    });

    // What counts as "the web app is loading" is a MAIN-FRAME,
    // CROSS-DOCUMENT navigation, and nothing else.
    //
    // The obvious-looking `did-start-loading` is wrong here and cost a bug that
    // made the app unusable: it is the tab's spinner, so it also fires for
    // subframe loads and — fatally — for same-document navigations. The SPA is
    // a `BrowserRouter`, so every route change it makes is a `pushState`,
    // which raises `did-start-loading` and is answered only by
    // `did-stop-loading`. `did-finish-load` never fires again, because no
    // document was ever loaded. The status therefore stuck on "loading", which
    // hides this view (see `#setStatus`) and leaves the shell's own spinner
    // over a page that had already rendered — for good. Pressing Reload forced
    // a real document load and "fixed" it, which is exactly the shape the bug
    // reports had: it hung on first paint, it hung again on the next route
    // change, and a reload always cleared it.
    //
    // `did-start-navigation` carries both discriminators, so the shell reacts
    // to real navigations and stays out of the way of the router.
    contents.on("did-start-navigation", (details) => {
      if (!details.isMainFrame || details.isSameDocument) return;
      failed = false;
      this.#setStatus({ state: "loading", url: details.url || startUrl });
    });
    contents.on("did-finish-load", () => {
      if (failed) return;
      this.#setStatus({ state: "ready", url: contents.getURL() || startUrl });
    });
    // A same-document navigation changes the address without changing the
    // state: the page is already on screen and stays there. Only the URL we
    // report is stale, and the offline screen reads it.
    contents.on("did-navigate-in-page", (_event, url, isMainFrame) => {
      if (!isMainFrame || this.#status.state !== "ready") return;
      this.#setStatus({ state: "ready", url });
    });
    // Only the main frame matters: a subresource that 404s is the page's own
    // problem, and reporting it would put the offline screen over a page that
    // is on screen and working.
    contents.on("did-fail-load", (_event, code, description, url, isMainFrame) => {
      if (!isMainFrame) return;
      // -3 is ERR_ABORTED, which is what a navigation superseded by another
      // navigation reports. It is not a failure anyone can act on.
      if (code === -3) return;
      failed = true;
      this.#setStatus({ state: "failed", url: url || origin, code, description });
    });
    contents.on("render-process-gone", () => {
      this.#setStatus({
        state: "failed",
        url: contents.getURL() || startUrl,
        description: "The page stopped responding.",
      });
    });

    window.contentView.addChildView(view);
    this.#layout();
    void contents.loadURL(startUrl);
  }

  #setStatus(status: CloudStatus): void {
    this.#status = status;
    // The view is hidden while it is not showing a working page, so the
    // shell's own screen is visible rather than a white rectangle behind it.
    this.#cloud?.setVisible(status.state === "ready");
    this.#deps.onCloudStatus(status);
  }

  /** Position the web app under the title bar. */
  #layout(): void {
    const window = this.#window;
    if (!window || window.isDestroyed() || !this.#cloud) return;
    const [width, height] = window.getContentSize();
    this.#cloud.setBounds({
      x: 0,
      y: CHROME_HEIGHT,
      width: Math.max(0, width),
      height: Math.max(0, height - CHROME_HEIGHT),
    });
  }
}

function sameOrigin(a: string, b: string): boolean {
  // Not `URL.origin`: the product is served from a custom scheme now, and that
  // property is the literal string "null" for every such URL. Comparing two of
  // them therefore answered TRUE for any pair — `evil://x` would have counted
  // as same-origin with the app. See originOf().
  const left = originOf(a);
  return left !== null && left === originOf(b);
}

/**
 * Our own renderer has no reason to navigate anywhere, and no reason to open a
 * window. A navigation there is either a bug or a page that got somewhere it
 * should not be.
 */
function hardenShellNavigation(window: BrowserWindow): void {
  window.webContents.on("will-navigate", (event) => event.preventDefault());
  window.webContents.setWindowOpenHandler(({ url }) => {
    openExternally(url);
    return { action: "deny" };
  });
}

/**
 * Navigation policy for the web app, and the reason the bridge's powers can
 * be granted at all.
 *
 * The preload is attached to this view for its whole life, so "which origin is
 * in it" is the only thing separating the TaskTrooper app from any other page.
 * Two rules therefore:
 *
 *  - `will-navigate` refuses anything off the trusted origin and hands it to
 *    the real browser instead. A link to an external site opens in Safari;
 *    it does not become a page holding this bridge.
 *  - `setWindowOpenHandler` denies every popup for the same reason. There is no
 *    sign-in in this product, so there is no window worth making an exception
 *    for.
 *
 * The main process re-checks the origin on every IPC call regardless
 * (`main/ipc.ts`), because a policy is only as good as the case its author
 * thought of.
 */
function hardenCloudNavigation(
  view: WebContentsView,
  origin: string,
  home: string,
  onHttpError: (url: string, code: number, statusText: string) => void,
): void {
  const contents = view.webContents;

  contents.on("will-navigate", (event, url) => {
    if (sameOrigin(url, origin)) return;
    event.preventDefault();
    openExternally(url);
  });

  // A same-origin page that redirects itself to another origin arrives here
  // rather than at will-navigate. Bounced to the home route rather than the
  // origin root, for the same reason the window opens there: `/` is the
  // marketing page, and recovering onto it is recovering to the wrong screen.
  contents.on("did-navigate", (_event, url) => {
    if (sameOrigin(url, origin)) return;
    void contents.loadURL(home);
  });

  // An error page is not a failed load — the body arrives and renders — so it
  // has to be caught here or the user reads it inside an app window and
  // concludes the app is broken.
  contents.on("did-navigate", (_event, url, httpResponseCode, httpStatusText) => {
    if (httpResponseCode < 400) return;
    onHttpError(url, httpResponseCode, httpStatusText);
  });

  // There is no sign-in in this product, so there is no popup worth allowing:
  // every window this page tries to open is a link, and links go to the user's
  // real browser.
  contents.setWindowOpenHandler(({ url }) => {
    openExternally(url);
    return { action: "deny" };
  });
}

/**
 * https only. `file:`, `javascript:` and the rest are how "open a link" becomes
 * "run something".
 *
 * There is one place in this app where a URL becomes something the OS acts on,
 * and a second one would be a second thing to keep correct. Returns whether it
 * actually asked the OS to open something, so a caller that owes the user an
 * answer — the PR link on a task card, say — can say "that link is not one I
 * can open" instead of doing nothing and looking hung.
 */
export function openExternally(url: string): boolean {
  try {
    if (new URL(url).protocol !== "https:") return false;
    void shell.openExternal(url);
    return true;
  } catch {
    // Not a URL; there is nothing to open and nothing to say about it.
    return false;
  }
}
