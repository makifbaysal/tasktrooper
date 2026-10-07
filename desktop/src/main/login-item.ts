import { existsSync, mkdirSync, rmSync, writeFileSync } from "node:fs";
import os from "node:os";
import path from "node:path";
import { app } from "electron";

/**
 * Launch at login, on each OS's own terms.
 *
 * macOS has login items and `openAsHidden`. Windows has a Run-key entry with
 * arguments, so the app is told it was started by the login item with
 * `--hidden` and keeps its window closed. Linux has neither in Electron —
 * `setLoginItemSettings` is a silent no-op there, which is how the setting
 * came to read "on" while doing nothing — so it gets an XDG autostart entry,
 * the mechanism every desktop session honours.
 */

export const HIDDEN_FLAG = "--hidden";

export function autostartFile(env: NodeJS.ProcessEnv = process.env, home: string = os.homedir()): string {
  const configHome = env.XDG_CONFIG_HOME && path.isAbsolute(env.XDG_CONFIG_HOME) ? env.XDG_CONFIG_HOME : path.join(home, ".config");
  return path.join(configHome, "autostart", "tasktrooper.desktop");
}

/**
 * One Exec argument, quoted per the Desktop Entry spec: always in double
 * quotes, with `"`, `` ` ``, `$` and `\` backslash-escaped — and that backslash
 * escaped again, because the value is also a key-file string whose own escapes
 * are undone first. `%` is doubled so it is not read as a field code.
 */
export function desktopExecArg(arg: string): string {
  const quoted = arg.replace(/\\/g, "\\\\\\\\").replace(/(["`$])/g, "\\\\$1").replace(/%/g, "%%");
  return `"${quoted}"`;
}

export function autostartEntry(executable: string): string {
  return [
    "[Desktop Entry]",
    "Type=Application",
    "Version=1.0",
    "Name=TaskTrooper",
    "Comment=Start TaskTrooper's local server in the background",
    `Exec=${desktopExecArg(executable)} ${HIDDEN_FLAG}`,
    "Icon=tasktrooper",
    "Terminal=false",
    "X-GNOME-Autostart-enabled=true",
    "",
  ].join("\n");
}

/** The AppImage itself when running from one: `process.execPath` is inside its mount, which is gone after exit. */
function linuxExecutable(): string {
  return process.env.APPIMAGE || process.execPath;
}

export function getLaunchAtLogin(): boolean {
  if (process.platform === "linux") return existsSync(autostartFile());
  if (process.platform === "win32") return app.getLoginItemSettings({ args: [HIDDEN_FLAG] }).openAtLogin;
  return app.getLoginItemSettings().openAtLogin;
}

/** Returns what the OS now reports, which is the honest answer when writing the entry failed. */
export function setLaunchAtLogin(enabled: boolean): boolean {
  if (process.platform === "linux") {
    const file = autostartFile();
    try {
      if (enabled) {
        mkdirSync(path.dirname(file), { recursive: true });
        writeFileSync(file, autostartEntry(linuxExecutable()), { mode: 0o644 });
      } else {
        rmSync(file, { force: true });
      }
    } catch {
      // Reported through the return value: the file is there or it is not.
    }
    return getLaunchAtLogin();
  }
  if (process.platform === "win32") {
    app.setLoginItemSettings({ openAtLogin: enabled, args: [HIDDEN_FLAG] });
    return getLaunchAtLogin();
  }
  app.setLoginItemSettings({
    openAtLogin: enabled,
    // Opened hidden: a machine that reboots overnight should come back with the
    // backend running and no window in the user's face at 9am.
    openAsHidden: true,
  });
  return getLaunchAtLogin();
}

/** Started by the Windows or Linux login item. macOS hides the app itself through `openAsHidden`. */
export function launchedHidden(argv: string[] = process.argv, platform: NodeJS.Platform = process.platform): boolean {
  return platform !== "darwin" && argv.includes(HIDDEN_FLAG);
}
