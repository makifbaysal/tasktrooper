import { CircleCheck, CircleHelp, Hourglass, Minus, TriangleAlert, type LucideIcon } from "lucide-react";
import type { ImpactState } from "@/components/operations/changes/releaseImpact";
import { Badge, type BadgeProps } from "@/components/ui/badge";
import { useI18n } from "@/hooks/useI18n";
import { cn } from "@/lib/utils";

export const IMPACT_STATE_META: Record<ImpactState, { variant: NonNullable<BadgeProps["variant"]>; icon: LucideIcon; className?: string }> = {
  worse: { variant: "outline", icon: TriangleAlert, className: "border-transparent bg-destructive/15 text-destructive" },
  pending: { variant: "info", icon: Hourglass },
  neutral: { variant: "secondary", icon: Minus },
  improved: { variant: "success", icon: CircleCheck },
  unknown: { variant: "outline", icon: CircleHelp, className: "border-dashed text-muted-foreground" },
};

/** A deploy's impact verdict in words with its glyph, and the move that
 * decided it ("error rate +3.2 pt") beside it when there is one. */
export function ImpactVerdictPill({ state, detail, className }: { state: ImpactState; detail?: string; className?: string }) {
  const { t } = useI18n();
  const meta = IMPACT_STATE_META[state];
  const Icon = meta.icon;
  return (
    <Badge variant={meta.variant} className={cn("gap-1 rounded-md px-1.5 py-0 text-micro whitespace-nowrap", meta.className, className)}>
      <Icon className="h-3 w-3 shrink-0" aria-hidden />
      {t(`operations.changes.impactVerdict.states.${state}`)}
      {detail && <span className="font-mono font-normal tabular-nums">· {detail}</span>}
    </Badge>
  );
}
