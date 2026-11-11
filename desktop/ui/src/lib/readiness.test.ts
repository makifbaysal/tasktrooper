import { describe, expect, it } from "vitest";
import type { ReadinessStep, RepositoryReadiness } from "@/api";
import type { ToolchainItem, ToolchainReport } from "@/lib/desktop-bridge";
import { destinationFor, openSteps, resolveReadiness, resolveToolchainStep, stackToolchainIds } from "@/lib/readiness";

function item(id: ToolchainItem["id"], status: ToolchainItem["status"], installKind: ToolchainItem["installKind"] = "auto"): ToolchainItem {
  return { id, label: id.toUpperCase(), status, installKind, dependsOn: [] };
}

const report = (items: ToolchainItem[]): ToolchainReport => ({ generatedAt: 0, platform: "darwin", items });

const toolchain: ReadinessStep = {
  id: "toolchain",
  status: "unknown",
  action: "desktop",
  depends_on: ["scan"],
  facts: { languages: ["go", "typescript", "ruby"] },
};

function readiness(steps: ReadinessStep[]): RepositoryReadiness {
  return {
    repository_id: "r1",
    steps,
    done: 0,
    skipped: 0,
    total: steps.length,
    complete: false,
    dismissed: false,
    run_all: { tasks: 0, runs: 0, steps: [] },
  };
}

describe("stackToolchainIds", () => {
  it("maps the languages the shell can install and ignores the rest", () => {
    expect(stackToolchainIds(["Go", "TypeScript", "javascript", "ruby", "Kotlin"]).sort()).toEqual(["go", "jdk", "node"]);
  });
});

describe("resolveToolchainStep", () => {
  it("stays unknown without a bridge, and without a report yet", () => {
    expect(resolveToolchainStep(toolchain, null, false).step?.reason).toBe("no_bridge");
    expect(resolveToolchainStep(toolchain, null, true).step?.status).toBe("unknown");
  });

  it("is done when every tool of the stack is installed", () => {
    const { step } = resolveToolchainStep(toolchain, report([item("go", "installed"), item("node", "installed")]), true);
    expect(step?.status).toBe("done");
  });

  it("is todo with the missing tools, naming which the shell can install", () => {
    const { step, resolution } = resolveToolchainStep(
      toolchain,
      report([item("go", "missing"), item("node", "missing", "manual")]),
      true,
    );
    expect(step?.status).toBe("todo");
    expect(step?.facts?.tools).toEqual(["GO", "NODE"]);
    expect(resolution.installable).toEqual(["go"]);
  });

  it("is dropped when the stack needs nothing the shell manages", () => {
    const { step } = resolveToolchainStep({ ...toolchain, facts: { languages: ["ruby"] } }, report([item("go", "installed")]), true);
    expect(step).toBeNull();
  });

  it("leaves a step the server already settled alone", () => {
    const blocked = { ...toolchain, status: "todo" as const };
    expect(resolveToolchainStep(blocked, null, false).step).toBe(blocked);
  });
});

describe("resolveReadiness", () => {
  it("recounts progress after the toolchain step is settled", () => {
    const scan: ReadinessStep = { id: "scan", status: "done", action: "run", depends_on: [] };
    const guide: ReadinessStep = { id: "guide", status: "skipped", action: "task", depends_on: ["scan"] };
    const resolved = resolveReadiness(readiness([scan, toolchain, guide]), report([item("go", "installed"), item("node", "installed")]), true);
    expect(resolved).toMatchObject({ done: 2, skipped: 1, total: 3, complete: true });
  });

  it("is not complete while the toolchain step is unknown", () => {
    const scan: ReadinessStep = { id: "scan", status: "done", action: "run", depends_on: [] };
    expect(resolveReadiness(readiness([scan, toolchain]), null, true).complete).toBe(false);
  });
});

describe("destinationFor / openSteps", () => {
  it("sends each navigate step to the place that asks for what it needs", () => {
    const base = { status: "todo" as const, action: "navigate" as const, depends_on: [] };
    expect(destinationFor({ ...base, id: "delivery" })).toEqual({ kind: "tab", tab: "deploy" });
    expect(destinationFor({ ...base, id: "findings" })).toEqual({ kind: "tab", tab: "findings" });
    expect(destinationFor({ ...base, id: "observability", facts: { component_id: "c1" } })).toEqual({
      kind: "path",
      path: "/operations/entity/c1/summary",
    });
    expect(destinationFor({ ...base, id: "observability" })).toBeNull();
  });

  it("lists the steps still to do", () => {
    const steps: ReadinessStep[] = [
      { id: "scan", status: "done", action: "run", depends_on: [] },
      { id: "guide", status: "skipped", action: "task", depends_on: [] },
      { id: "tests", status: "running", action: "task", depends_on: [] },
    ];
    expect(openSteps(steps).map((s) => s.id)).toEqual(["tests"]);
  });
});
