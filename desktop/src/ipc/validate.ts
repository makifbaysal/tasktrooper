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

import {
  BUILT_IN_PROVIDER_TYPES,
  CHILD_IDS,
  CUSTOM_PROVIDER_TYPE,
  type ChildId,
  type ProviderKeyType,
  type NotificationPreferences,
  type RunnerPairingBundle,
  type UserSettings,
} from "./types.js";
import type {
  AccountSignInRequest,
  ChatFocusRequest,
  ChooseDirectoryRequest,
  DiagnosticsRequest,
  KeyRemoveRequest,
  KeysPrefill,
  KeySetRequest,
  LogsStreamRequest,
  OpenExternalRequest,
  PairRequest,
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
 * actually a link the OS should open is `openExternally`'s call in
 * `main/window.ts`, the one place that decision is made.
 */
export function validateOpenExternal(raw: unknown): OpenExternalRequest {
  const o = asRecord(raw, "openExternal");
  return { url: asCleanNonEmpty(o.url, "openExternal.url") };
}

/**
 * Both fields null (no chat open) or both non-empty ids — never one of each,
 * since a session id with no agent id (or the reverse) is not a screen this
 * app can render.
 */
export function validateChatFocus(raw: unknown): ChatFocusRequest {
  const o = asRecord(raw, "chatFocus");
  const agentId = o.agentId === null ? null : asCleanNonEmpty(o.agentId, "chatFocus.agentId", { max: 256 });
  const sessionId = o.sessionId === null ? null : asCleanNonEmpty(o.sessionId, "chatFocus.sessionId", { max: 256 });
  if ((agentId === null) !== (sessionId === null)) {
    fail("chatFocus: agentId and sessionId must both be null or both set");
  }
  return { agentId, sessionId };
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

export function validateLogsStream(raw: unknown): LogsStreamRequest {
  const o = asRecord(raw, "logsStream");
  return { on: asBoolean(o.on, "logsStream.on") };
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
    if (child !== "supervisor" && child !== "runner" && !(CHILD_IDS as readonly string[]).includes(child)) {
      fail(`logs.child: unknown child ${child}`);
    }
    out.child = child as ChildId | "supervisor";
  }
  if (o.afterSeq !== undefined) out.afterSeq = asInt(o.afterSeq, "logs.afterSeq", 0, Number.MAX_SAFE_INTEGER);
  if (o.limit !== undefined) out.limit = asInt(o.limit, "logs.limit", 1, 10_000);
  return out;
}

/**
 * The six notification switches, always given together — the settings page
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
    agentChatReplies: asBoolean(o.agentChatReplies, `${what}.agentChatReplies`),
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
  if (o.mode !== undefined) {
    if (o.mode !== "local" && o.mode !== "account") fail("settings.mode: expected local or account");
    out.mode = o.mode;
  }
  if (o.accountOrigin !== undefined) out.accountOrigin = asOriginShaped(o.accountOrigin, "settings.accountOrigin");
  return out;
}

/**
 * The shape of an account origin, and only the shape: `http(s)://host[:port]`
 * with nothing after it. Which schemes and hosts are acceptable on this build
 * is `main/account/origin.ts`'s decision, made where it is used.
 */
function asOriginShaped(value: unknown, what: string): string {
  const raw = asCleanNonEmpty(value, what, { max: 512 });
  let url: URL;
  try {
    url = new URL(raw);
  } catch {
    fail(`${what}: not a URL`);
  }
  if (url.protocol !== "https:" && url.protocol !== "http:") fail(`${what}: expected http or https`);
  if (url.username !== "" || url.password !== "") fail(`${what}: must not carry credentials`);
  if ((url.pathname !== "/" && url.pathname !== "") || url.search !== "" || url.hash !== "") {
    fail(`${what}: expected an origin, with no path, query or fragment`);
  }
  return `${url.protocol}//${url.host}`;
}

/**
 * `account.signIn(origin?)`. The origin is optional — absent means the one
 * this app would use anyway — and only its shape is checked here.
 */
export function validateSignIn(raw: unknown): AccountSignInRequest {
  if (raw === undefined || raw === null) return {};
  const o = asRecord(raw, "signIn");
  return o.origin === undefined ? {} : { origin: asOriginShaped(o.origin, "signIn.origin") };
}

/**
 * The shape of a pairing bundle crossing IPC: every field a clean, non-empty
 * string. Shallow on purpose; `main/config/pairing.ts#asPairingBundle`
 * re-validates (`tm_base_url` https or loopback) before anything is written,
 * and the main process checks the bundle's origin against the account's.
 */
export function validatePairRequest(raw: unknown): PairRequest {
  const o = asRecord(raw, "pair");
  const b = asRecord(o.bundle, "pair.bundle");
  const bundle: RunnerPairingBundle = {
    runner_token: asCleanNonEmpty(b.runner_token, "pair.bundle.runner_token", { max: 4096 }),
    tm_base_url: asCleanNonEmpty(b.tm_base_url, "pair.bundle.tm_base_url", { max: 512 }),
    tenant_id: asCleanNonEmpty(b.tenant_id, "pair.bundle.tenant_id", { max: 256 }),
    member_uid: asCleanNonEmpty(b.member_uid, "pair.bundle.member_uid", { max: 256 }),
    paired_at: asCleanNonEmpty(b.paired_at, "pair.bundle.paired_at", { max: 64 }),
    label: asCleanNonEmpty(b.label, "pair.bundle.label", { max: 256 }),
  };
  return { bundle };
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

export function validateChooseDirectory(raw: unknown): ChooseDirectoryRequest {
  if (raw === undefined || raw === null) return {};
  const o = asRecord(raw, "chooseDirectory");
  const out: ChooseDirectoryRequest = {};
  if (o.title !== undefined) out.title = asClean(o.title, "chooseDirectory.title", { max: 256 }).trim();
  if (o.defaultPath !== undefined) {
    const p = asClean(o.defaultPath, "chooseDirectory.defaultPath", { max: 1024 }).trim();
    if (p !== "") out.defaultPath = p;
  }
  if (o.buttonLabel !== undefined) {
    out.buttonLabel = asClean(o.buttonLabel, "chooseDirectory.buttonLabel", { max: 64 }).trim();
  }
  return out;
}

/**
 * A provider id: the identifier rule the runner holds `providers` to — a
 * built-in type's name, or a custom endpoint's UUID.
 */
const PROVIDER_ID = /^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$/;

function asProviderId(value: unknown, what: string): string {
  const id = asCleanNonEmpty(value, what, { max: 64 });
  if (!PROVIDER_ID.test(id)) fail(`${what}: letters, digits, '.', '_' and '-' only`);
  return id;
}

/**
 * The API-key window's `set`. Shape only — which id a type takes, whether a
 * key may be left out and where it may travel are `main/keys/keys.ts`'s.
 *
 * No message here quotes a value it refuses: the key is one of them, and a
 * refusal is read back by the page and may reach a log.
 */
export function validateKeySet(raw: unknown): KeySetRequest {
  const o = asRecord(raw, "keys.set");
  const type = asString(o.type, "keys.set.type", { max: 64 });
  if (!(BUILT_IN_PROVIDER_TYPES as readonly string[]).includes(type) && type !== CUSTOM_PROVIDER_TYPE) {
    fail("keys.set.type: not a provider type this app offers");
  }
  const out: KeySetRequest = { type: type as ProviderKeyType };
  if (o.id !== undefined) out.id = asProviderId(o.id, "keys.set.id");
  if (o.base_url !== undefined) {
    const base = asClean(o.base_url, "keys.set.base_url", { max: 512 }).trim();
    if (base !== "") out.base_url = base;
  }
  if (o.models !== undefined) {
    if (!Array.isArray(o.models)) fail("keys.set.models: expected a list");
    if (o.models.length > 32) fail("keys.set.models: at most 32");
    out.models = o.models.map((m, i) => asCleanNonEmpty(m, `keys.set.models[${i}]`, { max: 256 }));
  }
  if (o.api_key !== undefined) {
    if (typeof o.api_key !== "string") fail("keys.set.api_key: expected a string");
    if (o.api_key.length > 4096) fail("keys.set.api_key: longer than 4096 characters");
    if (CONTROL_CHARS.test(o.api_key)) fail("keys.set.api_key: contains control characters");
    const key = o.api_key.trim();
    if (key !== "") out.api_key = key;
  }
  return out;
}

const PREFILL_UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
const PREFILL_LOOPBACK = new Set(["127.0.0.1", "localhost", "[::1]"]);

function isPrefillAddress(raw: string): boolean {
  try {
    const url = new URL(raw);
    if (url.username !== "" || url.password !== "") return false;
    return url.protocol === "https:" || (url.protocol === "http:" && PREFILL_LOOPBACK.has(url.hostname));
  } catch {
    return false;
  }
}

/**
 * What the account's page may ask the key window to focus on. A key is not
 * accepted at all: a field this does not name is dropped, and `api_key` is
 * refused outright so a page that tries to send one learns it cannot.
 */
export function validateKeysPrefill(raw: unknown): KeysPrefill {
  const o = asRecord(raw, "keys.prefill");
  if ("api_key" in o) fail("keys.prefill: takes no key");
  const type = asString(o.type, "keys.prefill.type", { max: 64 });
  const builtIn = (BUILT_IN_PROVIDER_TYPES as readonly string[]).includes(type);
  if (!builtIn && type !== CUSTOM_PROVIDER_TYPE) fail("keys.prefill.type: not a provider type this app offers");
  const id = asString(o.id, "keys.prefill.id", { max: 64 });
  if (builtIn ? id !== type : !PREFILL_UUID.test(id)) {
    fail("keys.prefill.id: a built-in type's id is its type, a custom endpoint's a UUID");
  }
  const out: KeysPrefill = { id, type: type as ProviderKeyType };
  if (o.base_url !== undefined) {
    const base = asClean(o.base_url, "keys.prefill.base_url", { max: 512 }).trim();
    if (base !== "") {
      if (!isPrefillAddress(base)) fail("keys.prefill.base_url: must be https, or http to this computer");
      out.base_url = base;
    }
  }
  if (type === CUSTOM_PROVIDER_TYPE && out.base_url === undefined) {
    fail("keys.prefill.base_url: a custom endpoint needs its address");
  }
  if (o.models !== undefined) {
    if (!Array.isArray(o.models)) fail("keys.prefill.models: expected a list");
    if (o.models.length > 32) fail("keys.prefill.models: at most 32");
    out.models = o.models.map((m, i) => asCleanNonEmpty(m, `keys.prefill.models[${i}]`, { max: 256 }));
  }
  if (o.name !== undefined) {
    const name = asClean(o.name, "keys.prefill.name", { max: 120 }).trim();
    if (name !== "") out.name = name;
  }
  return out;
}

export function validateKeyRemove(raw: unknown): KeyRemoveRequest {
  const o = asRecord(raw, "keys.remove");
  return { id: asProviderId(o.id, "keys.remove.id") };
}
