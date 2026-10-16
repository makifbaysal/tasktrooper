import type { LogSeverity } from "@/api";
import { isLogSeverity } from "@/components/operations/logs/severity";
import { Badge, type BadgeProps } from "@/components/ui/badge";
import { useI18n } from "@/hooks/useI18n";
import { cn } from "@/lib/utils";

const PILL: Record<LogSeverity, { variant: NonNullable<BadgeProps["variant"]>; className?: string }> = {
  critical: { variant: "destructive" },
  error: { variant: "outline", className: "border-transparent bg-destructive/15 text-destructive" },
  warning: { variant: "warning" },
  info: { variant: "info" },
  debug: { variant: "secondary" },
};

export function SeverityPill({ severity, className }: { severity: string; className?: string }) {
  const { t } = useI18n();
  const known = isLogSeverity(severity) ? severity : "info";
  const { variant, className: tone } = PILL[known];
  return (
    <Badge
      variant={variant}
      className={cn("rounded-[4px] px-1.5 py-0 font-mono text-micro tracking-wide uppercase", tone, className)}
    >
      {t(`operations.opsQuery.severity.${known}`)}
    </Badge>
  );
}
