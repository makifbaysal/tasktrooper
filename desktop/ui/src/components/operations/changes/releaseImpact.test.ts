import { describe, expect, it } from "vitest";
import type { Release, ReleaseImpactCheck, ReleaseImpactSignal } from "@/api";
import {
  impactDeltaText,
  impactState,
  impactSummary,
  impactThresholdText,
  impactWindow,
  listImpact,
  worstImpactSignal,
} from "@/components/operations/changes/releaseImpact";
import {
  attributedRelease,
  checksFailing,
  incidentRollbackRelease,
  rollbackSuggestion,
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

  it("picks the worst verdict, then the larger move", () => {
    const signals = check().signals.map((s) => ({ ...s, breached: false }));
    expect(worstImpactSignal(signals)?.signal).toBe("http.error_ratio");
    expect(worstImpactSignal([{ ...signals[0], verdict: "unknown" }])).toBeUndefined();
  });

  it("reads the charts an hour either side of the deploy", () => {
    const at = Date.parse("2026-10-15T10:00:00Z");
    expect(impactWindow("2026-10-15T10:00:00Z", 60)).toEqual({ from: at - 3_600_000, to: at + 3_600_000, at });
    expect(impactWindow(null, 60)).toBeUndefined();
  });
});

describe("rollback suggestion", () => {
  const now = Date.parse("2026-10-15T10:30:00Z");
  const off = { auto_rollback: false, mode: "on_merge", verify: { max_new_errors: 0 } } as Release["profile"];

  it("waits on a person for a failed release, or one awaiting a verdict on failing evidence", () => {
    const failed = release("r1", "r-shop", { status: "failed", failure_reason: "deploy failed", profile: off });
    expect(rollbackSuggestion(failed, [], now)).toEqual({ reason: "failed", detail: "deploy failed" });
    const verdict = release("r2", "r-shop", { status: "awaiting_verdict", profile: off, checks: { early_stop: "error rate +3.2 pt" } });
    expect(rollbackSuggestion(verdict, [], now)).toEqual({ reason: "verify", detail: "error rate +3.2 pt" });
    expect(rollbackSuggestion({ ...verdict, checks: {} }, [], now)).toBeNull();
    expect(rollbackSuggestion({ ...failed, profile: { ...off, auto_rollback: true } }, [], now)).toBeNull();
  });

  it("puts a production issue on the release that finished just before it, as the server does", () => {
    const shipped = release("r3", "r-shop", { status: "released", profile: off, finished_at: "2026-10-15T09:55:00Z" });
    const issue = incident("i1", "r-shop", "high", { first_seen_at: "2026-10-15T10:05:00Z" });
    expect(attributedRelease(issue, [shipped])?.id).toBe("r3");
    expect(rollbackSuggestion(shipped, [issue], now)).toEqual({ reason: "incident", incident: issue });
    expect(incidentRollbackRelease(issue, [shipped], now)?.id).toBe("r3");

    const late = incident("i2", "r-shop", "high", { first_seen_at: "2026-10-15T10:20:00Z" });
    expect(attributedRelease(late, [shipped])).toBeUndefined();
    const staging = incident("i3", "r-shop", "high", { env: "staging", first_seen_at: "2026-10-15T10:05:00Z" });
    expect(attributedRelease(staging, [shipped])).toBeUndefined();
    expect(incidentRollbackRelease({ ...issue, status: "resolved" }, [shipped], now)).toBeUndefined();
  });

  it("reads the soak's own failing evidence", () => {
    const base = release("r", "r-shop", { profile: off });
    expect(checksFailing({ ...base, checks: { smoke: [{ ok: false } as never] } })).toBe(true);
    expect(checksFailing({ ...base, checks: { new_errors: [{} as never] } })).toBe(true);
    expect(checksFailing({ ...base, checks: { impact: check() } })).toBe(true);
    expect(checksFailing({ ...base, checks: { health: [] } })).toBe(false);
  });
});
