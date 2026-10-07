import { Menu, Tray, app } from "electron";
import type { SupervisorSnapshot, UpdateStatus } from "../ipc/types.js";
import { trayIcon, type TrayGlyph } from "./tray-icons.js";

/**
 * The menu bar item (C8).
 *
 * This is the app's real interface most of the time. The window is something
 * you open when something is wrong; the tray is what tells you whether
 * anything is. So it carries the state in its glyph, the reason in its
 * tooltip, and exactly four actions — the ones you would want without opening
 * the window.
 *
 * Quit here is the draining quit, not `app.quit()`. See main/index.ts: the
 * backend comes down first and with time to use, so the Claude Code sessions it
 * is supervising are cancelled rather than orphaned.
 */

export interface TrayDeps {
  /** Opens the window, optionally on one of the web app's own routes. */
  showWindow: (route?: string) => void;
  start: () => void;
  stop: () => void;
  quit: () => void;
  checkForUpdate: () => void;
  /** Drains the children, then hands off to Squirrel. Relaunches the app. */
  restartToUpdate: () => void;
}

export class AppTray {
  #tray: Tray | null = null;
  readonly #deps: TrayDeps;
  #snapshot: SupervisorSnapshot | null = null;
  #update: UpdateStatus = { phase: "unsupported" };

  constructor(deps: TrayDeps) {
    this.#deps = deps;
  }

  create(): void {
    if (this.#tray) return;
    this.#tray = new Tray(trayIcon("stopped"));
    this.#tray.setToolTip("TaskTrooper — not running");
    this.#tray.on("click", () => this.#deps.showWindow());
    this.#render();
  }

  update(snapshot: SupervisorSnapshot): void {
    this.#snapshot = snapshot;
    this.#render();
  }

  updateStatus(status: UpdateStatus): void {
    this.#update = status;
    this.#render();
  }

  destroy(): void {
    this.#tray?.destroy();
    this.#tray = null;
  }

  #render(): void {
    const tray = this.#tray;
    if (!tray) return;

    const snapshot = this.#snapshot;
    const glyph = glyphFor(snapshot);
    tray.setImage(trayIcon(glyph));
    tray.setToolTip(`TaskTrooper — ${describe(snapshot)}`);

    const busy = snapshot?.state === "starting" || snapshot?.state === "stopping" || snapshot?.state === "preflight";
    const up = snapshot?.state === "running" || snapshot?.state === "degraded";

    tray.setContextMenu(
      Menu.buildFromTemplate([
        { label: describe(snapshot), enabled: false },
        { type: "separator" },
        { label: "Start the local server", enabled: !busy && !up, click: () => this.#deps.start() },
        { label: "Stop the local server", enabled: !busy && up, click: () => this.#deps.stop() },
        { type: "separator" },
        // Routes in the web app, not tabs in a shell: the local controls live on
        // the Claude Code card of Settings → LLM Connection.
        { label: "Claude Code settings…", click: () => this.#deps.showWindow("/settings/llm") },
        { label: "Board…", click: () => this.#deps.showWindow("/board") },
        ...this.#updateItems(up),
        { type: "separator" },
        {
          // The label says what it does, because "Quit" next to a running
          // backend is a promise about how it ends.
          label: up ? "Quit (stops the local server)" : "Quit",
          accelerator: "CmdOrCtrl+Q",
          click: () => this.#deps.quit(),
        },
      ]),
    );
  }

  /**
   * The update affordance, and the reason it is this small.
   *
   * A build with no feed shows nothing — no greyed-out "Check for updates", no
   * "updates unavailable". The one line that appears when something is actually
   * ready says what pressing it costs, because pressing it takes the backend
   * down and brings the app back.
   */
  #updateItems(up: boolean): Electron.MenuItemConstructorOptions[] {
    const status = this.#update;
    if (status.phase === "unsupported") return [];

    const items: Electron.MenuItemConstructorOptions[] = [{ type: "separator" }];

    if (status.phase === "ready") {
      items.push({
        label: up
          ? `Restart to update${status.version ? ` to ${status.version}` : ""} (stops the local server)`
          : `Restart to update${status.version ? ` to ${status.version}` : ""}`,
        click: () => this.#deps.restartToUpdate(),
      });
      return items;
    }

    if (status.phase === "available") {
      items.push({ label: `Downloading update… ${status.percent ?? 0}%`, enabled: false });
      return items;
    }

    items.push({
      label: status.phase === "checking" ? "Checking for updates…" : "Check for updates…",
      enabled: status.phase !== "checking",
      click: () => this.#deps.checkForUpdate(),
    });
    if (status.phase === "error" && status.detail) {
      items.push({ label: `Last check failed: ${status.detail}`, enabled: false });
    }
    return items;
  }
}

function glyphFor(snapshot: SupervisorSnapshot | null): TrayGlyph {
  if (!snapshot) return "stopped";
  switch (snapshot.state) {
    case "running":
      return "running";
    case "degraded":
    case "starting":
    case "preflight":
    case "stopping":
      return "degraded";
    default:
      return "stopped";
  }
}

/**
 * One line, and it has to be the line the user actually wants. "Degraded" on
 * its own is useless; "agent-server restarting" is not.
 */
function describe(snapshot: SupervisorSnapshot | null): string {
  if (!snapshot) return "not running";
  switch (snapshot.state) {
    case "idle":
      return "not started";
    case "preflight":
      return "checking configuration…";
    case "starting":
      return snapshot.step ? `starting ${snapshot.step}…` : "starting…";
    case "running":
      return "running";
    case "degraded": {
      const restarting = snapshot.children.find((c) => c.state === "restarting" || c.state === "crashed");
      return restarting ? `${restarting.id} restarting` : "not answering";
    }
    case "stopping":
      return "stopping…";
    case "stopped":
      return "stopped";
    case "failed":
      return snapshot.detail ?? "failed to start";
    default:
      return "unknown";
  }
}

/** Launch-at-login, as macOS's own login-items API sees it. */
export function getLaunchAtLogin(): boolean {
  return app.getLoginItemSettings().openAtLogin;
}

export function setLaunchAtLogin(enabled: boolean): boolean {
  app.setLoginItemSettings({
    openAtLogin: enabled,
    // Opened hidden: a machine that reboots overnight should come back with the
    // backend running and no window in the user's face at 9am.
    openAsHidden: true,
  });
  return getLaunchAtLogin();
}
