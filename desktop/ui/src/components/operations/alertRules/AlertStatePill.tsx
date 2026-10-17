import { BellOff, BellRing, CircleCheck, CircleDashed, Hourglass, type LucideIcon } from "lucide-react";
import type { AlertRuleState } from "@/api";
import { Badge, type BadgeProps } from "@/components/ui/badge";
import { useI18n } from "@/hooks/useI18n";
import { cn } from "@/lib/utils";

export const ALERT_STATE_META: Record<AlertRuleState, { variant: NonNullable<BadgeProps["variant"]>; icon: LucideIcon; className?: string }> = {
  firing: { variant: "outline", icon: BellRing, className: "border-transparent bg-destructive/15 text-destructive" },
  pending: { variant: "warning", icon: Hourglass },
  no_data: { variant: "outline", icon: CircleDashed, className: "border-dashed text-muted-foreground" },
  ok: { variant: "success", icon: CircleCheck },
};

/** A rule's state in words with its glyph; a rule switched off says so instead. */
export function AlertStatePill({ state, disabled = false, className }: { state: AlertRuleState; disabled?: boolean; className?: string }) {
  const { t } = useI18n();
  const meta = ALERT_STATE_META[state] ?? ALERT_STATE_META.no_data;
  const Icon = disabled ? BellOff : meta.icon;
  return (
    <Badge
      variant={disabled ? "secondary" : meta.variant}
      className={cn("gap-1 px-2 py-0 text-micro whitespace-nowrap", !disabled && meta.className, className)}
    >
      <Icon className="h-3 w-3 shrink-0" aria-hidden />
      {disabled ? t("operations.alertRules.states.disabled") : t(`operations.alertRules.states.${state}`)}
    </Badge>
  );
}
