import { describe, expect, it } from "vitest";
import { PLAY_LAG_MS, boundSourceKinds, vitalsLagLikely, vitalsWindow } from "@/components/operations/releases/vitalsWindow";

const NOW = Date.parse("2026-10-17T10:42:00Z");

describe("mobile vitals window", () => {
  it("reads the last 7 days, ending on the hour", () => {
    const w = vitalsWindow(NOW);
    expect(w.to).toBe(Date.parse("2026-10-17T10:00:00Z"));
    expect(w.to - w.from).toBe(7 * 86_400_000);
    expect(w.preset).toBe("7d");
  });

  it("expects Play's daily vitals — and only Play's — to be late in a window ending within its delay", () => {
    const play = "google_play_vitals";
    expect(vitalsLagLikely("mobile.crash_free_users", { to: NOW }, play, NOW)).toBe(true);
    expect(vitalsLagLikely("mobile.anr_rate", { to: NOW - PLAY_LAG_MS + 1 }, play, NOW)).toBe(true);
    expect(vitalsLagLikely("mobile.crash_rate", { to: NOW - PLAY_LAG_MS }, play, NOW)).toBe(false);
    expect(vitalsLagLikely("mobile.cold_start", { to: NOW }, play, NOW)).toBe(false);
    expect(vitalsLagLikely("mobile.crash_free_users", { to: NOW }, "sentry", NOW)).toBe(false);
    expect(vitalsLagLikely("mobile.crash_free_users", { to: NOW }, undefined, NOW)).toBe(false);
  });
});

describe("boundSourceKinds", () => {
  it("maps each bound signal to its source's kind", () => {
    const kinds = boundSourceKinds({
      component_id: "c",
      role: "mobile",
      environment: "production",
      signals: [
        { signal: "mobile.crash_free_users", kind: "metric", unit: "%", golden: true, status: "bound", binding: { data_source_kind: "google_play_vitals" } as never, suggestions: [] },
        { signal: "mobile.anr_rate", kind: "metric", unit: "%", golden: true, status: "unbound", binding: null, suggestions: [] },
      ],
    });
    expect([...kinds]).toEqual([["mobile.crash_free_users", "google_play_vitals"]]);
    expect(boundSourceKinds(null).size).toBe(0);
  });
});
