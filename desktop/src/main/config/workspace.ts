import { accessSync, constants, existsSync, mkdirSync, statfsSync } from "node:fs";
import os from "node:os";
import path from "node:path";
import { app, dialog, shell } from "electron";
import type { ChooseDirectoryRequest } from "../../ipc/channels.js";
import type { WorkspaceCheck } from "../../ipc/types.js";

/**
 * The workspace directory: where Claude Code sessions write `workspaces/`, `repos/`
 * and `agents/`.
 *
 * This is the one path the user genuinely should see and choose. It fills with
 * git checkouts — hundreds of megabytes of them — and they will open it in
 * Finder, so hiding it behind an opaque container path would be hiding the
 * thing they most need to find.
 *
 * The old default was `./data`, resolved against wherever the start script
 * happened to be launched from, which is how most of a gigabyte of task
 * workspaces accumulated next to a source checkout. The default here is
 * absolute, stable, and somewhere a person would look.
 */

/** `~/TaskTrooper` — visible in Finder's sidebar territory, not buried. */
export function defaultWorkspaceDir(): string {
  return path.join(os.homedir(), "TaskTrooper");
}

/**
 * Validate a candidate.
 *
 * The distinction that matters is error vs warning. An error is a directory
 * that cannot work — unwritable, inside the .app bundle. A warning is one that
 * works today and corrupts later, and those are said plainly and then left to
 * the user, because a cloud-synced folder is a legitimate choice for someone
 * who knows what they are doing and a disaster for someone who does not.
 */
export function checkWorkspace(candidate: string): WorkspaceCheck {
  const target = path.resolve(candidate);
  const warnings: string[] = [];

  if (!path.isAbsolute(target)) {
    return { path: target, ok: false, error: "Choose an absolute path.", warnings, exists: false };
  }

  // Inside the .app bundle is a hard no: the bundle is code-signed, so writing
  // into it breaks the signature, and it is replaced wholesale on every
  // update, so anything in it is deleted without warning.
  const appPath = app.getAppPath();
  if (target === appPath || target.startsWith(`${appPath}${path.sep}`) || target.includes(".app/Contents/")) {
    return {
      path: target,
      ok: false,
      error: "This is inside the app itself. It would be deleted on the next update and would break the app's signature.",
      warnings,
      exists: existsSync(target),
    };
  }

  const exists = existsSync(target);
  if (exists) {
    try {
      accessSync(target, constants.W_OK);
    } catch {
      return { path: target, ok: false, error: "This folder is not writable.", warnings, exists };
    }
  } else {
    // Creatable is enough — the parent has to exist and be writable. Creating
    // it eagerly here would leave stray folders behind every time someone
    // browsed and changed their mind.
    const parent = path.dirname(target);
    if (!existsSync(parent)) {
      return { path: target, ok: false, error: `${parent} does not exist.`, warnings, exists };
    }
    try {
      accessSync(parent, constants.W_OK);
    } catch {
      return { path: target, ok: false, error: `${parent} is not writable.`, warnings, exists };
    }
  }

  // Cloud-synced folders. A sync client rewriting files under a running `git`
  // produces corruption that looks like a git bug and takes hours to trace
  // back to the folder choice — so it is worth naming, by name.
  const cloudMarkers: [RegExp, string][] = [
    [/\/Library\/Mobile Documents\//, "iCloud Drive"],
    [/\/Dropbox(\/|$)/, "Dropbox"],
    [/\/Google Drive(\/|$)/i, "Google Drive"],
    [/\/OneDrive(\/|$)/i, "OneDrive"],
    [/\/Sync(thing)?(\/|$)/i, "a sync folder"],
  ];
  for (const [pattern, name] of cloudMarkers) {
    if (pattern.test(target)) {
      warnings.push(
        `This is in ${name}. Agents create git checkouts here, and a sync client rewriting files underneath one corrupts it in ways that are very hard to debug.`,
      );
      break;
    }
  }

  // Network and removable volumes: /Volumes is where both mount, and an agent
  // run that loses its workspace mid-clone fails in a way nobody will guess at.
  if (target.startsWith("/Volumes/")) {
    warnings.push(
      "This is on a mounted volume. If it is a network share or a removable disk, agent runs will fail when it goes away.",
    );
  }

  let freeBytes: number | undefined;
  try {
    const stats = statfsSync(exists ? target : path.dirname(target));
    freeBytes = stats.bavail * stats.bsize;
    // Repos plus their build artefacts. Below this, the first clone is the one
    // that fails, and it fails as a git error rather than as "disk full".
    if (freeBytes < 5 * 1024 ** 3) {
      warnings.push(`Only ${(freeBytes / 1024 ** 3).toFixed(1)} GB free here. Agents clone repositories into this folder.`);
    }
  } catch {
    freeBytes = undefined;
  }

  return { path: target, ok: true, warnings, exists, ...(freeBytes !== undefined ? { freeBytes } : {}) };
}

/** Create it, along with the three subdirectories a session expects to find. */
export function ensureWorkspace(dir: string): void {
  mkdirSync(dir, { recursive: true });
}

/**
 * The folder picker. Returns null when the user cancels — distinct from a
 * choice that failed validation, which comes back as a `WorkspaceCheck` with
 * an error so the UI can explain it.
 */
export async function pickWorkspace(current: string): Promise<WorkspaceCheck | null> {
  const result = await dialog.showOpenDialog({
    title: "Choose where agents work",
    message: "Agents clone repositories and write their workspaces here.",
    defaultPath: existsSync(current) ? current : os.homedir(),
    buttonLabel: "Use this folder",
    properties: ["openDirectory", "createDirectory"],
  });
  const chosen = result.filePaths[0];
  if (result.canceled || chosen === undefined) return null;
  return checkWorkspace(chosen);
}

/**
 * Generic folder picker. Opens the native OS directory chooser and returns
 * the selected directory's absolute path, or null when canceled.
 */
export async function pickDirectory(options?: ChooseDirectoryRequest): Promise<string | null> {
  const defaultPath = options?.defaultPath && existsSync(options.defaultPath)
    ? options.defaultPath
    : os.homedir();
  const result = await dialog.showOpenDialog({
    title: options?.title ?? "Choose Directory",
    defaultPath,
    buttonLabel: options?.buttonLabel ?? "Select",
    properties: ["openDirectory", "createDirectory"],
  });
  const chosen = result.filePaths[0];
  if (result.canceled || chosen === undefined) return null;
  return chosen;
}

export function reveal(target: string): void {
  if (existsSync(target)) shell.showItemInFolder(target);
  else void shell.openPath(path.dirname(target));
}
