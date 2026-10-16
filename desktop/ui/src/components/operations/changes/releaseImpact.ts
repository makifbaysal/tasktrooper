import type { ImpactVerdict, Release, ReleaseImpact, ReleaseImpactCheckSignal, ReleaseImpactSignal } from "@/api";
import { HEADLINE_QUANTILE } from "@/components/operations/charts/signalChart";
import { formatSignalValue, signalLabel, type SignalFormatContext } from "@/components/operations/console/signalFormat";

export type ImpactState = ImpactVerdict | "pending";

export const IMPACT_STATES: ImpactState[] = ["worse", "pending", "neutral", "improved", "unknown"];

const VERDICT_RANK: Record<ImpactVerdict, number> = { worse: 3, neutral: 2, improved: 1, unknown: 0 };

// A ratio's change reads in percentage points, which is also the unit its
// soak threshold is set in.
const POINT_UNITS = new Set(["%"]);

const QUANTILE_SIGNALS = new Set(["http.latency", "web.lcp", "web.inp", "mobile.cold_start"]);

type ImpactLike = Pick<ReleaseImpactCheckSignal, "signal" | "before" | "after" | "delta" | "delta_pct" | "verdict"> & {
  unit?: string;
  breached?: boolean;
  max_delta?: number | null;
};

/** The signal to name beside a verdict: a breached threshold first, then the
 * worst verdict, then the largest relative move. */
export function worstImpactSignal<T extends ImpactLike>(signals: T[]): T | undefined {
  const move = (s: T) => Math.abs(s.delta_pct ?? 0);
  return [...signals]
    .filter((s) => s.verdict !== "unknown" || s.breached)
    .sort(
      (a, b) =>
        Number(Boolean(b.breached)) - Number(Boolean(a.breached)) ||
        VERDICT_RANK[b.verdict] - VERDICT_RANK[a.verdict] ||
        move(b) - move(a),
    )[0];
}

/** What the change column says for a release: the soak's own impact check,
 * when its profile sets an impact threshold; nothing otherwise. */
export function listImpact(release: Pick<Release, "checks">): { state: ImpactState; worst?: ReleaseImpactCheckSignal } | undefined {
  const check = release.checks?.impact;
  if (!check) return undefined;
  const state: ImpactState = check.pending && check.verdict !== "worse" ? "pending" : check.verdict;
  return { state, worst: state === "unknown" ? undefined : worstImpactSignal(check.signals ?? []) };
}

export function impactState(impact: Pick<ReleaseImpact, "pending" | "verdict">): ImpactState {
  return impact.pending && impact.verdict !== "worse" ? "pending" : impact.verdict;
}

function signed(v: number, locale: string, digits = 1): string {
  const num = new Intl.NumberFormat(locale, { maximumFractionDigits: digits }).format(Math.abs(v));
  return `${v < 0 ? "−" : "+"}${num}`;
}

/** A signal's move across the deploy: percentage points for a ratio, else the
 * relative change, else the difference in the signal's own unit. */
export function impactDeltaText(s: ImpactLike, ctx: SignalFormatContext): string {
  const unit = s.unit ?? (s.max_delta != null ? "%" : undefined);
  if (unit && POINT_UNITS.has(unit) && s.delta != null) return ctx.t("operations.changes.impactVerdict.points", { value: signed(s.delta, ctx.locale) });
  if (s.delta_pct != null) return `${signed(s.delta_pct, ctx.locale)}%`;
  if (s.delta != null) {
    const abs = formatSignalValue(Math.abs(s.delta), unit, ctx);
    return `${s.delta < 0 ? "−" : "+"}${abs}`;
  }
  return "—";
}

/** "Error rate +3.2 pt": the named signal and its move. */
export function impactSummary(s: ImpactLike, ctx: SignalFormatContext): string {
  return `${impactSignalName(s.signal, ctx.t, true)} ${impactDeltaText(s, ctx)}`;
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
