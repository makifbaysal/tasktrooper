import { CircleCheck, CircleDashed, OctagonAlert, type LucideIcon } from "lucide-react";
import type { DataSourceCapability, DataSourceStatus } from "@/api";
import { capabilityLabel } from "@/components/operations/sources/dataSourceSchema";
import { Badge, type BadgeProps } from "@/components/ui/badge";
import { useI18n } from "@/hooks/useI18n";
import { cn } from "@/lib/utils";

const STATUS_META: Record<DataSourceStatus, { variant: NonNullable<BadgeProps["variant"]>; icon: LucideIcon }> = {
  ok: { variant: "success", icon: CircleCheck },
  error: { variant: "destructive", icon: OctagonAlert },
  unverified: { variant: "secondary", icon: CircleDashed },
};

export function DataSourceStatusBadge({ status, className }: { status: DataSourceStatus; className?: string }) {
  const { t } = useI18n();
  const { variant, icon: Icon } = STATUS_META[status] ?? STATUS_META.unverified;
  return (
    <Badge variant={variant} className={cn("gap-1 whitespace-nowrap", className)}>
      <Icon className="h-3 w-3 shrink-0" aria-hidden />
      {t(`operations.sources.status.${STATUS_META[status] ? status : "unverified"}`)}
    </Badge>
  );
}

export function CapabilityChips({ capabilities, className }: { capabilities: DataSourceCapability[]; className?: string }) {
  const { t } = useI18n();
  if (capabilities.length === 0) return null;
  return (
    <span className={cn("flex flex-wrap gap-1", className)}>
      {capabilities.map((c) => (
        <Badge key={c} variant="outline" className="font-normal">
          {capabilityLabel(t, c)}
        </Badge>
      ))}
    </span>
  );
}
