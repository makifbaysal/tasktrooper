// The repository setup checklist, pure parts: how the app finishes the step
// only this computer can judge (the developer tools for the stack), and the
// progress numbers the card shows. The server's checklist and its statuses
// are `api.getRepositoryReadiness`.

import type { ReadinessStep, ReadinessStepId, RepositoryReadiness } from "@/api";
import type { ToolchainId, ToolchainItem, ToolchainReport } from "@/lib/desktop-bridge";

/** Language names the scan reports (lowercased by the server) → the shell's tools. */
const LANGUAGE_TOOLS: Readonly<Record<string, readonly ToolchainId[]>> = {
  go: ["go"],
  golang: ["go"],
  typescript: ["node"],
  javascript: ["node"],
  node: ["node"],
  "node.js": ["node"],
  java: ["jdk"],
  kotlin: ["jdk"],
  "c#": ["dotnet"],
  csharp: ["dotnet"],
  "f#": ["dotnet"],
  ".net": ["dotnet"],
  rust: ["rust"],
  python: ["python"],
  dart: ["flutter"],
  swift: ["xcode-clt"],
  "objective-c": ["xcode-clt"],
};

export function stackToolchainIds(languages: readonly string[]): ToolchainId[] {
  const ids = new Set<ToolchainId>();
  for (const language of languages) {
    for (const id of LANGUAGE_TOOLS[language.trim().toLowerCase()] ?? []) ids.add(id);
  }
  return [...ids];
}

export function stepLanguages(step: ReadinessStep): string[] {
  const raw = step.facts?.languages;
  return Array.isArray(raw) ? raw.filter((v): v is string => typeof v === "string") : [];
}

export interface ToolchainResolution {
  /** Tools of the stack this computer lacks, installable or not. */
  missing: ToolchainItem[];
  /** What `install` is called with: the missing ones the shell can install itself. */
  installable: ToolchainId[];
}

/**
 * The server cannot see this computer, so its `toolchain` step arrives
 * `unknown`. With the shell's report the app settles it: done when every tool
 * the stack calls for is installed, todo when one is missing, and dropped when
 * the stack needs nothing the shell manages. Without the report it stays
 * unknown.
 */
export function resolveToolchainStep(
  step: ReadinessStep,
  report: ToolchainReport | null,
  available: boolean,
): { step: ReadinessStep | null; resolution: ToolchainResolution } {
  const none: ToolchainResolution = { missing: [], installable: [] };
  if (step.status !== "unknown") return { step, resolution: none };
  if (!available) return { step: { ...step, reason: "no_bridge" }, resolution: none };
  if (!report) return { step, resolution: none };

  const wanted = stackToolchainIds(stepLanguages(step));
  const byId = new Map(report.items.map((item) => [item.id, item]));
  const relevant = wanted.map((id) => byId.get(id)).filter((item): item is ToolchainItem => item !== undefined);
  if (relevant.length === 0) return { step: null, resolution: none };

  const missing = relevant.filter((item) => item.status !== "installed");
  if (missing.length === 0) {
    return { step: { ...step, status: "done", reason: "installed" }, resolution: none };
  }
  return {
    step: {
      ...step,
      status: "todo",
      reason: "missing",
      facts: { ...step.facts, tools: missing.map((m) => m.label) },
    },
    resolution: { missing, installable: missing.filter((m) => m.installKind !== "manual").map((m) => m.id) },
  };
}

export interface ResolvedReadiness {
  steps: ReadinessStep[];
  done: number;
  skipped: number;
  total: number;
  complete: boolean;
  toolchain: ToolchainResolution;
}

/** The server's checklist with the toolchain step settled and the progress recounted. */
export function resolveReadiness(
  readiness: RepositoryReadiness,
  report: ToolchainReport | null,
  toolchainAvailable: boolean,
): ResolvedReadiness {
  let toolchain: ToolchainResolution = { missing: [], installable: [] };
  const steps: ReadinessStep[] = [];
  for (const step of readiness.steps) {
    if (step.id !== "toolchain") {
      steps.push(step);
      continue;
    }
    const resolved = resolveToolchainStep(step, report, toolchainAvailable);
    toolchain = resolved.resolution;
    if (resolved.step) steps.push(resolved.step);
  }
  const done = steps.filter((s) => s.status === "done").length;
  const skipped = steps.filter((s) => s.status === "skipped").length;
  return {
    steps,
    done,
    skipped,
    total: steps.length,
    complete: steps.length > 0 && done + skipped === steps.length,
    toolchain,
  };
}

/** Steps the person should look at first: not done, not skipped. */
export function openSteps(steps: readonly ReadinessStep[]): ReadinessStep[] {
  return steps.filter((s) => s.status !== "done" && s.status !== "skipped");
}

export type ReadinessDestination =
  | { kind: "tab"; tab: "deploy" | "findings" | "design"; componentId?: string }
  | { kind: "path"; path: string };

/** Where a `navigate` step sends the person. */
export function destinationFor(step: ReadinessStep): ReadinessDestination | null {
  const id: ReadinessStepId = step.id;
  switch (id) {
    case "delivery":
    case "store":
      return { kind: "tab", tab: "deploy" };
    case "findings":
      return { kind: "tab", tab: "findings" };
    case "design":
      return { kind: "tab", tab: "design" };
    case "observability": {
      const componentId = step.facts?.component_id;
      return typeof componentId === "string" ? { kind: "path", path: `/operations/entity/${componentId}/summary` } : null;
    }
    default:
      return null;
  }
}

export function factNumber(step: ReadinessStep, key: string): number {
  const v = step.facts?.[key];
  return typeof v === "number" ? v : 0;
}

export function factList(step: ReadinessStep, key: string): string[] {
  const v = step.facts?.[key];
  return Array.isArray(v) ? v.filter((x): x is string => typeof x === "string") : [];
}
