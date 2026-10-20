import { describe, expect, it } from "vitest";
import type { ObservabilityReadiness } from "@/api";
import {
  analyticsOff,
  defaultTarget,
  detectedInstrumentation,
  enableRequest,
  offeredGoals,
  readinessGaps,
  signalCounts,
  suggestedGoals,
  validateEnable,
} from "@/components/operations/observability/observabilityReadiness";
import { en } from "@/locales/en";

function lookup(key: string): string | undefined {
  let node: unknown = en;
  for (const part of key.split(".")) {
    if (!node || typeof node !== "object") return undefined;
    node = (node as Record<string, unknown>)[part];
  }
  return typeof node === "string" ? node : undefined;
}

const t = (key: string, params?: Record<string, string | number>) =>
  (lookup(key) ?? key).replace(/\{(\w+)\}/g, (m, name: string) => (params && name in params ? String(params[name]) : m));

const readiness: ObservabilityReadiness = {
  component_id: "c-api",
  environment: "production",
  role: "backend",
  provider: "gcp",
  signals: [
    { signal: "http.requests", status: "bound", golden: true },
    { signal: "http.latency", status: "unbound", golden: true },
    { signal: "infra.cpu", status: "bound", golden: false },
    { signal: "logs.lines", status: "suggested", golden: false },
  ],
  instrumentation: { otel: false, sentry: true, prometheus: true },
  suggestions: [
    { goal: "metrics", code: "golden_partly_unbound" },
    { goal: "traces", code: "no_tracing_sdk" },
    { goal: "traces", code: "no_tracing_sdk" },
    { goal: "logs", code: "something_new" },
  ],
};

describe("readiness wording", () => {
  it("words each gap once and keeps an unknown code as sent", () => {
    expect(readinessGaps(readiness, t)).toEqual([
      "Some of its golden signals aren't bound to a data source.",
      "No tracing SDK (OpenTelemetry, Datadog or New Relic) is in its code.",
      "Gap: something_new",
    ]);
    expect(readinessGaps({ ...readiness, suggestions: null }, t)).toEqual([]);
  });

  it("counts bound signals, golden apart", () => {
    expect(signalCounts(readiness)).toEqual({ bound: 2, total: 4, goldenBound: 1, goldenTotal: 2 });
  });

  it("lists the instrumentation found, in the catalog's order", () => {
    expect(detectedInstrumentation(readiness)).toEqual(["sentry", "prometheus"]);
  });
});

describe("goals", () => {
  it("preselects the goals a gap names, once each", () => {
    expect(suggestedGoals(readiness)).toEqual(["metrics", "traces", "logs"]);
    expect(suggestedGoals(null)).toEqual([]);
  });

  it("offers the role's goals plus any a gap names", () => {
    expect(offeredGoals("backend", null)).toEqual(["metrics", "traces", "logs", "queues"]);
    expect(offeredGoals("frontend", { ...readiness, suggestions: [{ goal: "crash", code: "no_crash_sdk" }] })).toEqual([
      "metrics",
      "traces",
      "logs",
      "rum",
      "crash",
    ]);
    expect(offeredGoals("library", null)).toHaveLength(6);
  });
});

describe("target and request", () => {
  it("defaults to the cloud's own telemetry on GCP or AWS, else a stack of its own", () => {
    expect(defaultTarget("gcp")).toBe("own_cloud");
    expect(defaultTarget("aws")).toBe("own_cloud");
    expect(defaultTarget("vercel")).toBe("self_hosted");
    expect(defaultTarget("")).toBe("self_hosted");
  });

  it("checks the goals and the OTLP endpoint like the server", () => {
    expect(validateEnable({ goals: [], target: "own_cloud", otlpEndpoint: "" })).toEqual({ goals: "required" });
    expect(validateEnable({ goals: ["traces"], target: "otlp_endpoint", otlpEndpoint: "" })).toEqual({ otlp_endpoint: "required" });
    expect(validateEnable({ goals: ["traces"], target: "otlp_endpoint", otlpEndpoint: "ftp://x" })).toEqual({ otlp_endpoint: "invalid" });
    expect(validateEnable({ goals: ["traces"], target: "otlp_endpoint", otlpEndpoint: "https://otel.example.com:4318" })).toEqual({});
  });

  it("sends the endpoint only with the OTLP target, goals in the catalog's order", () => {
    expect(enableRequest({ goals: ["logs", "metrics"], target: "own_cloud", otlpEndpoint: "https://ignored" }, "staging")).toEqual({
      environment: "staging",
      goals: ["metrics", "logs"],
      target: "own_cloud",
    });
    expect(enableRequest({ goals: ["traces"], target: "otlp_endpoint", otlpEndpoint: " https://otel:4318 " }, "production").otlp_endpoint).toBe(
      "https://otel:4318",
    );
  });
});

describe("analyticsOff", () => {
  const off = { web_analytics: "not_enabled", speed_insights: "enabled" } as const;

  it("names the feature each web signal reads", () => {
    expect(analyticsOff(off, "web.page_views")).toBe(true);
    expect(analyticsOff(off, "web.visitors")).toBe(true);
    expect(analyticsOff(off, "web.lcp")).toBe(false);
    expect(analyticsOff({ web_analytics: "enabled", speed_insights: "not_enabled" }, "web.cls")).toBe(true);
  });

  it("says nothing for a provider that does not say, an unknown state or another signal", () => {
    expect(analyticsOff(null, "web.page_views")).toBe(false);
    expect(analyticsOff(undefined, "web.lcp")).toBe(false);
    expect(analyticsOff({ web_analytics: "unknown", speed_insights: "unknown" }, "web.page_views")).toBe(false);
    expect(analyticsOff(off, "http.requests")).toBe(false);
  });

  it("words the gap for a project with analytics off", () => {
    const t = (key: string) => key;
    expect(readinessGaps({ suggestions: [{ goal: "rum", code: "analytics_not_enabled" }] } as never, t)).toEqual([
      "operations.observability.codes.analytics_not_enabled",
    ]);
  });
});
