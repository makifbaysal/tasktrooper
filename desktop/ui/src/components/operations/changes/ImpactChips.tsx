import { Bug, CircleAlert, Clock3, FlaskConical, HeartPulse, Minus, OctagonAlert, type LucideIcon } from "lucide-react";
import type { ReleaseChecks, ReleaseStatus } from "@/api";
import { impactChips, type ImpactChip, type ImpactTone } from "@/components/operations/changes/changeFeed";
import { Badge, type BadgeProps } from "@/components/ui/badge";
import { useI18n } from "@/hooks/useI18n";
import { cn } from "@/lib/utils";

const TONE: Record<ImpactTone, { variant: NonNullable<BadgeProps["variant"]>; className?: string }> = {
  success: { variant: "success" },
  warning: { variant: "warning" },
  destructive: { variant: "outline", className: "border-transparent bg-destructive/15 text-destructive" },
  muted: { variant: "outline", className: "border-dashed text-muted-foreground" },
};

const ICON: Record<ImpactChip["kind"], LucideIcon> = {
  health: HeartPulse,
  smoke: FlaskConical,
  newErrors: Bug,
  earlyStop: OctagonAlert,
  gaps: CircleAlert,
  notChecked: Minus,
  checking: Clock3,
};

type Translate = (key: string, params?: Record<string, string | number>) => string;

function chipText(t: Translate, chip: ImpactChip): { text: string; title: string } {
  const i = (key: string, params?: Record<string, string | number>) => t(`operations.changes.impact.${key}`, params);
  switch (chip.kind) {
    case "health":
      return {
        text: i("health", { ok: chip.ok, total: chip.ok + chip.failed }),
        title: i("healthLabel", { ok: chip.ok, failed: chip.failed }),
      };
    case "smoke":
      return {
        text: i("smoke", { passed: chip.passed, total: chip.passed + chip.failed }),
        title: i("smokeLabel", { passed: chip.passed, failed: chip.failed }),
      };
    case "newErrors":
      return { text: i("newErrors", { count: chip.count }), title: i("newErrorsLabel", { count: chip.count }) };
    case "earlyStop":
      return { text: i("earlyStop"), title: chip.reason };
    case "gaps":
      return { text: i("gaps", { count: chip.notes.length }), title: i("gapsLabel", { notes: chip.notes.join("; ") }) };
    case "checking":
      return { text: i("checking"), title: i("checking") };
    default:
      return { text: i("notChecked"), title: i("notChecked") };
  }
}

/** What the release's own verification recorded, as compact chips. */
export function ImpactChips({ checks, status, className }: { checks?: ReleaseChecks; status: ReleaseStatus; className?: string }) {
  const { t } = useI18n();
  const chips = impactChips(checks, status);
  return (
    <ul className={cn("flex flex-wrap items-center gap-1", className)}>
      {chips.map((chip) => {
        const { text, title } = chipText(t, chip);
        const tone = TONE[chip.tone];
        const Icon = ICON[chip.kind];
        return (
          <li key={chip.kind}>
            <Badge
              variant={tone.variant}
              title={title}
              className={cn("gap-1 rounded-md px-1.5 py-0 font-mono text-micro tabular-nums whitespace-nowrap", tone.className)}
            >
              <Icon className="h-3 w-3 shrink-0" aria-hidden />
              {text}
              {title !== text && <span className="sr-only">{`: ${title}`}</span>}
            </Badge>
          </li>
        );
      })}
    </ul>
  );
}
