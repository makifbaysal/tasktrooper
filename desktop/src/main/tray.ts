import { Menu, Tray, nativeTheme } from "electron";
import type { SupervisorSnapshot, UpdateStatus } from "../ipc/types.js";
import { trayIcon, trayTone, type TrayGlyph, type TrayTone } from "./tray-icons.js";

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
  /**
   * What Start and Stop act on, in words: the local server, or in account
   * mode the runner. Read at each render, so it follows a mode switch.
   */
  subject?: () => string;
  /**
   * A state worth saying at the top of the menu itself — running locally for
   * now while still signed in — and the press that ends it. Read at each
   * render.
   */
  banner?: () => TrayBanner | null;
  /**
   * The anonymous-usage checkbox. Omitted when the build has no analytics
   * configuration, so a fork's tray carries no switch for nothing. The tray is
   * where an account-mode user reaches it: the account's page cannot show it.
   */
  analytics?: { available: () => boolean; checked: () => boolean; forcedOff: () => boolean; toggle: (on: boolean) => void };
}

export interface TrayBanner {
  label: string;
  action: string;
  run: () => void;
}

export class AppTray {
  #tray: Tray | null = null;
  readonly #deps: TrayDeps;
  #snapshot: SupervisorSnapshot | null = null;
  #update: UpdateStatus = { phase: "unsupported" };
  /**
   * What the tray last showed, as a key. The supervisor emits a state per
   * narration line and per child transition, and most of them change nothing
   * the tray draws; rebuilding the menu and the image for those is native
   * work done for nothing.
   */
  #shown = "";

  constructor(deps: TrayDeps) {
    this.#deps = deps;
  }

  /**
   * False when this session has no tray to put an icon in. The app keeps
   * running either way; relaunching it is then the way back to the window
   * (`second-instance` in index.ts).
   */
  create(): boolean {
    if (this.#tray) return true;
    try {
      this.#tray = new Tray(trayIcon("stopped", this.#tone()));
    } catch (err) {
      console.warn(`[tray] no tray icon: ${err instanceof Error ? err.message : String(err)}`);
      return false;
    }
    this.#tray.setToolTip("TaskTrooper — not running");
    this.#tray.on("click", () => this.#deps.showWindow());
    // Off macOS the glyph's colour is chosen for the panel, and the panel can
    // switch between light and dark while the app runs.
    if (process.platform !== "darwin") nativeTheme.on("updated", this.#onThemeUpdated);
    this.#render();
    return true;
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
    nativeTheme.off("updated", this.#onThemeUpdated);
    this.#tray?.destroy();
    this.#tray = null;
    this.#shown = "";
  }

  readonly #onThemeUpdated = (): void => this.#render();

  #tone(): TrayTone {
    return trayTone(process.platform, {
      systemDark: nativeTheme.shouldUseDarkColorsForSystemIntegratedUI,
      appDark: nativeTheme.shouldUseDarkColors,
    });
  }

  #render(): void {
    const tray = this.#tray;
    if (!tray) return;

    const snapshot = this.#snapshot;
    const glyph = glyphFor(snapshot);
    const tone = this.#tone();
    const line = describe(snapshot);
    const busy = snapshot?.state === "starting" || snapshot?.state === "stopping" || snapshot?.state === "preflight";
    const up = snapshot?.state === "running" || snapshot?.state === "degraded";
    const update = this.#update;
    const subject = this.#deps.subject?.() ?? "the local server";
    const banner = this.#deps.banner?.() ?? null;
    const analytics = this.#deps.analytics?.available() ? this.#deps.analytics : null;
    const analyticsChecked = analytics?.checked() ?? false;
    const analyticsLocked = analytics?.forcedOff() ?? false;
    const shown = JSON.stringify([
      glyph,
      tone,
      line,
      busy,
      up,
      subject,
      banner?.label,
      banner?.action,
      analytics !== null,
      analyticsChecked,
      analyticsLocked,
      update.phase,
      update.version,
      update.percent,
      update.detail,
    ]);
    if (shown === this.#shown) return;
    this.#shown = shown;

    tray.setImage(trayIcon(glyph, tone));
    tray.setToolTip(banner ? `TaskTrooper — ${banner.label}; ${line}` : `TaskTrooper — ${line}`);

    tray.setContextMenu(
      Menu.buildFromTemplate([
        ...(banner
          ? [
              { label: banner.label, enabled: false },
              { label: banner.action, click: () => banner.run() },
              { type: "separator" as const },
            ]
          : []),
        { label: line, enabled: false },
        { type: "separator" },
        { label: `Start ${subject}`, enabled: !busy && !up, click: () => this.#deps.start() },
        { label: `Stop ${subject}`, enabled: !busy && up, click: () => this.#deps.stop() },
        { type: "separator" },
        // Routes in the web app, not tabs in a shell: the local controls live on
        // the Claude Code card of Settings → LLM Connection.
        { label: "Claude Code settings…", click: () => this.#deps.showWindow("/settings/llm") },
        { label: "Board…", click: () => this.#deps.showWindow("/board") },
        ...this.#updateItems(up, subject),
        { type: "separator" },
        ...(analytics
          ? [
              {
                label: "Anonymous usage statistics",
                type: "checkbox" as const,
                checked: analyticsChecked,
                enabled: !analyticsLocked,
                click: (item: Electron.MenuItem) => analytics.toggle(item.checked),
              },
              { type: "separator" as const },
            ]
          : []),
        {
          // The label says what it does, because "Quit" next to a running
          // backend is a promise about how it ends. The accelerator is only a
          // label in a tray menu; the application menu (app-menu.ts) is what
          // binds it, on every platform.
          label: up ? `Quit (stops ${subject})` : "Quit",
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
  #updateItems(up: boolean, subject: string): Electron.MenuItemConstructorOptions[] {
    const status = this.#update;
    if (status.phase === "unsupported") return [];

    const items: Electron.MenuItemConstructorOptions[] = [{ type: "separator" }];

    if (status.phase === "ready") {
      items.push({
        label: up
          ? `Restart to update${status.version ? ` to ${status.version}` : ""} (stops ${subject})`
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
