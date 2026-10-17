import type {
  ImpactVerdict,
  ObservabilitySignal,
  Release,
  ReleaseImpact,
  ReleaseImpactCheck,
  ReleaseImpactCheckSignal,
  ReleaseImpactSignal,
} from "@/api";
import { HEADLINE_QUANTILE } from "@/components/operations/charts/signalChart";
import { formatSignalValue, signalLabel, type SignalFormatContext } from "@/components/operations/console/signalFormat";

export type ImpactState = ImpactVerdict | "pending";

export const IMPACT_STATES: ImpactState[] = ["worse", "pending", "neutral", "improved", "unknown"];

const VERDICT_RANK: Record<ImpactVerdict, number> = { worse: 3, neutral: 2, improved: 1, unknown: 0 };

// A ratio's change reads in percentage points, which is also the unit its
// soak threshold is set in.
const POINT_UNITS = new Set(["%"]);

const QUANTILE_SIGNALS = new Set(["http.latency", "web.lcp", "web.inp", "mobile.cold_start"]);

/** The signal catalog by id (`GET /v1/signals`): a soak's check signals carry
 * no unit or direction of their own. */
export type SignalCatalog = ReadonlyMap<string, Pick<ObservabilitySignal, "unit" | "better">>;

type ImpactLike = Pick<ReleaseImpactCheckSignal, "signal" | "before" | "after" | "delta" | "delta_pct" | "verdict"> & {
  unit?: string;
  higher_is_better?: boolean;
  breached?: boolean;
  max_delta?: number | null;
};

function unitOf(s: ImpactLike, catalog?: SignalCatalog): string | undefined {
  return s.unit ?? catalog?.get(s.signal)?.unit ?? (s.max_delta != null ? "%" : undefined);
}

function betterOf(s: ImpactLike, catalog?: SignalCatalog): "higher" | "lower" | undefined {
  if (s.higher_is_better !== undefined) return s.higher_is_better ? "higher" : "lower";
  return catalog?.get(s.signal)?.better;
}

/** How bad a move is, comparable across signals: percentage points for a
 * ratio, percent otherwise, signed so a move the bad way ranks above the
 * same move the good way (by size alone when the direction is not known). */
export function impactSeverity(s: ImpactLike, catalog?: SignalCatalog): number {
  const unit = unitOf(s, catalog);
  const move = unit && POINT_UNITS.has(unit) ? s.delta : s.delta_pct;
  if (move == null || !Number.isFinite(move)) return 0;
  const better = betterOf(s, catalog);
  if (better === "lower") return move;
  if (better === "higher") return -move;
  return Math.abs(move);
}

/** The signal to name beside a verdict: a breached threshold first, then the
 * worst verdict, then the most severe move. */
export function worstImpactSignal<T extends ImpactLike>(signals: T[], catalog?: SignalCatalog): T | undefined {
  return [...signals]
    .filter((s) => s.verdict !== "unknown" || s.breached)
    .sort(
      (a, b) =>
        Number(Boolean(b.breached)) - Number(Boolean(a.breached)) ||
        VERDICT_RANK[b.verdict] - VERDICT_RANK[a.verdict] ||
        impactSeverity(b, catalog) - impactSeverity(a, catalog),
    )[0];
}

/** What the change column says for a release: the soak's own impact check,
 * when its profile sets an impact threshold; nothing otherwise. */
export function listImpact(
  release: Pick<Release, "checks">,
  catalog?: SignalCatalog,
): { state: ImpactState; worst?: ReleaseImpactCheckSignal } | undefined {
  const check = release.checks?.impact;
  if (!check) return undefined;
  const state: ImpactState = check.pending && check.verdict !== "worse" ? "pending" : check.verdict;
  return { state, worst: state === "unknown" ? undefined : worstImpactSignal(check.signals ?? [], catalog) };
}

export function impactState(impact: Pick<ReleaseImpact, "pending" | "verdict">): ImpactState {
  return impact.pending && impact.verdict !== "worse" ? "pending" : impact.verdict;
}

function signed(v: number, locale: string, digits = 1): string {
  const num = new Intl.NumberFormat(locale, { maximumFractionDigits: digits }).format(Math.abs(v));
  return `${v < 0 ? "−" : "+"}${num}`;
}

/** A signal's move across the deploy: percentage points for a ratio, else the
 * relative change, else the difference in the signal's own unit. The unit is
 * the signal's, else the catalog's. */
export function impactDeltaText(s: ImpactLike, ctx: SignalFormatContext, catalog?: SignalCatalog): string {
  const unit = unitOf(s, catalog);
  if (unit && POINT_UNITS.has(unit) && s.delta != null) return ctx.t("operations.changes.impactVerdict.points", { value: signed(s.delta, ctx.locale) });
  if (s.delta_pct != null) return `${signed(s.delta_pct, ctx.locale)}%`;
  if (s.delta != null) {
    const abs = formatSignalValue(Math.abs(s.delta), unit, ctx);
    return `${s.delta < 0 ? "−" : "+"}${abs}`;
  }
  return "—";
}

/** "Error rate +3.2 pt": the named signal and its move. */
export function impactSummary(s: ImpactLike, ctx: SignalFormatContext, catalog?: SignalCatalog): string {
  return `${impactSignalName(s.signal, ctx.t, true)} ${impactDeltaText(s, ctx, catalog)}`;
}

export function impactSignalName(signal: string, t: SignalFormatContext["t"], short = false): string {
  const name = signalLabel(t, signal, short);
  return QUANTILE_SIGNALS.has(signal) ? t("operations.alertRules.condition.quantile", { signal: name, quantile: HEADLINE_QUANTILE }) : name;
}

/** The threshold a soak held a signal to, in words ("≤ +2 pt", "≤ +20%"). */
export function impactThresholdText(s: Pick<ReleaseImpactCheckSignal, "max_delta" | "max_delta_pct">, ctx: SignalFormatContext): string | undefined {
  if (s.max_delta != null && s.max_delta > 0) return ctx.t("operations.changes.impactVerdict.atMost", { value: ctx.t("operations.changes.impactVerdict.points", { value: signed(s.max_delta, ctx.locale) }) });
  if (s.max_delta_pct != null && s.max_delta_pct > 0) return ctx.t("operations.changes.impactVerdict.atMost", { value: `${signed(s.max_delta_pct, ctx.locale)}%` });
  return undefined;
}

/** The value as the table shows it, "—" when the window had none. */
export function impactValue(v: number | null | undefined, unit: string | undefined, ctx: SignalFormatContext): string {
  return v == null || !Number.isFinite(v) ? "—" : formatSignalValue(v, unit, ctx);
}

/** The window an impact chart reads: `minutes` either side of the deploy. */
export function impactWindow(deployedAt: string | null | undefined, minutes: number): { from: number; to: number; at: number } | undefined {
  const at = deployedAt ? Date.parse(deployedAt) : NaN;
  if (!Number.isFinite(at)) return undefined;
  const span = Math.max(1, minutes) * 60_000;
  return { from: at - span, to: at + span, at };
}

export function sortImpactSignals(signals: ReleaseImpactSignal[], order: string[]): ReleaseImpactSignal[] {
  const rank = (id: string) => {
    const i = order.indexOf(id);
    return i < 0 ? order.length : i;
  };
  return [...signals].sort((a, b) => rank(a.signal) - rank(b.signal) || a.signal.localeCompare(b.signal));
}

export const RELEASE_IMPACT_PENDING_POLL_MS = 60_000;
// A settled impact does not change; the poll only guards a page left open.
export const RELEASE_IMPACT_SETTLED_POLL_MS = 10 * 60_000;

/** Every minute while the window after the deploy is open — the release is
 * still settling, or the impact's own answer says `pending` — else rarely. */
export function impactPollMs(settling: boolean, impactPending: boolean | undefined): number {
  return settling || impactPending === true ? RELEASE_IMPACT_PENDING_POLL_MS : RELEASE_IMPACT_SETTLED_POLL_MS;
}

/** A soak threshold held to a signal that never settled: not judged, so not passed. */
export function stillSettling(held: Pick<ReleaseImpactCheckSignal, "settling" | "breached"> | undefined): boolean {
  return Boolean(held?.settling && !held.breached);
}

function thresholdOf(held: Pick<ReleaseImpactCheckSignal, "max_delta" | "max_delta_pct">): string | undefined {
  if (held.max_delta != null) return "max_error_rate_delta";
  if (held.max_delta_pct != null) return "max_p95_delta_pct";
  return undefined;
}

/** The soak's unchecked thresholds by why: still settling when it ended (the
 * signal it holds them to says so), or not bound or not readable. */
export function uncheckedThresholds(check: Pick<ReleaseImpactCheck, "unchecked" | "signals"> | undefined): { settling: string[]; unread: string[] } {
  const settlingKeys = new Set((check?.signals ?? []).filter(stillSettling).map(thresholdOf).filter((k): k is string => Boolean(k)));
  const settling: string[] = [];
  const unread: string[] = [];
  for (const name of check?.unchecked ?? []) (settlingKeys.has(name) ? settling : unread).push(name);
  return { settling, unread };
}
