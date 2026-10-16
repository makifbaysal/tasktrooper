import { describe, expect, it } from "vitest";
import type { AlertRule } from "@/api";
import {
  backtestRequest,
  conditionText,
  draftFromRule,
  draftFromSuggestion,
  durationParts,
  emptyRuleDraft,
  filterRules,
  fireBands,
  firingBands,
  mapServerFields,
  ruleInput,
  ruleThresholds,
  sortRules,
  stateCounts,
  validateRuleDraft,
} from "@/components/operations/alertRules/alertRules";
import { en } from "@/locales/en";

function lookup(key: string): string | undefined {
  let cur: unknown = en;
  for (const part of key.split(".")) cur = (cur as Record<string, unknown> | undefined)?.[part];
  return typeof cur === "string" ? cur : undefined;
}

const t = (key: string, params?: Record<string, string | number>) =>
  (lookup(key) ?? key).replace(/\{(\w+)\}/g, (_, k: string) => String(params?.[k] ?? `{${k}}`));
const ctx = { locale: "en-US", t };

function rule(overrides: Partial<AlertRule> = {}): AlertRule {
  return {
    id: "ar-1",
    name: "Slow responses",
    component_id: "c-api",
    environment: "production",
    signal: "http.latency",
    data_source_id: null,
    query: "",
    condition: { op: "gt", threshold: 800, for_seconds: 600, window_seconds: 300 },
    severity: "high",
    channels: ["desktop"],
    open_incident: true,
    enabled: true,
    state: "ok",
    state_since: "2026-10-15T10:00:00Z",
    last_value: null,
    last_evaluated_at: null,
    created_at: "2026-10-15T09:00:00Z",
    updated_at: "2026-10-15T09:00:00Z",
    ...overrides,
  };
}

describe("conditionText", () => {
  it("reads a level rule back in words, with its quantile and hold", () => {
    expect(conditionText(rule(), "ms", ctx)).toBe("Response time (p95) > 800 ms for 10 min");
    expect(conditionText(rule({ signal: "web.lcp", condition: { op: "gt", threshold: 2500, for_seconds: 0, window_seconds: 300 } }), "ms", ctx)).toBe(
      "Largest contentful paint (p75) > 2.5 s",
    );
    expect(
      conditionText(rule({ signal: "mobile.crash_free_users", condition: { op: "lt", threshold: 99.5, for_seconds: 1800, window_seconds: 900 } }), "%", ctx),
    ).toBe("Crash-free users < 99.5% for 30 min");
  });

  it("says a change rule's threshold in signed percent over its window", () => {
    const up = rule({ signal: "http.error_ratio", condition: { op: "change_pct_gt", threshold: 50, for_seconds: 0, window_seconds: 3600 } });
    expect(conditionText(up, "%", ctx)).toBe("Error rate change over 1 h > +50%");
    const down = rule({ signal: "http.requests", condition: { op: "change_pct_lt", threshold: -30, for_seconds: 120, window_seconds: 600 } });
    expect(conditionText(down, "req/s", ctx)).toBe("Throughput change over 10 min < −30% for 2 min");
  });

  it("covers rising, no data and a custom query", () => {
    const rising = rule({ signal: "queue.backlog", condition: { op: "rising", threshold: 1000, for_seconds: 600, window_seconds: 600 } });
    expect(conditionText(rising, "messages", ctx)).toBe("Backlog above 1,000 messages and rising over 10 min for 10 min");
    expect(conditionText({ ...rising, condition: { ...rising.condition, threshold: 0, for_seconds: 0 } }, "messages", ctx)).toBe("Backlog rising over 10 min");
    expect(conditionText(rule({ condition: { op: "no_data", threshold: 0, for_seconds: 900, window_seconds: 300 } }), "ms", ctx)).toBe(
      "No data from Response time (p95) for 15 min",
    );
    expect(conditionText(rule({ signal: "", query: "up{job='api'}", condition: { op: "lt", threshold: 1, for_seconds: 45, window_seconds: 60 } }), undefined, ctx)).toBe(
      "Custom query < 1 for 45 s",
    );
  });
});

describe("durations and thresholds", () => {
  it("keeps a span in its largest whole unit", () => {
    expect(durationParts(7200)).toEqual({ value: 2, unit: "hours" });
    expect(durationParts(300)).toEqual({ value: 5, unit: "minutes" });
    expect(durationParts(90)).toEqual({ value: 90, unit: "seconds" });
    expect(durationParts(0)).toEqual({ value: 0, unit: "seconds" });
  });

  it("draws a line only for a rule held to a level", () => {
    expect(ruleThresholds(rule(), "Threshold")).toEqual([{ value: 800, label: "Threshold" }]);
    expect(ruleThresholds(rule({ condition: { op: "change_pct_gt", threshold: 20, for_seconds: 0, window_seconds: 300 } }), "x")).toEqual([]);
    expect(ruleThresholds(rule({ condition: { op: "no_data", threshold: 0, for_seconds: 0, window_seconds: 300 } }), "x")).toEqual([]);
  });
});

describe("the editor's model", () => {
  it("asks for what the server needs before a round trip", () => {
    const draft = emptyRuleDraft("", "production");
    expect(validateRuleDraft(draft)).toEqual({ componentId: "required", signal: "required", threshold: "required" });
    expect(validateRuleDraft({ ...draft, componentId: "c", signal: "http.latency", threshold: "abc" })).toEqual({ threshold: "not_a_number" });
    expect(validateRuleDraft({ ...draft, componentId: "c", mode: "custom", threshold: "1" })).toEqual({ dataSourceId: "required", query: "required" });
    expect(validateRuleDraft({ ...draft, componentId: "c", signal: "s", op: "no_data", windowSeconds: 30 })).toEqual({ windowSeconds: "window_short" });
    expect(validateRuleDraft({ ...draft, componentId: "c", signal: "s", op: "no_data", forSeconds: 8 * 86400 })).toEqual({ forSeconds: "too_long" });
  });

  it("sends a bound signal or a source and its query, never both", () => {
    const signal = ruleInput({ ...draftFromRule(rule()), name: "" }, "Fallback");
    expect(signal).toEqual({
      name: "Fallback",
      component_id: "c-api",
      environment: "production",
      signal: "http.latency",
      condition: { op: "gt", threshold: 800, for_seconds: 600, window_seconds: 300 },
      severity: "high",
      channels: ["desktop"],
      open_incident: true,
      enabled: true,
    });
    const custom = ruleInput({ ...draftFromRule(rule()), mode: "custom", dataSourceId: "ds-1", query: " up ", channels: ["desktop", "slack"] }, "x");
    expect(custom.signal).toBe("");
    expect(custom.data_source_id).toBe("ds-1");
    expect(custom.query).toBe("up");
    expect(custom.channels).toEqual(["desktop"]);
    expect(backtestRequest(draftFromRule(rule()), 30, "x").days).toBe(14);
  });

  it("offers the channels that deliver on a suggestion that names none", () => {
    const { id: _id, state: _state, ...suggestion } = rule({ channels: [] });
    expect(draftFromSuggestion(suggestion).channels).toEqual(["desktop"]);
    expect(draftFromRule(rule({ channels: [] })).channels).toEqual([]);
  });

  it("files the server's field errors under the editor's fields", () => {
    expect(mapServerFields({ "condition.window_seconds": "invalid", signal: "required", limit: "invalid" })).toEqual({
      errors: { windowSeconds: "invalid", signal: "required" },
      rest: [["limit", "invalid"]],
    });
  });
});

describe("the list", () => {
  const rules = [
    rule({ id: "a", name: "B ok", state: "ok" }),
    rule({ id: "b", name: "A firing", state: "firing" }),
    rule({ id: "c", name: "Off firing", state: "firing", enabled: false }),
    rule({ id: "d", name: "Pending", state: "pending", signal: "http.error_ratio" }),
  ];

  it("counts, sorts the worst first and filters by state and text", () => {
    expect(stateCounts(rules)).toEqual({ firing: 2, pending: 1, no_data: 0, ok: 1 });
    expect(sortRules(rules).map((r) => r.id)).toEqual(["b", "d", "a", "c"]);
    expect(filterRules(rules, "firing", "").map((r) => r.id)).toEqual(["b", "c"]);
    expect(filterRules(rules, "all", "error_ratio").map((r) => r.id)).toEqual(["d"]);
    expect(filterRules(rules, "all", "checkout", (r) => [r.id === "a" ? "checkout-api" : undefined]).map((r) => r.id)).toEqual(["a"]);
  });

  it("shades a backtest's fires and a rule's firing spans", () => {
    const until = Date.parse("2026-10-15T12:00:00Z");
    expect(
      fireBands(
        [
          { start: "2026-10-15T11:00:00Z", end: null, peak: 3 },
          { start: "2026-10-15T09:00:00Z", end: "2026-10-15T09:30:00Z", peak: 5 },
        ],
        until,
      ),
    ).toEqual([
      { from: Date.parse("2026-10-15T09:00:00Z"), to: Date.parse("2026-10-15T09:30:00Z"), peak: 5, open: false },
      { from: Date.parse("2026-10-15T11:00:00Z"), to: until, peak: 3, open: true },
    ]);
    expect(
      firingBands(
        [
          { at: "2026-10-15T10:10:00Z", from_state: "pending", to_state: "firing" },
          { at: "2026-10-15T10:00:00Z", from_state: "ok", to_state: "pending" },
          { at: "2026-10-15T10:40:00Z", from_state: "firing", to_state: "ok" },
          { at: "2026-10-15T11:50:00Z", from_state: "ok", to_state: "firing" },
        ],
        until,
      ),
    ).toEqual([
      { from: Date.parse("2026-10-15T10:10:00Z"), to: Date.parse("2026-10-15T10:40:00Z"), open: false },
      { from: Date.parse("2026-10-15T11:50:00Z"), to: until, open: true },
    ]);
  });
});
