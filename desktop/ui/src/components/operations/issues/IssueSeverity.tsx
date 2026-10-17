import { CircleAlert, Info, OctagonAlert, TriangleAlert, type LucideIcon } from "lucide-react";
import type { IncidentSeverity, IncidentStatus } from "@/api";
import { Badge, type BadgeProps } from "@/components/ui/badge";
import { useI18n } from "@/hooks/useI18n";
import { cn } from "@/lib/utils";

export const ISSUE_SEVERITY_META: Record<IncidentSeverity, { icon: LucideIcon; className: string }> = {
  critical: { icon: OctagonAlert, className: "text-destructive" },
  high: { icon: TriangleAlert, className: "text-destructive" },
  medium: { icon: CircleAlert, className: "text-warning" },
  low: { icon: Info, className: "text-muted-foreground" },
};

/** An incident's severity: its glyph in the severity's colour and the word. */
export function IssueSeverity({ severity, className }: { severity: IncidentSeverity; className?: string }) {
  const { t } = useI18n();
  const { icon: Icon, className: tone } = ISSUE_SEVERITY_META[severity];
  return (
    <span className={cn("inline-flex items-center gap-1.5 whitespace-nowrap", className)}>
      <Icon className={cn("h-3.5 w-3.5 shrink-0", tone)} aria-hidden />
      <span className={cn("font-medium", severity === "critical" && "text-destructive")}>{t(`operations.issue.severity.${severity}`)}</span>
    </span>
  );
}

const STATUS: Record<IncidentStatus, { variant: NonNullable<BadgeProps["variant"]>; className?: string }> = {
  open: { variant: "outline", className: "border-transparent bg-destructive/15 text-destructive" },
  triaging: { variant: "info" },
  proposed: { variant: "info" },
  fixing: { variant: "info" },
  resolved: { variant: "success" },
  ignored: { variant: "secondary" },
};

export function IssueStatusBadge({ status, className }: { status: IncidentStatus; className?: string }) {
  const { t } = useI18n();
  const meta = STATUS[status];
  return (
    <Badge variant={meta.variant} className={cn("px-2 py-0 text-micro whitespace-nowrap", meta.className, className)}>
      {t(`operations.issue.status.${status}`)}
    </Badge>
  );
}
