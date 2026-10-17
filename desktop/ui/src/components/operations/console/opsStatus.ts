import type { CloudResourceStatus, DeployEnvironment, Incident, IncidentStatus } from "@/api";
import { environmentFromLabel } from "@/lib/environmentLabel";

export type OpsStatus = "critical" | "warning" | "healthy" | "notReporting";

export const OPS_STATUSES: OpsStatus[] = ["critical", "warning", "healthy", "notReporting"];

export const OPEN_INCIDENT_STATUSES: IncidentStatus[] = ["open", "triaging", "proposed", "fixing"];

export function isOpenIncident(incident: Pick<Incident, "status">): boolean {
  return OPEN_INCIDENT_STATUSES.includes(incident.status);
}

export function isSevereIncident(incident: Pick<Incident, "severity">): boolean {
  return incident.severity === "critical" || incident.severity === "high";
}

export function incidentEnvironment(env: string | undefined): DeployEnvironment | undefined {
  return environmentFromLabel(env);
}

// The one place a resource's stored health becomes a severity, so the health
// square, the status pill and the Health tile never disagree about "failed".
export function healthSeverity(health: CloudResourceStatus | undefined): "critical" | "warning" | undefined {
  if (health === "failed") return "critical";
  if (health === "degraded") return "warning";
  return undefined;
}

export type OpsReasonKey =
  | "incidentCritical"
  | "resourceFailed"
  | "incidentWarning"
  | "resourceDegraded"
  | "errors"
  | "healthy"
  | "notConnected"
  | "noHealth";

export interface OpsHealth {
  status: OpsStatus;
  reason: OpsReasonKey;
  count?: number;
}

export interface OpsHealthInput {
  connected: boolean;
  health?: CloudResourceStatus;
  incidents: Pick<Incident, "severity" | "status">[];
  errors24h?: number;
}

export function deriveOpsHealth({ connected, health, incidents, errors24h = 0 }: OpsHealthInput): OpsHealth {
  const open = incidents.filter(isOpenIncident);
  const severe = open.filter(isSevereIncident).length;
  if (severe > 0) return { status: "critical", reason: "incidentCritical", count: severe };
  const severity = connected ? healthSeverity(health) : undefined;
  if (severity === "critical") return { status: "critical", reason: "resourceFailed" };
  if (open.length > 0) return { status: "warning", reason: "incidentWarning", count: open.length };
  if (severity === "warning") return { status: "warning", reason: "resourceDegraded" };
  if (connected && errors24h > 0) return { status: "warning", reason: "errors", count: errors24h };
  if (connected && (health === "healthy" || health === "deploying")) return { status: "healthy", reason: "healthy" };
  return { status: "notReporting", reason: connected ? "noHealth" : "notConnected" };
}

export function compareOpsStatus(a: OpsStatus, b: OpsStatus): number {
  return OPS_STATUSES.indexOf(a) - OPS_STATUSES.indexOf(b);
}
