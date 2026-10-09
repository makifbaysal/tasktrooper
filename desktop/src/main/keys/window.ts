import path from "node:path";
import { pathToFileURL } from "node:url";
import { BrowserWindow, app } from "electron";

/**
 * The API-key window: this app's own page, in a window of its own.
 *
 * Not a route of the web app. In account mode that view holds a remote
 * origin's page, and in local mode the same bundle (`desktop/ui`) is also
 * what a browser loads; a key typed into either would be a key typed into a
 * page that is not this process's to vouch for. This window is built from
 * `src/renderer/keys.html`, loads nothing remote, navigates nowhere, has a
 * preload that exposes the three `shell:keys:*` calls and nothing else, and
 * keeps its storage in memory. The main process answers those calls only for
 * this window's top frame on this page (`sender-guard.ts#isOwnPage`).
 */

/** In memory — no `persist:` — so nothing typed here reaches a disk cache. */
const KEYS_PARTITION = "tasktrooper-keys";

export class KeysWindow {
  #window: BrowserWindow | null = null;
  #pageUrl = "";

  /** The window's contents while it is open; the sender guard compares against it. */
  get contents(): Electron.WebContents | null {
    const window = this.#window;
    return window && !window.isDestroyed() ? window.webContents : null;
  }

  /** The URL the page was loaded from, exactly. "" while no window is open. */
  get pageUrl(): string {
    return this.contents ? this.#pageUrl : "";
  }

  /** Bring the window up, creating it the first time. */
  open(): void {
    const existing = this.#window;
    if (existing && !existing.isDestroyed()) {
      if (existing.isMinimized()) existing.restore();
      existing.show();
      existing.focus();
      return;
    }

    const window = new BrowserWindow({
      width: 640,
      height: 700,
      minWidth: 480,
      minHeight: 480,
      show: false,
      title: "API keys — TaskTrooper",
      backgroundColor: "#0b0d13",
      ...(process.platform === "darwin" ? {} : { autoHideMenuBar: true }),
      webPreferences: {
        preload: path.join(app.getAppPath(), "dist", "preload", "keys.cjs"),
        partition: KEYS_PARTITION,
        contextIsolation: true,
        nodeIntegration: false,
        sandbox: true,
        webSecurity: true,
        webviewTag: false,
        // A key is not a word; nothing typed here goes to a spelling service.
        spellcheck: false,
      },
    });

    window.once("ready-to-show", () => window.show());
    window.on("closed", () => {
      if (this.#window === window) {
        this.#window = null;
        this.#pageUrl = "";
      }
    });
    window.webContents.on("will-navigate", (event) => event.preventDefault());
    window.webContents.on("will-redirect", (event) => event.preventDefault());
    window.webContents.setWindowOpenHandler(() => ({ action: "deny" }));

    const dev = process.env.VITE_DEV_SERVER_URL;
    this.#pageUrl = dev
      ? new URL("keys.html", dev).href
      : pathToFileURL(path.join(app.getAppPath(), "dist", "renderer", "keys.html")).href;
    this.#window = window;
    void window.loadURL(this.#pageUrl);
  }

  close(): void {
    const window = this.#window;
    if (window && !window.isDestroyed()) window.close();
  }
}
