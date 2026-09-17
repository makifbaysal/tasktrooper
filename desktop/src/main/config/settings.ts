import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import path from "node:path";
import { app } from "electron";
import type { Overrides, UserSettings } from "../../ipc/types.js";
import { validateOverrides, validateSettingsPatch } from "../../ipc/validate.js";
import { defaultWorkspaceDir } from "./workspace.js";

/**
 * Non-secret state: three user settings, and any manual overrides for what
 * detection failed to find. Plain JSON in userData.
 *
 * Split from the Keychain-backed secrets on purpose. These are values a user
 * might reasonably read, diff, or paste into a bug report; putting them behind
 * the same encryption as the API key would make both harder to reason about.
 * Nothing here is a credential, and every credential is in SecretStore.
 *
 * It does not hold a port either. The backend picks its own and prints it, so
 * there is nothing here that could disagree with what is actually running.
 */

const FILE = "settings.json";

interface StoredState {
  settings: UserSettings;
  overrides?: Overrides;
  /** The workspace before the last change, so "reveal the old one" can work. */
  previousWorkspaceDir?: string;
}

export function defaultSettings(): UserSettings {
  return {
    workspaceDir: defaultWorkspaceDir(),
    launchAtLogin: false,
    // On by default, because the backend IS the product: an app that launches
    // and then waits to be told to start its own server would open on an
    // offline screen every single time.
    autoConnect: true,
    notifications: {
      enabled: true,
      analizReview: true,
      humanUat: true,
      humanNeeded: true,
      agentComments: true,
    },
  };
}

export class SettingsStore {
  readonly #file: string;
  #cache: StoredState | null = null;

  constructor(dir = app.getPath("userData")) {
    this.#file = path.join(dir, FILE);
  }

  #state(): StoredState {
    if (this.#cache) return this.#cache;
    let stored: Partial<StoredState> = {};
    try {
      const raw: unknown = JSON.parse(readFileSync(this.#file, "utf8"));
      if (typeof raw === "object" && raw !== null) stored = raw as Partial<StoredState>;
    } catch {
      stored = {};
    }
    this.#cache = {
      // Run the file through the same validators IPC uses. A settings.json
      // someone hand-edited is untrusted input in exactly the way an IPC
      // payload is, and it reaches the same `spawn` call.
      settings: { ...defaultSettings(), ...safe(() => validateSettingsPatch(stored.settings ?? {}), {}) },
      overrides: safe(() => validateOverrides(stored.overrides ?? {}), {}),
      ...(typeof stored.previousWorkspaceDir === "string" ? { previousWorkspaceDir: stored.previousWorkspaceDir } : {}),
    };
    return this.#cache;
  }

  #write(next: StoredState): void {
    mkdirSync(path.dirname(this.#file), { recursive: true });
    writeFileSync(this.#file, `${JSON.stringify(next, null, 2)}\n`, "utf8");
    this.#cache = next;
  }

  get(): UserSettings {
    return this.#state().settings;
  }

  set(patch: Partial<UserSettings>): UserSettings {
    const state = this.#state();
    const before = state.settings.workspaceDir;
    const settings = { ...state.settings, ...patch };
    const next: StoredState = {
      ...state,
      settings,
      ...(patch.workspaceDir !== undefined && patch.workspaceDir !== before
        ? { previousWorkspaceDir: before }
        : {}),
    };
    this.#write(next);
    return settings;
  }

  overrides(): Overrides {
    return this.#state().overrides ?? {};
  }

  setOverrides(patch: Overrides): Overrides {
    const state = this.#state();
    const merged: Overrides = { ...state.overrides, ...patch };
    // An explicit "" clears rather than sets: the diagnostics view's only way
    // to undo an override it should not have needed.
    for (const key of Object.keys(merged) as (keyof Overrides)[]) {
      if (merged[key] === "") delete merged[key];
    }
    this.#write({ ...state, overrides: merged });
    return merged;
  }

  previousWorkspaceDir(): string | undefined {
    return this.#state().previousWorkspaceDir;
  }
}

function safe<T>(fn: () => T, fallback: T): T {
  try {
    return fn();
  } catch {
    return fallback;
  }
}
