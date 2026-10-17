import {
  ALERT_CHANNEL_KINDS,
  ALERT_CHANNELS_AVAILABLE,
  ALERT_READ_FAILURES_AS_NO_DATA,
  ALERT_RULE_STATES,
  type AlertBacktestFire,
  type AlertBacktestRequest,
  type AlertChannelKind,
  type AlertRule,
  type AlertRuleCondition,
  type AlertRuleEvent,
  type AlertRuleInput,
  type AlertRuleOp,
  type AlertRuleState,
  type AlertRuleSuggestion,
  type DeployEnvironment,
  type IncidentSeverity,
  type MetricSeries,
  type NotificationChannelInfo,
} from "@/api";
import type { Threshold } from "@/components/operations/charts/TimeSeriesChart";
import { formatSignalValue, signalLabel, type SignalFormatContext } from "@/components/operations/console/signalFormat";

export const ALERT_RULES_POLL_MS = 30_000;

export const BACKTEST_MIN_DAYS = 1;
export const BACKTEST_MAX_DAYS = 14;
export const BACKTEST_DEFAULT_DAYS = 7;

/** alerting's limits: a name, the window one value summarizes, the hold. */
export const ALERT_RULE_NAME_MAX = 200;
export const ALERT_WINDOW_MIN_SECONDS = 60;
export const ALERT_WINDOW_MAX_SECONDS = 24 * 3600;
export const ALERT_FOR_MAX_SECONDS = 7 * 24 * 3600;

export const ALERT_SEVERITIES: IncidentSeverity[] = ["critical", "high", "medium", "low"];

// The catalog's quantile signals and what alerting judges each at: Core Web
// Vitals at the 75th percentile, the rest at the 95th (alerting/service.go).
const QUANTILE_SIGNALS = new Set(["http.latency", "mobile.cold_start", "web.lcp", "web.inp", "web.cls", "web.ttfb"]);

export function alertQuantile(signal: string): string | undefined {
  if (!QUANTILE_SIGNALS.has(signal)) return undefined;
  return signal.startsWith("web.") ? "p75" : "p95";
}

/** The run of failed reads a rule held its state through, when there is one:
 * how many, the last failure, and whether it now reads as no data. */
export function readFailures(rule: Pick<AlertRule, "error_count" | "last_error">): { count: number; error: string; asNoData: boolean } | undefined {
  const count = rule.error_count ?? 0;
  if (count <= 0) return undefined;
  return { count, error: rule.last_error ?? "", asNoData: count > ALERT_READ_FAILURES_AS_NO_DATA };
}

export type AlertStateFilter = AlertRuleState | "all";

export function isAlertStateFilter(value: string | null): value is AlertStateFilter {
  return value === "all" || ALERT_RULE_STATES.some((s) => s === value);
}

export function opNeedsThreshold(op: AlertRuleOp): boolean {
  return op !== "no_data";
}

export function opIsChange(op: AlertRuleOp): boolean {
  return op === "change_pct_gt" || op === "change_pct_lt";
}

export interface DurationParts {
  value: number;
  unit: "seconds" | "minutes" | "hours";
}

/** Seconds in the largest unit that keeps them whole. */
export function durationParts(seconds: number): DurationParts {
  const s = Math.max(0, Math.round(seconds));
  if (s > 0 && s % 3600 === 0) return { value: s / 3600, unit: "hours" };
  if (s > 0 && s % 60 === 0) return { value: s / 60, unit: "minutes" };
  return { value: s, unit: "seconds" };
}

export function formatDurationSeconds(seconds: number, ctx: SignalFormatContext): string {
  const { value, unit } = durationParts(seconds);
  const num = new Intl.NumberFormat(ctx.locale, { maximumFractionDigits: 1 }).format(value);
  return `${num} ${ctx.t(`operations.units.${unit}`)}`;
}

export function ruleSignalName(rule: Pick<AlertRule, "signal" | "query">, t: SignalFormatContext["t"]): string {
  if (!rule.signal) return t("operations.alertRules.customQuery");
  const name = signalLabel(t, rule.signal);
  const quantile = alertQuantile(rule.signal);
  return quantile ? t("operations.alertRules.condition.quantile", { signal: name, quantile }) : name;
}

function signedPercent(v: number, locale: string): string {
  const num = new Intl.NumberFormat(locale, { maximumFractionDigits: 1 }).format(Math.abs(v));
  return `${v < 0 ? "−" : "+"}${num}%`;
}

/** A judged value as the table, the timeline and a peak show it: a change
 * rule's in signed percent, any other in the signal's unit. */
export function formatThreshold(op: AlertRuleOp, threshold: number | null | undefined, unit: string | undefined, ctx: SignalFormatContext): string {
  if (threshold == null || !Number.isFinite(threshold)) return "—";
  return opIsChange(op) ? signedPercent(threshold, ctx.locale) : formatSignalValue(threshold, unit, ctx);
}

/** The server tests `pct > threshold` (change_pct_gt) and `pct < threshold`
 * (change_pct_lt). Read as "rises by more than X%" and "falls by more than
 * X%", the editor holds the size X; a fall is sent as −X. A stored rule whose
 * sign does not fit the wording (a "change_pct_lt" at +5) is not one of those. */
export function changeWorded(op: AlertRuleOp, threshold: number | null | undefined): boolean {
  if (threshold == null || !Number.isFinite(threshold)) return true;
  if (op === "change_pct_gt") return threshold >= 0;
  if (op === "change_pct_lt") return threshold <= 0;
  return false;
}

function unsignedPercent(v: number, locale: string): string {
  return `${new Intl.NumberFormat(locale, { maximumFractionDigits: 1 }).format(Math.abs(v))}%`;
}

/** A condition as the editor holds it: no threshold until one is typed. */
export type DraftCondition = Omit<AlertRuleCondition, "threshold"> & { threshold?: number | null };

type ConditionRule = Pick<AlertRule, "signal" | "query"> & { condition: DraftCondition };

/** The rule's condition in words, the way New Relic reads one back:
 * "Response time (p95) > 800 ms for 10 min", "Error rate rises by more than
 * 50% over 10 min". */
export function conditionText(rule: ConditionRule, unit: string | undefined, ctx: SignalFormatContext): string {
  const c = (key: string, params?: Record<string, string | number>) => ctx.t(`operations.alertRules.condition.${key}`, params);
  const { op, threshold, for_seconds: forSeconds, window_seconds: windowSeconds } = rule.condition;
  const signal = ruleSignalName(rule, ctx.t);
  const value = formatThreshold(op, threshold, unit, ctx);
  const window = formatDurationSeconds(windowSeconds, ctx);
  const size = threshold == null || !Number.isFinite(threshold) ? "—" : unsignedPercent(threshold, ctx.locale);
  let text: string;
  switch (op) {
    case "gt":
      text = c("gt", { signal, threshold: value });
      break;
    case "lt":
      text = c("lt", { signal, threshold: value });
      break;
    case "change_pct_gt":
      text = changeWorded(op, threshold) ? c("rises", { signal, threshold: size, window }) : c("changeGt", { signal, threshold: value, window });
      break;
    case "change_pct_lt":
      text = changeWorded(op, threshold) ? c("falls", { signal, threshold: size, window }) : c("changeLt", { signal, threshold: value, window });
      break;
    case "rising":
      text = threshold ? c("rising", { signal, threshold: value, window }) : c("risingAny", { signal, window });
      break;
    default:
      return forSeconds > 0 ? c("noDataFor", { signal, duration: formatDurationSeconds(forSeconds, ctx) }) : c("noData", { signal });
  }
  return forSeconds > 0 ? c("for", { condition: text, duration: formatDurationSeconds(forSeconds, ctx) }) : text;
}

/** The line a chart of the rule's values draws: only a rule held to a level
 * has one (a change rule's threshold is on its change series). */
export function ruleThresholds(rule: { condition: DraftCondition }, label: string): Threshold[] {
  const { op, threshold } = rule.condition;
  if ((op !== "gt" && op !== "lt" && op !== "rising") || threshold == null || !Number.isFinite(threshold)) return [];
  return [{ value: threshold, label }];
}

/** The line drawn over the series a rule is judged on: a change rule's on
 * its percent change, any other as `ruleThresholds`. */
export function judgedThresholds(rule: { condition: DraftCondition }, label: string): Threshold[] {
  const { op, threshold } = rule.condition;
  if (!opIsChange(op)) return ruleThresholds(rule, label);
  return threshold == null || !Number.isFinite(threshold) ? [] : [{ value: threshold, label }];
}

/** The backtest's series names (alerting/backtest.go). */
export const BACKTEST_CHANGE_SERIES = "change_pct";
export const BACKTEST_QUERY_SERIES = "query";

/** Where a rule's chart reads what the evaluator judges: the series read for
 * a signal (it answers p50/p75/p95/p99, so a web vital's p75 too), the query
 * for a custom one, and a server replay for what neither answers — a change
 * rule's percent change. */
export type JudgedSource = "series" | "query" | "replay";

export function judgedSource(rule: Pick<AlertRule, "signal" | "condition">): JudgedSource {
  if (opIsChange(rule.condition.op)) return "replay";
  return rule.signal ? "series" : "query";
}

const QUANTILE_HEAD = /^p\d{2}(?:\s|$)/;

/** Of a quantile signal's p50/p75/p95/p99 lines, the one its rule is judged at;
 * a read with no quantile lines (a query without `$__quantile`) as it is. */
export function judgedSeries(signal: string, series: MetricSeries[]): MetricSeries[] {
  const quantile = alertQuantile(signal);
  if (!quantile || !series.some((s) => QUANTILE_HEAD.test(s.name ?? ""))) return series;
  return series.filter((s) => (s.name ?? "").split(" ")[0] === quantile);
}

const DAY_MS = 86_400_000;

/** The days a replay reaches back to cover a window starting at `from`. */
export function replayDays(from: number, now: number): number {
  const span = Number.isFinite(from) ? now - from : 0;
  return Math.min(BACKTEST_MAX_DAYS, Math.max(BACKTEST_MIN_DAYS, Math.ceil(span / DAY_MS)));
}

/** A saved rule replayed for its chart: channels and issues play no part in
 * what is judged, so none are sent. */
export function replayRequest(rule: AlertRule, days: number): AlertBacktestRequest {
  return {
    name: rule.name,
    component_id: rule.component_id,
    environment: rule.environment,
    signal: rule.signal,
    ...(rule.signal ? {} : { data_source_id: rule.data_source_id, query: rule.query }),
    condition: rule.condition,
    severity: rule.severity,
    channels: [],
    open_incident: false,
    enabled: rule.enabled,
    days: clampBacktestDays(days),
  };
}

export function stateCounts(rules: Pick<AlertRule, "state">[]): Record<AlertRuleState, number> {
  const counts = Object.fromEntries(ALERT_RULE_STATES.map((s) => [s, 0])) as Record<AlertRuleState, number>;
  for (const r of rules) if (r.state in counts) counts[r.state] += 1;
  return counts;
}

const STATE_RANK: Record<AlertRuleState, number> = { firing: 0, pending: 1, no_data: 2, ok: 3 };

/** Firing first, then pending, no data and OK; a disabled rule after every
 * enabled one; then by name. */
export function sortRules<T extends Pick<AlertRule, "state" | "enabled" | "name">>(rules: T[]): T[] {
  return [...rules].sort(
    (a, b) =>
      Number(!a.enabled) - Number(!b.enabled) ||
      (STATE_RANK[a.state] ?? 9) - (STATE_RANK[b.state] ?? 9) ||
      a.name.localeCompare(b.name),
  );
}

export function ruleMatches(rule: AlertRule, query: string, extra: Array<string | undefined> = []): boolean {
  const q = query.trim().toLowerCase();
  if (!q) return true;
  return [rule.name, rule.signal, rule.query, ...extra].some((v) => typeof v === "string" && v.toLowerCase().includes(q));
}

export function filterRules(rules: AlertRule[], state: AlertStateFilter, query: string, extraOf: (rule: AlertRule) => Array<string | undefined> = () => []): AlertRule[] {
  return rules.filter((r) => (state === "all" || r.state === state) && ruleMatches(r, query, extraOf(r)));
}

/** Newest first. */
export function ruleTimeline(events: AlertRuleEvent[]): AlertRuleEvent[] {
  return [...events].sort((a, b) => Date.parse(b.at) - Date.parse(a.at));
}

export interface FireBand {
  from: number;
  to: number;
  peak?: number | null;
  open: boolean;
}

/** A fire still on at the end of the span runs to `until`. */
export function fireBands(fires: AlertBacktestFire[], until: number): FireBand[] {
  return fires
    .map((f) => {
      const from = Date.parse(f.start);
      const end = f.end ? Date.parse(f.end) : NaN;
      return { from, to: Number.isFinite(end) ? end : until, peak: f.peak, open: !Number.isFinite(end) };
    })
    .filter((b) => Number.isFinite(b.from) && b.to >= b.from)
    .sort((a, b) => a.from - b.from);
}

/** When the rule was firing, from its state changes: a span opens on a move
 * to firing and closes on the next move out of it, or runs to `until`. */
export function firingBands(events: AlertRuleEvent[], until: number): FireBand[] {
  const bands: FireBand[] = [];
  let open: number | null = null;
  for (const ev of [...events].sort((a, b) => Date.parse(a.at) - Date.parse(b.at))) {
    const at = Date.parse(ev.at);
    if (!Number.isFinite(at)) continue;
    if (ev.to_state === "firing" && open === null) open = at;
    else if (ev.to_state !== "firing" && open !== null) {
      bands.push({ from: open, to: at, open: false });
      open = null;
    }
  }
  if (open !== null) bands.push({ from: open, to: Math.max(open, until), open: true });
  return bands;
}

// ---- The editor's model ----------------------------------------------

export type RuleSourceMode = "signal" | "custom";

export interface AlertRuleDraft {
  name: string;
  componentId: string;
  environment: DeployEnvironment;
  mode: RuleSourceMode;
  signal: string;
  dataSourceId: string;
  query: string;
  op: AlertRuleOp;
  /** As typed, so "0." and "" survive editing. */
  threshold: string;
  forSeconds: number;
  windowSeconds: number;
  severity: IncidentSeverity;
  channels: AlertChannelKind[];
  openIncident: boolean;
  enabled: boolean;
}

export type DraftField =
  | "name"
  | "componentId"
  | "environment"
  | "signal"
  | "dataSourceId"
  | "query"
  | "op"
  | "threshold"
  | "forSeconds"
  | "windowSeconds"
  | "severity"
  | "channels"
  | "days";

export type DraftErrors = Partial<Record<DraftField, string>>;

export function emptyRuleDraft(
  componentId: string,
  environment: DeployEnvironment,
  channels: readonly AlertChannelKind[] = ALERT_CHANNELS_AVAILABLE,
): AlertRuleDraft {
  return {
    name: "",
    componentId,
    environment,
    mode: "signal",
    signal: "",
    dataSourceId: "",
    query: "",
    op: "gt",
    threshold: "",
    forSeconds: 300,
    windowSeconds: 300,
    severity: "medium",
    channels: [...channels],
    openIncident: false,
    enabled: true,
  };
}

/** A number as it is typed back in the reader's locale, without grouping. */
export function thresholdInputText(value: number, locale = "en-US"): string {
  return new Intl.NumberFormat(locale, { useGrouping: false, maximumFractionDigits: 10 }).format(value);
}

/** The draft holds a change rule's size; a fall is stored as its negative. */
function heldThreshold(op: AlertRuleOp, stored: number): number {
  return op === "change_pct_lt" && stored !== 0 ? -stored : stored;
}

export function draftFromRule(rule: AlertRule | AlertRuleSuggestion, locale = "en-US"): AlertRuleDraft {
  const custom = !rule.signal && (Boolean(rule.query) || Boolean(rule.data_source_id));
  const op = rule.condition?.op ?? "gt";
  const threshold = rule.condition?.threshold;
  return {
    name: rule.name ?? "",
    componentId: rule.component_id,
    environment: rule.environment,
    mode: custom ? "custom" : "signal",
    signal: rule.signal ?? "",
    dataSourceId: rule.data_source_id ?? "",
    query: rule.query ?? "",
    op,
    threshold: threshold == null || !Number.isFinite(threshold) ? "" : thresholdInputText(heldThreshold(op, threshold), locale),
    forSeconds: rule.condition?.for_seconds ?? 0,
    windowSeconds: rule.condition?.window_seconds ?? 300,
    severity: rule.severity ?? "medium",
    channels: [...(rule.channels ?? [])],
    openIncident: rule.open_incident ?? false,
    enabled: rule.enabled ?? true,
  };
}

/** A suggestion names no channel; the editor offers the ones that deliver,
 * for the person to keep or clear before saving. */
export function draftFromSuggestion(
  s: AlertRuleSuggestion,
  locale = "en-US",
  available: readonly AlertChannelKind[] = ALERT_CHANNELS_AVAILABLE,
): AlertRuleDraft {
  const draft = draftFromRule(s, locale);
  return draft.channels.length > 0 ? draft : { ...draft, channels: [...available] };
}

/** The default name when none is typed: the condition itself. */
export function suggestedRuleName(draft: AlertRuleDraft, unit: string | undefined, ctx: SignalFormatContext): string {
  return conditionText(draftConditionRule(draft, ctx.locale), unit, ctx);
}

const SPACES = /[\s  ]/g;
const PLAIN_NUMBER = /^-?(?:\d+\.?\d*|\.\d+)$/;

function separators(locale: string): { group: string; decimal: string } {
  const parts = new Intl.NumberFormat(locale).formatToParts(12345.6);
  return {
    group: parts.find((p) => p.type === "group")?.value ?? ",",
    decimal: parts.find((p) => p.type === "decimal")?.value ?? ".",
  };
}

function escapeRe(s: string): string {
  return s.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

/** A typed number in the reader's locale: "1,000.5" in English, "1.000,5" in
 * Turkish or German. A lone separator that cannot be grouping ("0.5" in
 * German, "1,5" in English) is the decimal point; both separators in the
 * other order are the other convention. Spaces and a leading + or − (U+2212)
 * are allowed. */
export function parseThresholdText(text: string, locale = "en-US"): number | null {
  let s = text.trim().replace(SPACES, "").replace(/^[−–]/, "-").replace(/^\+/, "");
  if (s === "") return null;
  const { group: rawGroup, decimal } = separators(locale);
  const group = rawGroup.replace(SPACES, "");
  if (group && group !== decimal) {
    const hasGroup = s.includes(group);
    const hasDecimal = s.includes(decimal);
    if (hasGroup && hasDecimal) {
      if (s.lastIndexOf(decimal) > s.lastIndexOf(group)) s = s.split(group).join("");
      else s = s.split(decimal).join("").replace(group, decimal);
    } else if (hasGroup) {
      const grouped = new RegExp(`^-?\\d{1,3}(?:${escapeRe(group)}\\d{3})+$`);
      if (grouped.test(s)) s = s.split(group).join("");
      else if (s.split(group).length === 2) s = s.replace(group, decimal);
      else return null;
    }
  }
  s = s.replace(decimal, ".");
  if (!PLAIN_NUMBER.test(s)) return null;
  const n = Number(s);
  return Number.isFinite(n) ? n : null;
}

/** The threshold the server is sent: the typed number, or for a fall its negative. */
export function draftThreshold(draft: Pick<AlertRuleDraft, "op" | "threshold">, locale = "en-US"): number | null {
  const typed = parseThresholdText(draft.threshold, locale);
  return typed === null ? null : heldThreshold(draft.op, typed);
}

/** The draft as `conditionText` reads it. */
export function draftConditionRule(draft: AlertRuleDraft, locale = "en-US"): ConditionRule {
  return {
    signal: draft.mode === "signal" ? draft.signal : "",
    query: draft.mode === "custom" ? draft.query : "",
    condition: {
      op: draft.op,
      threshold: opNeedsThreshold(draft.op) ? draftThreshold(draft, locale) : null,
      for_seconds: Number.isFinite(draft.forSeconds) ? draft.forSeconds : 0,
      window_seconds: Number.isFinite(draft.windowSeconds) ? draft.windowSeconds : 0,
    },
  };
}

export function validateRuleDraft(draft: AlertRuleDraft, locale = "en-US"): DraftErrors {
  const errors: DraftErrors = {};
  if (draft.name.trim().length > ALERT_RULE_NAME_MAX) errors.name = "too_long";
  if (!draft.componentId) errors.componentId = "required";
  if (draft.mode === "signal") {
    if (!draft.signal) errors.signal = "required";
  } else {
    if (!draft.dataSourceId) errors.dataSourceId = "required";
    if (!draft.query.trim()) errors.query = "required";
  }
  if (opNeedsThreshold(draft.op)) {
    const typed = parseThresholdText(draft.threshold, locale);
    if (draft.threshold.trim() === "") errors.threshold = "required";
    else if (typed === null) errors.threshold = "not_a_number";
    else if (opIsChange(draft.op) && typed < 0) errors.threshold = "signed_change";
  }
  if (!Number.isFinite(draft.forSeconds) || draft.forSeconds < 0) errors.forSeconds = "invalid";
  else if (draft.forSeconds > ALERT_FOR_MAX_SECONDS) errors.forSeconds = "too_long";
  if (!Number.isFinite(draft.windowSeconds) || draft.windowSeconds < ALERT_WINDOW_MIN_SECONDS) errors.windowSeconds = "window_short";
  else if (draft.windowSeconds > ALERT_WINDOW_MAX_SECONDS) errors.windowSeconds = "too_long";
  return errors;
}

export function draftErrorCount(errors: DraftErrors): number {
  return Object.values(errors).filter(Boolean).length;
}

/** What is sent: a bound signal, or a data source and its query — never both.
 * Channels go as the draft holds them: one this build cannot offer, or the
 * server does not deliver yet, stays on the rule until it is cleared. */
export function ruleInput(draft: AlertRuleDraft, fallbackName: string, locale = "en-US"): AlertRuleInput {
  const { signal, query, condition } = draftConditionRule(draft, locale);
  const custom = draft.mode === "custom";
  return {
    name: draft.name.trim() || fallbackName,
    component_id: draft.componentId,
    environment: draft.environment,
    signal,
    ...(custom ? { data_source_id: draft.dataSourceId, query: (query ?? "").trim() } : {}),
    condition: {
      op: condition.op,
      threshold: opNeedsThreshold(condition.op) ? (condition.threshold ?? 0) : 0,
      for_seconds: Math.round(condition.for_seconds),
      window_seconds: Math.round(condition.window_seconds),
    },
    severity: draft.severity,
    channels: [...draft.channels],
    open_incident: draft.openIncident,
    enabled: draft.enabled,
  };
}

export function clampBacktestDays(days: number): number {
  if (!Number.isFinite(days)) return BACKTEST_DEFAULT_DAYS;
  return Math.min(BACKTEST_MAX_DAYS, Math.max(BACKTEST_MIN_DAYS, Math.round(days)));
}

export function validBacktestDays(days: number): boolean {
  return Number.isInteger(days) && days >= BACKTEST_MIN_DAYS && days <= BACKTEST_MAX_DAYS;
}

/** A backtest names no channel: channels play no part in what is judged, and
 * the server refuses one that does not deliver on any rule it reads. */
export function backtestRequest(draft: AlertRuleDraft, days: number, fallbackName: string, locale = "en-US"): AlertBacktestRequest {
  return { ...ruleInput(draft, fallbackName, locale), channels: [], days: clampBacktestDays(days) };
}

/** What a backtest's result depends on: what is read and how it is judged —
 * not the name, severity, channels, the issue switch or whether it is on. */
export function backtestKey(draft: AlertRuleDraft, locale = "en-US"): string {
  const { component_id, environment, signal, data_source_id, query, condition } = ruleInput(draft, "", locale);
  return JSON.stringify({ component_id, environment, signal, data_source_id, query, condition });
}

/** One channel as the editor lists it: what the server says of it, and
 * whether the rule already names it (kept, and shown, even when it cannot
 * deliver here or this build does not know it). */
export interface ChannelOption {
  kind: string;
  available: boolean;
  /** A kind this build has a name for. */
  known: boolean;
  onRule: boolean;
}

/** The server's list in its order (until it answers, `ALERT_CHANNEL_KINDS`
 * with only `ALERT_CHANNELS_AVAILABLE` delivering), then any other kind the
 * rule names. */
export function channelOptions(server: NotificationChannelInfo[] | null | undefined, ruleChannels: readonly string[]): ChannelOption[] {
  const listed: NotificationChannelInfo[] =
    server && server.length > 0
      ? server
      : ALERT_CHANNEL_KINDS.map((kind) => ({ kind, available: ALERT_CHANNELS_AVAILABLE.includes(kind) }));
  const known = (kind: string) => ALERT_CHANNEL_KINDS.some((k) => k === kind);
  const options = listed.map((c) => ({ kind: c.kind, available: c.available, known: known(c.kind), onRule: ruleChannels.includes(c.kind) }));
  for (const kind of ruleChannels) {
    if (!options.some((o) => o.kind === kind)) options.push({ kind, available: false, known: known(kind), onRule: true });
  }
  return options;
}

/** The kinds a new rule starts with: the ones that deliver here. */
export function deliveringChannels(server: NotificationChannelInfo[] | null | undefined): AlertChannelKind[] {
  if (!server || server.length === 0) return [...ALERT_CHANNELS_AVAILABLE];
  return server.filter((c) => c.available && ALERT_CHANNEL_KINDS.some((k) => k === c.kind)).map((c) => c.kind as AlertChannelKind);
}

/** A channel's name: the dictionary's for a known kind, else the kind as sent. */
export function channelLabel(kind: string, t: SignalFormatContext["t"]): string {
  return ALERT_CHANNEL_KINDS.some((k) => k === kind) ? t(`operations.alertRules.editor.channelKinds.${kind}`) : kind;
}

const SERVER_FIELDS: Record<string, DraftField> = {
  name: "name",
  component_id: "componentId",
  environment: "environment",
  signal: "signal",
  data_source_id: "dataSourceId",
  query: "query",
  op: "op",
  "condition.op": "op",
  threshold: "threshold",
  "condition.threshold": "threshold",
  for_seconds: "forSeconds",
  "condition.for_seconds": "forSeconds",
  window_seconds: "windowSeconds",
  "condition.window_seconds": "windowSeconds",
  severity: "severity",
  channels: "channels",
  days: "days",
};

/** A refusal's `fields` split into the draft's own fields and the rest; a
 * field the form does not show (absent from `rendered`) goes with the rest,
 * so no refusal is ever dropped. */
export function mapServerFields(
  fields: Record<string, string>,
  rendered?: ReadonlySet<DraftField>,
): { errors: DraftErrors; rest: Array<[string, string]> } {
  const errors: DraftErrors = {};
  const rest: Array<[string, string]> = [];
  for (const [key, code] of Object.entries(fields)) {
    const at = SERVER_FIELDS[key] ?? (key.startsWith("channels") ? "channels" : undefined);
    if (at && (!rendered || rendered.has(at))) errors[at] = code;
    else rest.push([key, code]);
  }
  return { errors, rest };
}

/** The list after a write: replaced in place, or added. */
export function upsertRule(rules: AlertRule[], rule: AlertRule): AlertRule[] {
  const at = rules.findIndex((r) => r.id === rule.id);
  if (at < 0) return [...rules, rule];
  const next = [...rules];
  next[at] = rule;
  return next;
}

export function removeRule(rules: AlertRule[], id: string): AlertRule[] {
  return rules.filter((r) => r.id !== id);
}
