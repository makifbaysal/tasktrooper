import { Badge, type BadgeProps } from "@/components/ui/badge";
import { useI18n } from "@/hooks/useI18n";

const KNOWN_SEVERITIES = ["critical", "warning", "info"];

export function SeverityBadge({ severity }: { severity: string }) {
  const { t } = useI18n();
  const variant: BadgeProps["variant"] = severity === "critical" ? "destructive" : severity === "warning" ? "warning" : "info";
  return <Badge variant={variant}>{KNOWN_SEVERITIES.includes(severity) ? t(`telemetryHub.severity.${severity}`) : severity}</Badge>;
}

const STATE_VARIANT: Record<string, BadgeProps["variant"]> = {
  firing: "destructive",
  open: "destructive",
  pending: "warning",
  acknowledged: "info",
  resolved: "success",
  ignored: "secondary",
  ok: "success",
  muted: "secondary",
  paused: "secondary",
  nodata: "secondary",
};

export function StateBadge({ state }: { state: string }) {
  const { t } = useI18n();
  const known = state in STATE_VARIANT;
  return <Badge variant={STATE_VARIANT[state] ?? "outline"}>{known ? t(`telemetryHub.states.${state}`) : state}</Badge>;
}
