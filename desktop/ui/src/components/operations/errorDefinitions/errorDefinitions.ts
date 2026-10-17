import {
  ERROR_DEFINITION_STATUSES,
  type ErrorAction,
  type ErrorAlertThreshold,
  type ErrorCategory,
  type ErrorDefinition,
  type ErrorDefinitionInput,
  type ErrorDefinitionStatus,
  type ErrorMatch,
  type IncidentSeverity,
} from "@/api";
import { formatDurationSeconds } from "@/components/operations/alertRules/alertRules";
import type { SignalFormatContext, Translate } from "@/components/operations/console/signalFormat";

// The server's limits (internal/application/errordef/service.go).
export const DEFINITION_LIMITS = {
  name: 200,
  rationale: 4000,
  evidence: 20,
  evidenceEntry: 500,
  fingerprint: 500,
  regex: 512,
  exceptionType: 200,
  endpoint: 500,
  attributes: 20,
  attributeKey: 100,
  attributeValue: 500,
  countMin: 1,
  countMax: 1_000_000,
  windowMin: 60,
  windowMax: 604_800,
} as const;

/** What the sweep judges an alert or incident definition by when it sets none. */
export const DEFAULT_THRESHOLDS: Record<"alert" | "incident", ErrorAlertThreshold> = {
  alert: { count: 10, window_seconds: 3600 },
  incident: { count: 1, window_seconds: 3600 },
};

export function isThresholded(action: ErrorAction): action is "alert" | "incident" {
  return action === "alert" || action === "incident";
}

export function isDefinitionStatus(value: string | null): value is ErrorDefinitionStatus {
  return ERROR_DEFINITION_STATUSES.some((s) => s === value);
}

export const ERROR_DEFINITIONS_PATH = "/operations/errors/definitions";

/** A definition's place on the Definitions page: its repository picked, its panel open. */
export function definitionPath(def: Pick<ErrorDefinition, "id" | "repository_id">): string {
  const qs = new URLSearchParams({ repository: def.repository_id, definition: def.id });
  return `${ERROR_DEFINITIONS_PATH}?${qs.toString()}`;
}

function shortFingerprint(fp: string): string {
  return fp.length > 16 ? `${fp.slice(0, 12)}…` : fp;
}

/** One clause per match field that is set, in words. */
export function matchClauses(match: ErrorMatch, t: Translate): string[] {
  const m = (key: string, params: Record<string, string | number>) => t(`operations.errorDefinitions.match.${key}`, params);
  const out: string[] = [];
  if (match.message_regex) out.push(m("messageRegex", { value: match.message_regex }));
  if (match.exception_type) out.push(m("exceptionType", { value: match.exception_type }));
  if (match.endpoint) out.push(m("endpoint", { value: match.endpoint }));
  if (match.status) out.push(m("status", { value: match.status }));
  for (const [key, value] of Object.entries(match.attributes ?? {})) out.push(m("attribute", { key, value }));
  if (match.fingerprint) out.push(m("fingerprint", { value: shortFingerprint(match.fingerprint) }));
  return out;
}

/** The whole match read back as one sentence: "message matches /…/ and status is 502". */
export function matchSummary(match: ErrorMatch, ctx: SignalFormatContext): string {
  const clauses = matchClauses(match, ctx.t);
  if (clauses.length === 0) return ctx.t("operations.errorDefinitions.match.empty");
  try {
    return new Intl.ListFormat(ctx.locale, { style: "long", type: "conjunction" }).format(clauses);
  } catch {
    return clauses.join(", ");
  }
}

/** The threshold an alert or incident definition is judged by; the server's default when it sets none. */
export function effectiveThreshold(def: Pick<ErrorDefinition, "action" | "alert_threshold">): {
  threshold: ErrorAlertThreshold;
  isDefault: boolean;
} | null {
  if (!isThresholded(def.action)) return null;
  if (def.alert_threshold) return { threshold: def.alert_threshold, isDefault: false };
  return { threshold: DEFAULT_THRESHOLDS[def.action], isDefault: true };
}

export function thresholdText(def: Pick<ErrorDefinition, "action" | "alert_threshold">, ctx: SignalFormatContext): string | null {
  const eff = effectiveThreshold(def);
  if (!eff) return null;
  const params = {
    count: new Intl.NumberFormat(ctx.locale).format(eff.threshold.count),
    window: formatDurationSeconds(eff.threshold.window_seconds, ctx),
  };
  return ctx.t(`operations.errorDefinitions.${eff.isDefault ? "thresholdDefault" : "threshold"}`, params);
}

// ---------------------------------------------------------------------------
// The regex test box. The server matches with Go's RE2; the browser has
// JavaScript's engine, so the pattern is translated where the two spell the
// same thing differently, and what RE2 refuses outright is said so.
// ---------------------------------------------------------------------------

const RE2_UNSUPPORTED = /\(\?<?[=!]|\\[1-9]|\\k</;

/** Lookarounds and backreferences: RE2 refuses them, so the server would too. */
export function re2Unsupported(pattern: string): boolean {
  return RE2_UNSUPPORTED.test(pattern);
}

export type CompiledPattern = { ok: true; regex: RegExp } | { ok: false; reason: "unsupported" | "invalid"; message: string };

export function compileForBrowser(pattern: string): CompiledPattern {
  if (re2Unsupported(pattern)) return { ok: false, reason: "unsupported", message: "" };
  let source = pattern;
  let flags = "";
  const lead = /^\(\?([imsU]+)\)/.exec(source);
  if (lead) {
    if (lead[1].includes("U")) return { ok: false, reason: "unsupported", message: "" };
    flags = [...new Set(lead[1].split(""))].join("");
    source = source.slice(lead[0].length);
  }
  source = source.replace(/\(\?P</g, "(?<").replace(/\\A/g, "^").replace(/\\z/g, "$");
  try {
    return { ok: true, regex: new RegExp(source, flags) };
  } catch (e) {
    return { ok: false, reason: "invalid", message: e instanceof Error ? e.message : String(e) };
  }
}

export type RegexTestResult =
  | { state: "empty" }
  | { state: "noSample" }
  | { state: "unsupported" }
  | { state: "invalid"; message: string }
  | { state: "noMatch" }
  | { state: "match"; start: number; end: number };

// The server reads at most 16 KiB of a message or a sample.
const SAMPLE_LIMIT = 16 * 1024;

export function testPattern(pattern: string, sample: string): RegexTestResult {
  if (pattern.trim() === "") return { state: "empty" };
  const compiled = compileForBrowser(pattern.trim());
  if (!compiled.ok) return compiled.reason === "unsupported" ? { state: "unsupported" } : { state: "invalid", message: compiled.message };
  if (sample === "") return { state: "noSample" };
  const found = compiled.regex.exec(sample.slice(0, SAMPLE_LIMIT));
  if (!found) return { state: "noMatch" };
  return { state: "match", start: found.index, end: found.index + found[0].length };
}

const REGEX_META = /[\\^$.*+?()[\]{}|]/g;
// A UUID, a hex id of 8 or more with a digit in it, or a run of digits.
const VARIABLE_TOKEN = /[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}|\b(?=[0-9a-f]*\d)[0-9a-f]{8,}\b|\d+/gi;

function escapeRegex(text: string): string {
  return text.replace(REGEX_META, "\\$&");
}

/** A pattern for an error message: its first line, literal, with the parts
 * that change from one occurrence to the next (ids, numbers) left open. */
export function suggestMessageRegex(message: string): string {
  const line = message
    .split("\n")
    .map((l) => l.trim())
    .find((l) => l !== "");
  if (!line) return "";
  const text = line.slice(0, 160);
  let out = "";
  let at = 0;
  for (const m of text.matchAll(VARIABLE_TOKEN)) {
    const index = m.index ?? 0;
    out += escapeRegex(text.slice(at, index));
    out += /^\d+$/.test(m[0]) ? "\\d+" : "[0-9a-f-]+";
    at = index + m[0].length;
  }
  out += escapeRegex(text.slice(at));
  return out.length > DEFINITION_LIMITS.regex ? out.slice(0, DEFINITION_LIMITS.regex).replace(/\\$/, "") : out;
}

// ---------------------------------------------------------------------------
// The editor's draft.
// ---------------------------------------------------------------------------

export interface DefinitionDraft {
  repositoryId: string;
  /** "" covers every component of the repository. */
  componentId: string;
  name: string;
  fingerprint: string;
  messageRegex: string;
  exceptionType: string;
  endpoint: string;
  status: string;
  attributes: Record<string, string>;
  category: ErrorCategory | "";
  severity: IncidentSeverity;
  action: ErrorAction;
  thresholdCount: string;
  thresholdWindowSeconds: number;
  rationale: string;
}

export type DraftField =
  | "repositoryId"
  | "componentId"
  | "name"
  | "match"
  | "fingerprint"
  | "messageRegex"
  | "exceptionType"
  | "endpoint"
  | "status"
  | "attributes"
  | "category"
  | "severity"
  | "action"
  | "threshold"
  | "thresholdCount"
  | "thresholdWindow"
  | "rationale"
  | "evidence";

/** A field's error code: the server's (`required`, `invalid`, `invalid_option`,
 * `too_long`, `not_permitted`) or the editor's own `re2_unsupported`. */
export type DraftErrors = Partial<Record<DraftField, string>>;

export function emptyDefinitionDraft(repositoryId = "", componentId = ""): DefinitionDraft {
  return {
    repositoryId,
    componentId,
    name: "",
    fingerprint: "",
    messageRegex: "",
    exceptionType: "",
    endpoint: "",
    status: "",
    attributes: {},
    category: "",
    severity: "medium",
    action: "none",
    thresholdCount: String(DEFAULT_THRESHOLDS.alert.count),
    thresholdWindowSeconds: DEFAULT_THRESHOLDS.alert.window_seconds,
    rationale: "",
  };
}

export function draftFromDefinition(def: ErrorDefinition): DefinitionDraft {
  const threshold = def.alert_threshold ?? (isThresholded(def.action) ? DEFAULT_THRESHOLDS[def.action] : DEFAULT_THRESHOLDS.alert);
  return {
    repositoryId: def.repository_id,
    componentId: def.component_id ?? "",
    name: def.name,
    fingerprint: def.match.fingerprint ?? "",
    messageRegex: def.match.message_regex ?? "",
    exceptionType: def.match.exception_type ?? "",
    endpoint: def.match.endpoint ?? "",
    status: def.match.status ? String(def.match.status) : "",
    attributes: { ...(def.match.attributes ?? {}) },
    category: def.category,
    severity: def.severity,
    action: def.action,
    thresholdCount: String(threshold.count),
    thresholdWindowSeconds: threshold.window_seconds,
    rationale: def.rationale ?? "",
  };
}

/** A new definition for one error group: this group exactly (its
 * fingerprint), and a message pattern to widen it to its kind. */
export function draftFromGroup(group: { repository_id: string; component_id: string; fingerprint: string; message: string }): DefinitionDraft {
  const firstLine = group.message.split("\n").find((l) => l.trim() !== "")?.trim() ?? "";
  return {
    ...emptyDefinitionDraft(group.repository_id, group.component_id),
    name: firstLine.length > 80 ? `${firstLine.slice(0, 79)}…` : firstLine,
    fingerprint: group.fingerprint,
    messageRegex: suggestMessageRegex(group.message),
  };
}

/** A switch to another action keeps a threshold typed by hand, and starts an
 * untouched one from the new action's default. */
export function switchAction(draft: DefinitionDraft, action: ErrorAction): DefinitionDraft {
  const next = { ...draft, action };
  if (!isThresholded(action)) return next;
  const wasDefault =
    !isThresholded(draft.action) ||
    (Number(draft.thresholdCount) === DEFAULT_THRESHOLDS[draft.action].count &&
      draft.thresholdWindowSeconds === DEFAULT_THRESHOLDS[draft.action].window_seconds);
  if (!wasDefault) return next;
  return { ...next, thresholdCount: String(DEFAULT_THRESHOLDS[action].count), thresholdWindowSeconds: DEFAULT_THRESHOLDS[action].window_seconds };
}

function cleanAttributes(attributes: Record<string, string>): Record<string, string> {
  const out: Record<string, string> = {};
  for (const [k, v] of Object.entries(attributes)) {
    const key = k.trim();
    if (key) out[key] = v.trim();
  }
  return out;
}

export function draftMatch(draft: DefinitionDraft): ErrorMatch {
  const match: ErrorMatch = {};
  if (draft.fingerprint.trim()) match.fingerprint = draft.fingerprint.trim();
  if (draft.messageRegex.trim()) match.message_regex = draft.messageRegex.trim();
  if (draft.exceptionType.trim()) match.exception_type = draft.exceptionType.trim();
  if (draft.endpoint.trim()) match.endpoint = draft.endpoint.trim();
  const status = Number(draft.status.trim());
  if (draft.status.trim() !== "" && Number.isInteger(status)) match.status = status;
  const attributes = cleanAttributes(draft.attributes);
  if (Object.keys(attributes).length > 0) match.attributes = attributes;
  return match;
}

/** The server's checks, so most refusals are caught before a round trip. */
export function validateDefinitionDraft(draft: DefinitionDraft): DraftErrors {
  const errors: DraftErrors = {};
  const L = DEFINITION_LIMITS;
  if (!draft.repositoryId) errors.repositoryId = "required";
  const name = draft.name.trim();
  if (!name) errors.name = "required";
  else if (name.length > L.name) errors.name = "too_long";

  const match = draftMatch(draft);
  if (draft.status.trim() !== "") {
    const status = Number(draft.status.trim());
    if (!Number.isInteger(status) || status < 100 || status > 599) errors.status = "invalid";
  }
  if (Object.keys(match).length === 0 && !errors.status) errors.match = "required";
  if ((match.fingerprint ?? "").length > L.fingerprint) errors.fingerprint = "too_long";
  if (match.message_regex) {
    if (match.message_regex.length > L.regex) errors.messageRegex = "too_long";
    else if (re2Unsupported(match.message_regex)) errors.messageRegex = "re2_unsupported";
  }
  if ((match.exception_type ?? "").length > L.exceptionType) errors.exceptionType = "too_long";
  if ((match.endpoint ?? "").length > L.endpoint) errors.endpoint = "too_long";
  const attributes = Object.entries(match.attributes ?? {});
  if (attributes.length > L.attributes || attributes.some(([k, v]) => k.length > L.attributeKey || v.length > L.attributeValue)) {
    errors.attributes = "too_long";
  }

  if (!draft.category) errors.category = "required";
  if (isThresholded(draft.action)) {
    const count = Number(draft.thresholdCount.trim());
    if (draft.thresholdCount.trim() === "" || !Number.isInteger(count) || count < L.countMin || count > L.countMax) {
      errors.thresholdCount = "invalid";
    }
    const w = draft.thresholdWindowSeconds;
    if (!Number.isFinite(w) || w < L.windowMin || w > L.windowMax) errors.thresholdWindow = "invalid";
  }
  if (draft.rationale.trim().length > L.rationale) errors.rationale = "too_long";
  return errors;
}

export function draftErrorCount(errors: DraftErrors): number {
  return Object.keys(errors).length;
}

/** The request body: everything for a new definition; for an edit, every
 * field the form holds (`component_id: ""` makes it repository-wide, and a
 * threshold is cleared when the action takes none). */
export function definitionInput(draft: DefinitionDraft, editing: boolean): ErrorDefinitionInput {
  const thresholded = isThresholded(draft.action);
  const threshold: ErrorAlertThreshold | null = thresholded
    ? { count: Number(draft.thresholdCount.trim()), window_seconds: Math.round(draft.thresholdWindowSeconds) }
    : null;
  const body: ErrorDefinitionInput = {
    name: draft.name.trim(),
    match: draftMatch(draft),
    category: draft.category || undefined,
    severity: draft.severity,
    action: draft.action,
    rationale: draft.rationale.trim(),
  };
  if (editing) {
    body.component_id = draft.componentId;
    body.alert_threshold = threshold;
    return body;
  }
  body.repository_id = draft.repositoryId;
  if (draft.componentId) body.component_id = draft.componentId;
  if (threshold) body.alert_threshold = threshold;
  return body;
}

const SERVER_FIELDS: Record<string, DraftField> = {
  repository_id: "repositoryId",
  component_id: "componentId",
  name: "name",
  match: "match",
  "match.fingerprint": "fingerprint",
  "match.message_regex": "messageRegex",
  "match.exception_type": "exceptionType",
  "match.endpoint": "endpoint",
  "match.status": "status",
  "match.attributes": "attributes",
  category: "category",
  severity: "severity",
  action: "action",
  alert_threshold: "threshold",
  "alert_threshold.count": "thresholdCount",
  "alert_threshold.window_seconds": "thresholdWindow",
  rationale: "rationale",
  evidence: "evidence",
};

/** A 400's `fields`, put under the form's own fields; `rest` names any the form does not show. */
export function mapDefinitionFields(
  fields: Record<string, string>,
  rendered?: ReadonlySet<DraftField>,
): { errors: DraftErrors; rest: Array<[string, string]> } {
  const errors: DraftErrors = {};
  const rest: Array<[string, string]> = [];
  for (const [key, code] of Object.entries(fields)) {
    const at = SERVER_FIELDS[key] ?? (key.startsWith("match.attributes.") ? "attributes" : undefined);
    if (at && (!rendered || rendered.has(at))) errors[at] = code;
    else rest.push([key, code]);
  }
  return { errors, rest };
}

// ---------------------------------------------------------------------------
// The list.
// ---------------------------------------------------------------------------

export function statusCounts(defs: ErrorDefinition[]): Record<ErrorDefinitionStatus, number> {
  const out: Record<ErrorDefinitionStatus, number> = { active: 0, suggested: 0, dismissed: 0 };
  for (const d of defs) out[d.status] += 1;
  return out;
}

export function definitionMatches(def: ErrorDefinition, query: string, t: Translate): boolean {
  const q = query.trim().toLowerCase();
  if (!q) return true;
  const haystack = [def.name, def.rationale, ...matchClauses(def.match, t), def.match.fingerprint ?? ""].join("\n").toLowerCase();
  return haystack.includes(q);
}

export function upsertDefinition(defs: ErrorDefinition[] | null, def: ErrorDefinition): ErrorDefinition[] {
  const list = defs ?? [];
  const at = list.findIndex((d) => d.id === def.id);
  if (at < 0) return [def, ...list];
  const next = [...list];
  next[at] = def;
  return next;
}

export function removeDefinition(defs: ErrorDefinition[] | null, id: string): ErrorDefinition[] {
  return (defs ?? []).filter((d) => d.id !== id);
}
