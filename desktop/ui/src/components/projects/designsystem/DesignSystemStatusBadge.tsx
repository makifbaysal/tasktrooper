import type { DesignSystemStatus } from "@/api";
import { Badge, type BadgeProps } from "@/components/ui/badge";
import { useI18n } from "@/hooks/useI18n";

const STATUS_VARIANT: Record<DesignSystemStatus, NonNullable<BadgeProps["variant"]>> = {
  approved: "success",
  in_review: "warning",
  superseded: "secondary",
};

interface DesignSystemStatusBadgeProps {
  status: DesignSystemStatus;
  className?: string;
}

export function DesignSystemStatusBadge({ status, className }: DesignSystemStatusBadgeProps) {
  const { t } = useI18n();
  return (
    <Badge variant={STATUS_VARIANT[status] ?? "outline"} className={className}>
      {t(`designSystem.status.${status}`)}
    </Badge>
  );
}
