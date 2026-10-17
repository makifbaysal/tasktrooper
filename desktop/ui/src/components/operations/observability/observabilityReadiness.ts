import {
  OBSERVABILITY_GOALS,
  OBSERVABILITY_INSTRUMENTATION,
  type CloudProviderKind,
  type ComponentRole,
  type DeployEnvironment,
  type EnableObservabilityRequest,
  type ObservabilityGoal,
  type ObservabilityInstrumentation,
  type ObservabilityReadiness,
  type ObservabilityTarget,
  type ReadinessCode,
} from "@/api";

const READINESS_CODES: ReadinessCode[] = [
  "golden_unbound",
  "golden_partly_unbound",
  "no_tracing_sdk",
  "no_log_source",
  "no_rum",
  "no_crash_sdk",
  "queues_unbound",
];

export function isReadinessCode(code: string): code is ReadinessCode {
  return READINESS_CODES.some((c) => c === code);
}

// What "enable observability" can set up for each kind of component; a goal
// the readiness read suggests is offered whatever the role.
const ROLE_GOALS: Partial<Record<ComponentRole, ObservabilityGoal[]>> = {
  backend: ["metrics", "traces", "logs", "queues"],
  worker: ["metrics", "traces", "logs", "queues"],
  frontend: ["metrics", "rum", "traces", "logs"],
  mobile: ["metrics", "crash", "traces", "logs"],
  desktop: ["metrics", "crash", "traces", "logs"],
};

export function offeredGoals(role: ComponentRole, readiness?: ObservabilityReadiness | null): ObservabilityGoal[] {
  const offered = new Set<ObservabilityGoal>(ROLE_GOALS[role] ?? OBSERVABILITY_GOALS);
  for (const s of readiness?.suggestions ?? []) offered.add(s.goal);
  return OBSERVABILITY_GOALS.filter((g) => offered.has(g));
}

/** The goals the readiness read found a gap for, in the catalog's order. */
export function suggestedGoals(readiness?: ObservabilityReadiness | null): ObservabilityGoal[] {
  const wanted = new Set((readiness?.suggestions ?? []).map((s) => s.goal));
  return OBSERVABILITY_GOALS.filter((g) => wanted.has(g));
}

export interface SignalCounts {
  bound: number;
  total: number;
  goldenBound: number;
  goldenTotal: number;
}

export function signalCounts(readiness?: ObservabilityReadiness | null): SignalCounts {
  const out: SignalCounts = { bound: 0, total: 0, goldenBound: 0, goldenTotal: 0 };
  for (const s of readiness?.signals ?? []) {
    out.total += 1;
    if (s.status === "bound") out.bound += 1;
    if (s.golden) {
      out.goldenTotal += 1;
      if (s.status === "bound") out.goldenBound += 1;
    }
  }
  return out;
}

export function detectedInstrumentation(readiness?: ObservabilityReadiness | null): ObservabilityInstrumentation[] {
  const found = readiness?.instrumentation ?? {};
  return OBSERVABILITY_INSTRUMENTATION.filter((k) => found[k] === true);
}

/** The gaps worded, one per distinct code, unknown codes kept as sent. */
export function readinessGaps(readiness: ObservabilityReadiness | null | undefined, t: (key: string, params?: Record<string, string | number>) => string): string[] {
  const seen = new Set<string>();
  const out: string[] = [];
  for (const s of readiness?.suggestions ?? []) {
    if (seen.has(s.code)) continue;
    seen.add(s.code);
    out.push(isReadinessCode(s.code) ? t(`operations.observability.codes.${s.code}`) : t("operations.observability.codes.unknown", { code: s.code }));
  }
  return out;
}

/** The cloud a component already runs on has its own telemetry service; elsewhere a stack of its own. */
export function defaultTarget(provider: CloudProviderKind | "" | undefined): ObservabilityTarget {
  return provider === "gcp" || provider === "aws" ? "own_cloud" : "self_hosted";
}

export function isHttpUrl(raw: string): boolean {
  try {
    const url = new URL(raw.trim());
    return (url.protocol === "http:" || url.protocol === "https:") && url.host !== "";
  } catch {
    return false;
  }
}

export interface EnableDraft {
  goals: ObservabilityGoal[];
  target: ObservabilityTarget;
  otlpEndpoint: string;
}

export type EnableField = "goals" | "target" | "otlp_endpoint" | "environment";

/** The server's checks (opsai/enable.go). */
export function validateEnable(draft: EnableDraft): Partial<Record<EnableField, string>> {
  const errors: Partial<Record<EnableField, string>> = {};
  if (draft.goals.length === 0) errors.goals = "required";
  if (draft.target === "otlp_endpoint") {
    const url = draft.otlpEndpoint.trim();
    if (!url) errors.otlp_endpoint = "required";
    else if (url.length > 2048 || !isHttpUrl(url)) errors.otlp_endpoint = "invalid";
  }
  return errors;
}

export function enableRequest(draft: EnableDraft, environment: DeployEnvironment): EnableObservabilityRequest {
  const body: EnableObservabilityRequest = {
    environment,
    goals: OBSERVABILITY_GOALS.filter((g) => draft.goals.includes(g)),
    target: draft.target,
  };
  if (draft.target === "otlp_endpoint") body.otlp_endpoint = draft.otlpEndpoint.trim();
  return body;
}
