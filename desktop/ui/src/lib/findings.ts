import type { ComponentFinding, FindingSeverity, FindingStatus } from "@/api";
import type { BadgeProps } from "@/components/ui/badge";

export const FINDING_STATUSES: FindingStatus[] = ["open", "accepted", "false_positive", "resolved"];

const SEVERITY_RANK: Record<FindingSeverity, number> = { high: 0, medium: 1, low: 2 };

export function severityVariant(severity: FindingSeverity): BadgeProps["variant"] {
  if (severity === "high") return "destructive";
  if (severity === "medium") return "warning";
  return "secondary";
}

/** Worst first, then rule id, so a list reads the same after every reload. */
export function sortFindings(findings: ComponentFinding[]): ComponentFinding[] {
  return [...findings].sort(
    (a, b) => SEVERITY_RANK[a.severity] - SEVERITY_RANK[b.severity] || a.rule.localeCompare(b.rule),
  );
}

/** The component key a finding groups under; "" is repository-wide. */
export function findingComponentKey(finding: ComponentFinding): string {
  return finding.component_id ?? "";
}

export function openFindings(findings: ComponentFinding[], componentId?: string | null): ComponentFinding[] {
  return findings.filter(
    (f) => f.status === "open" && (componentId === undefined || componentId === null || f.component_id === componentId),
  );
}

export function hasHighOpen(findings: ComponentFinding[]): boolean {
  return findings.some((f) => f.status === "open" && f.severity === "high");
}
