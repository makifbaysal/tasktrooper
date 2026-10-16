import {
  CircleCheck,
  CircleSlash,
  CircleX,
  Clock3,
  FileText,
  Hourglass,
  Rocket,
  RotateCcw,
  ScanSearch,
  Undo2,
  type LucideIcon,
} from "lucide-react";
import type { ReleaseStatus } from "@/api";
import { Badge, type BadgeProps } from "@/components/ui/badge";
import { useI18n } from "@/hooks/useI18n";
import { cn } from "@/lib/utils";

export const RELEASE_STATUS_META: Record<ReleaseStatus, { variant: NonNullable<BadgeProps["variant"]>; icon: LucideIcon }> = {
  draft: { variant: "secondary", icon: FileText },
  pending: { variant: "secondary", icon: Clock3 },
  deploying: { variant: "info", icon: Rocket },
  verifying: { variant: "info", icon: ScanSearch },
  awaiting_verdict: { variant: "warning", icon: Hourglass },
  rolling_back: { variant: "warning", icon: Undo2 },
  released: { variant: "success", icon: CircleCheck },
  rolled_back: { variant: "warning", icon: RotateCcw },
  failed: { variant: "destructive", icon: CircleX },
  superseded: { variant: "secondary", icon: CircleSlash },
};

/** A release's status in words with its glyph, never colour alone. */
export function ReleaseStatusPill({ status, className }: { status: ReleaseStatus; className?: string }) {
  const { t } = useI18n();
  const { variant, icon: Icon } = RELEASE_STATUS_META[status];
  return (
    <Badge variant={variant} className={cn("gap-1 px-2 py-0 text-micro whitespace-nowrap", className)}>
      <Icon className="h-3 w-3 shrink-0" aria-hidden />
      {t(`release.statuses.${status}`)}
    </Badge>
  );
}
