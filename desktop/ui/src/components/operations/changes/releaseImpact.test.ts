import { describe, expect, it } from "vitest";
import type { ReleaseImpactCheck, ReleaseImpactSignal } from "@/api";
import {
  RELEASE_IMPACT_PENDING_POLL_MS,
  RELEASE_IMPACT_SETTLED_POLL_MS,
  impactDeltaText,
  impactPollMs,
  impactSeverity,
  impactState,
  impactSummary,
  impactThresholdText,
  impactWindow,
  listImpact,
  stillSettling,
  uncheckedThresholds,
  worstImpactSignal,
} from "@/components/operations/changes/releaseImpact";
import {
  incidentRollbackRelease,
  refusalKey,
  rollbackSuggestion,
  withoutProposal,
} from "@/components/operations/changes/rollbackSuggestion";
import { en } from "@/locales/en";
import { incident, release } from "@/test/opsConsoleFixtures";

function lookup(key: string): string | undefined {
  let cur: unknown = en;
  for (const part of key.split(".")) cur = (cur as Record<string, unknown> | undefined)?.[part];
  return typeof cur === "string" ? cur : undefined;
}
const t = (key: string, params?: Record<string, string | number>) =>
  (lookup(key) ?? key).replace(/\{(\w+)\}/g, (_, k: string) => String(params?.[k] ?? `{${k}}`));
const ctx = { locale: "en-US", t };

const check = (overrides: Partial<ReleaseImpactCheck> = {}): ReleaseImpactCheck => ({
  checked_at: "2026-10-15T10:30:00Z",
  pending: false,
  verdict: "worse",
  signals: [
    { signal: "http.requests", before: 100, after: 98, delta: -2, delta_pct: -2, verdict: "neutral" },
    { signal: "http.error_ratio", before: 0.4, after: 3.6, delta: 3.2, delta_pct: 800, verdict: "worse", max_delta: 2, breached: true },
    { signal: "http.latency", before: 200, after: 260, delta: 60, delta_pct: 30, verdict: "worse", max_delta_pct: 50 },
  ],
  ...overrides,
});

describe("deploy impact", () => {
  it("names the breached signal and its move in percentage points", () => {
    const impact = listImpact(release("r1", "r-shop", { checks: { impact: check() } }));
    expect(impact?.state).toBe("worse");
    expect(impact?.worst?.signal).toBe("http.error_ratio");
    expect(impactSummary(impact!.worst!, ctx)).toBe("Error rate +3.2 pt");
  });

  it("says pending while the window after the deploy is open, unless it is already worse", () => {
    expect(listImpact(release("r1", "r-shop", { checks: { impact: check({ pending: true, verdict: "neutral" }) } }))?.state).toBe("pending");
    expect(listImpact(release("r1", "r-shop", { checks: { impact: check({ pending: true }) } }))?.state).toBe("worse");
    expect(listImpact(release("r1", "r-shop", { checks: {} }))).toBeUndefined();
    expect(impactState({ pending: false, verdict: "improved" })).toBe("improved");
  });

  it("words a move by its unit and a threshold by its kind", () => {
    const latency: ReleaseImpactSignal = {
      signal: "http.latency",
      unit: "ms",
      before: 200,
      after: 260,
      baseline: 210,
      delta: 60,
      delta_pct: 30,
      higher_is_better: false,
      verdict: "worse",
    };
    expect(impactDeltaText(latency, ctx)).toBe("+30%");
    expect(impactDeltaText({ ...latency, delta_pct: null }, ctx)).toBe("+60 ms");
    expect(impactDeltaText({ ...latency, unit: "%", signal: "http.error_ratio", delta: -0.25 }, ctx)).toBe("−0.3 pt");
    expect(impactSummary(latency, ctx)).toBe("Response time (p95) +30%");
    expect(impactThresholdText({ max_delta: 2 }, ctx)).toBe("≤ +2 pt");
    expect(impactThresholdText({ max_delta_pct: 50 }, ctx)).toBe("≤ +50%");
    expect(impactThresholdText({}, ctx)).toBeUndefined();
  });

  it("picks the worst verdict, then the larger move in its own unit, points for a ratio", () => {
    const signals = check().signals.map((s) => ({ ...s, breached: false }));
    // +3.2 pt of errors against +30% of latency: never the ratio's raw +800%.
    expect(worstImpactSignal(signals)?.signal).toBe("http.latency");
    expect(worstImpactSignal([{ ...signals[0], verdict: "unknown" }])).toBeUndefined();
  });

  it("reads the charts an hour either side of the deploy", () => {
    const at = Date.parse("2026-10-15T10:00:00Z");
    expect(impactWindow("2026-10-15T10:00:00Z", 60)).toEqual({ from: at - 3_600_000, to: at + 3_600_000, at });
    expect(impactWindow(null, 60)).toBeUndefined();
  });
});

describe("the catalog's units and directions", () => {
  const catalog = new Map([
    ["http.error_ratio", { unit: "%", better: "lower" as const }],
    ["http.requests", { unit: "req/s", better: "higher" as const }],
    ["http.latency", { unit: "ms", better: "lower" as const }],
  ]);

  it("words a check signal's move in the catalog's unit", () => {
    const noThreshold = { signal: "http.error_ratio", before: 0.4, after: 1.4, delta: 1, delta_pct: 250, verdict: "worse" as const };
    expect(impactDeltaText(noThreshold, ctx)).toBe("+250%");
    expect(impactDeltaText(noThreshold, ctx, catalog)).toBe("+1 pt");
    expect(impactSummary(noThreshold, ctx, catalog)).toBe("Error rate +1 pt");
  });

  it("ranks by points for a ratio and percent otherwise, the bad way first", () => {
    const ratio = { signal: "http.error_ratio", before: 0.4, after: 1.4, delta: 1, delta_pct: 250, verdict: "worse" as const };
    const latency = { signal: "http.latency", before: 200, after: 260, delta: 60, delta_pct: 30, verdict: "worse" as const };
    expect(impactSeverity(ratio, catalog)).toBe(1);
    expect(impactSeverity(latency, catalog)).toBe(30);
    expect(worstImpactSignal([ratio, latency], catalog)?.signal).toBe("http.latency");
    expect(worstImpactSignal([ratio, latency])?.signal).toBe("http.error_ratio");

    const fewer = { signal: "http.requests", before: 100, after: 80, delta: -20, delta_pct: -20, verdict: "neutral" as const };
    const faster = { signal: "http.latency", before: 200, after: 150, delta: -50, delta_pct: -25, verdict: "neutral" as const };
    expect(impactSeverity(fewer, catalog)).toBe(20);
    expect(impactSeverity(faster, catalog)).toBe(-25);
    expect(worstImpactSignal([faster, fewer], catalog)?.signal).toBe("http.requests");
    expect(listImpact(release("r", "r-shop", { checks: { impact: check({ signals: [ratio, latency] }) } }), catalog)?.worst?.signal).toBe("http.latency");
  });

  it("re-reads the impact every minute while either side says the window is open", () => {
    expect(impactPollMs(true, false)).toBe(RELEASE_IMPACT_PENDING_POLL_MS);
    expect(impactPollMs(false, true)).toBe(RELEASE_IMPACT_PENDING_POLL_MS);
    expect(impactPollMs(false, false)).toBe(RELEASE_IMPACT_SETTLED_POLL_MS);
    expect(impactPollMs(false, undefined)).toBe(RELEASE_IMPACT_SETTLED_POLL_MS);
  });
});

describe("rollback suggestion", () => {
  const proposal = { proposed_at: "2026-10-15T10:20:00Z", by: "agent", reason: "verify_failed" as const, note: "error rate +3.2 pt", incident_id: "i-1" };

  it("comes only from the server's proposal, approvable only when the server allows it", () => {
    expect(rollbackSuggestion(release("r1", "r-shop", { status: "failed", rollback_allowed: true }))).toBeNull();
    expect(rollbackSuggestion(release("r1", "r-shop", { rollback_proposal: null, rollback_allowed: true }))).toBeNull();
    expect(rollbackSuggestion(release("r1", "r-shop", { rollback_proposal: proposal, rollback_allowed: true, rollback_refusal: "" }))).toEqual({
      kind: "approvable",
      proposal,
    });
    expect(rollbackSuggestion(release("r1", "r-shop", { rollback_proposal: proposal, rollback_allowed: false, rollback_refusal: "newer_shipped" }))).toEqual({
      kind: "refused",
      proposal,
      refusal: "newer_shipped",
    });
    expect(rollbackSuggestion(release("r1", "r-shop", { rollback_proposal: proposal }))).toMatchObject({ kind: "refused", refusal: "" });
    expect(refusalKey("too_old")).toBe("too_old");
    expect(refusalKey("not_configured")).toBe("not_configured");
    expect(refusalKey("frozen")).toBeUndefined();
  });

  it("puts a proposal on an issue only as the server attributed it, never by repository", () => {
    const issue = incident("i-1", "r-shop", "high", { task_id: "t-2" });
    const named = release("r-named", "r-other", { rollback_proposal: { ...proposal, proposed_at: "2026-10-15T09:00:00Z" } });
    const carrying = release("r-task", "r-shop", { rollback_proposal: { ...proposal, incident_id: null }, tasks: [{ id: "t-2" } as never] });
    const sameRepo = release("r-repo", "r-shop", { rollback_proposal: { ...proposal, incident_id: "i-9" }, tasks: [{ id: "t-9" } as never] });
    const noProposal = release("r-none", "r-shop", { tasks: [{ id: "t-2" } as never] });

    expect(incidentRollbackRelease(issue, [sameRepo, noProposal])).toBeUndefined();
    expect(incidentRollbackRelease(issue, [sameRepo, carrying])?.id).toBe("r-task");
    expect(incidentRollbackRelease(issue, [carrying, named])?.id).toBe("r-named");
    expect(incidentRollbackRelease({ id: "i-1" }, [carrying])).toBeUndefined();
  });

  it("applies a dismissal the server answered, or clears the proposal itself", () => {
    const r = release("r1", "r-shop", { rollback_proposal: proposal });
    expect(withoutProposal(r, undefined).rollback_proposal).toBeNull();
    const answered = { ...r, rollback_proposal: null, updated_at: "2026-10-15T11:00:00Z" };
    expect(withoutProposal(r, answered)).toBe(answered);
  });
});

describe("unchecked thresholds", () => {
  it("tells a threshold whose signal never settled from one that could not be read", () => {
    const check: Pick<ReleaseImpactCheck, "unchecked" | "signals"> = {
      unchecked: ["max_error_rate_delta", "max_p95_delta_pct"],
      signals: [{ signal: "http.error_ratio", before: 1, after: 1.2, verdict: "neutral", max_delta: 2, settling: true }],
    };
    expect(uncheckedThresholds(check)).toEqual({ settling: ["max_error_rate_delta"], unread: ["max_p95_delta_pct"] });
    expect(uncheckedThresholds(undefined)).toEqual({ settling: [], unread: [] });
    expect(stillSettling({ settling: true })).toBe(true);
    expect(stillSettling({ settling: true, breached: true })).toBe(false);
    expect(stillSettling({})).toBe(false);
  });
});
