import { Check, Minus, X } from "lucide-react";
import type { OpsHealth, OpsStatus } from "@/components/operations/console/opsStatus";
import { useI18n } from "@/hooks/useI18n";
import { cn } from "@/lib/utils";

type HealthSquareSize = "sm" | "md" | "lg";

const SQUARE: Record<OpsStatus, string> = {
  critical: "bg-destructive text-destructive-foreground",
  warning: "bg-warning text-black/80",
  healthy: "bg-success text-white",
  notReporting: "border border-dashed border-muted-foreground/70 bg-transparent text-muted-foreground",
};

const SIZE: Record<HealthSquareSize, { box: string; icon: string; bang: string }> = {
  sm: { box: "h-3.5 w-3.5 rounded-[3px]", icon: "h-2.5 w-2.5", bang: "text-[10px]" },
  md: { box: "h-4 w-4 rounded-[4px]", icon: "h-3 w-3", bang: "text-[11px]" },
  lg: { box: "h-7 w-7 rounded-md", icon: "h-4 w-4", bang: "text-base" },
};

type Translate = (key: string, params?: Record<string, string | number>) => string;

export function healthReasonText(t: Translate, health: OpsHealth): string {
  return t(`operations.health.reasons.${health.reason}`, { count: health.count ?? 0 });
}

interface HealthSquareProps {
  status: OpsStatus;
  /** Why: shown as the tooltip and read after the label. */
  reason?: string;
  size?: HealthSquareSize;
  showLabel?: boolean;
  /** Inside a control that already says the status in words. */
  decorative?: boolean;
  className?: string;
}

/** The console's health mark: a coloured square that always carries a glyph
 * and a spoken label, never colour alone. */
export function HealthSquare({ status, reason, size = "md", showLabel = false, decorative = false, className }: HealthSquareProps) {
  const { t } = useI18n();
  const label = t(`operations.status.${status}`);
  const dims = SIZE[size];
  const Icon = status === "critical" ? X : status === "healthy" ? Check : status === "notReporting" ? Minus : null;
  const square = (
    <span aria-hidden className={cn("inline-flex shrink-0 items-center justify-center", dims.box, SQUARE[status])}>
      {Icon ? <Icon className={dims.icon} strokeWidth={3} /> : <span className={cn("font-bold leading-none", dims.bang)}>!</span>}
    </span>
  );
  if (decorative) return <span className={cn("inline-flex", className)}>{square}</span>;
  const spoken = reason ? `${label}: ${reason}` : label;
  return (
    <span className={cn("inline-flex min-w-0 items-center gap-1.5", className)} title={reason ? `${label} · ${reason}` : label}>
      {square}
      {showLabel ? (
        <span className="truncate">
          {label}
          {reason && <span className="sr-only">{`: ${reason}`}</span>}
        </span>
      ) : (
        <span className="sr-only">{spoken}</span>
      )}
    </span>
  );
}
