/**
 * Runtime validation for every IPC payload.
 *
 * TypeScript types stop at the process boundary. `ipcMain.handle` receives
 * whatever the other side sent, and "the other side" is a renderer running a
 * bundle this process did not compile. So each handler runs its argument
 * through one of these before it means anything, and a failure is a thrown
 * Error the caller sees as a rejected promise — not a coerced default that
 * quietly does the wrong thing.
 *
 * Hand-written rather than a schema library: there are a handful of shapes,
 * they are all flat, and the checks that matter (a path that must be absolute,
 * an id that must be in a fixed set) are the ones a generic validator would
 * have needed custom refinements for anyway.
 */

import { CHILD_IDS, type ChildId, type NotificationPreferences, type UserSettings } from "./types.js";
import type {
  DiagnosticsRequest,
  OpenExternalRequest,
  PreflightRequest,
  RestartChildRequest,
  RevealRequest,
} from "./channels.js";
import type { HostLogsRequest, HostOverrides, HostPreferences } from "./host.js";

export class ValidationError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "ValidationError";
  }
}

function fail(what: string): never {
  throw new ValidationError(what);
}

function asRecord(value: unknown, what: string): Record<string, unknown> {
  if (typeof value !== "object" || value === null || Array.isArray(value)) {
    fail(`${what}: expected an object`);
  }
  return value as Record<string, unknown>;
}

function asString(value: unknown, what: string, { max = 4096 } = {}): string {
  if (typeof value !== "string") fail(`${what}: expected a string`);
  if (value.length > max) fail(`${what}: longer than ${max} characters`);
  return value;
}

function asInt(value: unknown, what: string, min: number, max: number): number {
  if (typeof value !== "number" || !Number.isFinite(value)) fail(`${what}: expected a finite number`);
  if (!Number.isInteger(value)) fail(`${what}: expected an integer`);
  if (value < min || value > max) fail(`${what}: out of range ${min}..${max}`);
  return value;
}

function asBoolean(value: unknown, what: string): boolean {
  if (typeof value !== "boolean") fail(`${what}: expected a boolean`);
  return value;
}

/**
 * No control characters, no newlines. Every one of these values ends up in a
 * child process's environment, an argv, a JSON config on a pipe, or a log
 * line; a newline in any of those is how one field becomes two.
 */
// eslint-disable-next-line no-control-regex -- matching control characters is the entire point.
const CONTROL_CHARS = /[\u0000-\u001f\u007f]/;

function asClean(value: unknown, what: string, opts?: { max?: number }): string {
  const s = asString(value, what, opts);
  if (CONTROL_CHARS.test(s)) fail(`${what}: contains control characters`);
  return s;
}

function asCleanNonEmpty(value: unknown, what: string, opts?: { max?: number }): string {
  const s = asClean(value, what, opts).trim();
  if (s === "") fail(`${what}: must not be empty`);
  return s;
}

const ABSOLUTE_PATH = /^(\/|[a-zA-Z]:[/\\]|\\\\)/;

function asAbsolutePath(value: unknown, what: string): string {
  const p = asCleanNonEmpty(value, what, { max: 1024 });
  if (!ABSOLUTE_PATH.test(p)) fail(`${what}: must be an absolute path`);
  return p;
}

/**
 * Reveal takes a NAME, not a path. The page cannot ask Finder to open an
 * arbitrary directory: it names one of three the main process already knows,
 * and the main process supplies the path.
 */
export function validateReveal(raw: unknown): RevealRequest {
  const o = asRecord(raw, "reveal");
  const what = asString(o.what, "reveal.what", { max: 32 });
  if (what !== "workspace" && what !== "previous-workspace" && what !== "logs") {
    fail("reveal.what: expected workspace, previous-workspace or logs");
  }
  return { what };
}

/**
 * A PR link, or any other external URL a task card wants opened. Only the
 * shape is checked here — non-empty, no control characters; whether it is
 * actually an `https:` link the OS should open is `openExternally`'s call in
 * `main/window.ts`, the one place that decision is made.
 */
export function validateOpenExternal(raw: unknown): OpenExternalRequest {
  const o = asRecord(raw, "openExternal");
  return { url: asCleanNonEmpty(o.url, "openExternal.url") };
}

export function validateDiagnosticsRequest(raw: unknown): DiagnosticsRequest {
  if (raw === undefined || raw === null) return {};
  const o = asRecord(raw, "diagnostics");
  return o.force === undefined ? {} : { force: asBoolean(o.force, "diagnostics.force") };
}

/**
 * The preflight request: one optional boolean, and nothing else will ever
 * belong here.
 *
 * It is worth saying why this validator exists at all for a payload this
 * small. `force` makes the main process re-run every probe, which spawns
 * `claude auth status` — real work, driven by a remote origin. A truthy
 * string would have been coerced by JavaScript into "always force", so a page
 * could turn a cheap read into a probe sweep on every render without ever
 * saying `true`. The narrow check is what keeps "force" a decision rather
 * than an accident.
 */
export function validatePreflightRequest(raw: unknown): PreflightRequest {
  if (raw === undefined || raw === null) return {};
  const o = asRecord(raw, "preflight");
  return o.force === undefined ? {} : { force: asBoolean(o.force, "preflight.force") };
}

export function validateRestartChild(raw: unknown): RestartChildRequest {
  const o = asRecord(raw, "restartChild");
  const child = asString(o.child, "restartChild.child", { max: 64 });
  if (!(CHILD_IDS as readonly string[]).includes(child)) fail(`restartChild.child: unknown child ${child}`);
  return { child: child as ChildId };
}

export function validateLogsRequest(raw: unknown): HostLogsRequest {
  if (raw === undefined || raw === null) return {};
  const o = asRecord(raw, "logs");
  const out: HostLogsRequest = {};
  if (o.child !== undefined) {
    const child = asString(o.child, "logs.child", { max: 64 });
    if (child !== "supervisor" && !(CHILD_IDS as readonly string[]).includes(child)) {
      fail(`logs.child: unknown child ${child}`);
    }
    out.child = child as ChildId | "supervisor";
  }
  if (o.afterSeq !== undefined) out.afterSeq = asInt(o.afterSeq, "logs.afterSeq", 0, Number.MAX_SAFE_INTEGER);
  if (o.limit !== undefined) out.limit = asInt(o.limit, "logs.limit", 1, 10_000);
  return out;
}

/**
 * The five notification switches, always given together — the settings page
 * always sends the full object (spread of the current value plus the one
 * field the user just toggled), so there is no partial-merge case to handle
 * here or in `SettingsStore`.
 */
function asNotifications(value: unknown, what: string): NotificationPreferences {
  const o = asRecord(value, what);
  return {
    enabled: asBoolean(o.enabled, `${what}.enabled`),
    analizReview: asBoolean(o.analizReview, `${what}.analizReview`),
    humanUat: asBoolean(o.humanUat, `${what}.humanUat`),
    humanNeeded: asBoolean(o.humanNeeded, `${what}.humanNeeded`),
    agentComments: asBoolean(o.agentComments, `${what}.agentComments`),
  };
}

/**
 * The whole user-facing settings surface: four fields.
 *
 * Not reachable from the bridge — the page gets `validatePreferences` below,
 * which is narrower. This one guards the settings file itself, which a person
 * can hand-edit and which is therefore untrusted input in exactly the way an
 * IPC payload is.
 */
export function validateSettingsPatch(raw: unknown): Partial<UserSettings> {
  const o = asRecord(raw, "settings");
  const out: Partial<UserSettings> = {};
  if (o.workspaceDir !== undefined) out.workspaceDir = asAbsolutePath(o.workspaceDir, "settings.workspaceDir");
  if (o.launchAtLogin !== undefined) out.launchAtLogin = asBoolean(o.launchAtLogin, "settings.launchAtLogin");
  if (o.autoConnect !== undefined) out.autoConnect = asBoolean(o.autoConnect, "settings.autoConnect");
  if (o.notifications !== undefined) out.notifications = asNotifications(o.notifications, "settings.notifications");
  return out;
}

/**
 * The switches the page may set.
 *
 * The workspace folder is deliberately absent: it is a path this app creates
 * directories under and hands to a Claude Code session, so it changes only
 * through the native picker, which the user drives. A page that could name it
 * could point a session at any directory the user can write to.
 */
export function validatePreferences(raw: unknown): HostPreferences {
  const o = asRecord(raw, "preferences");
  const out: HostPreferences = {};
  if (o.launchAtLogin !== undefined) out.launchAtLogin = asBoolean(o.launchAtLogin, "preferences.launchAtLogin");
  if (o.autoConnect !== undefined) out.autoConnect = asBoolean(o.autoConnect, "preferences.autoConnect");
  if (o.notifications !== undefined) out.notifications = asNotifications(o.notifications, "preferences.notifications");
  return out;
}

/**
 * Overrides exist only for what detection failed to find, and each is a path
 * to an executable this app will spawn — so each is an absolute path, and an
 * explicit empty string clears the override rather than setting "".
 *
 * A path arriving from the hosted page is the one place it names a filesystem
 * location, so this is only half the check: the main process also refuses one
 * that is not an existing executable file before storing it.
 */
export function validateOverrides(raw: unknown): HostOverrides {
  const o = asRecord(raw, "overrides");
  const out: HostOverrides = {};
  const pathOrClear = (key: keyof HostOverrides, what: string): void => {
    if (o[key] === undefined) return;
    const value = asClean(o[key], what, { max: 1024 }).trim();
    out[key] = value === "" ? "" : asAbsolutePath(value, what);
  };
  pathOrClear("claudeBin", "overrides.claudeBin");
  pathOrClear("gitBin", "overrides.gitBin");
  pathOrClear("chromeBin", "overrides.chromeBin");
  pathOrClear("appiumBin", "overrides.appiumBin");
  return out;
}
