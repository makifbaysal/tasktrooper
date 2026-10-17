import {
  BadgeCheck,
  CircleCheck,
  CircleDashed,
  CircleHelp,
  CirclePause,
  Hourglass,
  Loader2,
  OctagonAlert,
  PencilLine,
  TrendingUp,
  TriangleAlert,
  type LucideIcon,
} from "lucide-react";
import type { ReactNode } from "react";
import type { TrackStatus } from "@/api";
import { trackStatusBadgeVariant, trackStatusLabelKey } from "@/components/operations/StoreReleaseControls";
import { lifecycleLabelKey, lifecycleVariant, reviewLabelKey, reviewVariant } from "@/components/operations/releases/mobileReleases";
import { isTestBuildActive, testBuildStatusBadgeVariant, testBuildStatusLabelKey } from "@/components/operations/storeTestBuilds";
import { Badge, type BadgeProps } from "@/components/ui/badge";
import { useI18n } from "@/hooks/useI18n";
import { cn } from "@/lib/utils";

function Pill({ variant, icon: Icon, spin, children, className }: { variant: BadgeProps["variant"]; icon: LucideIcon; spin?: boolean; children: ReactNode; className?: string }) {
  return (
    <Badge variant={variant} className={cn("gap-1 px-2 py-0 whitespace-nowrap", className)}>
      <Icon className={cn("h-3 w-3 shrink-0", spin && "animate-spin")} aria-hidden />
      {children}
    </Badge>
  );
}

const TRACK_ICON: Record<TrackStatus, LucideIcon> = {
  none: CircleDashed,
  draft: PencilLine,
  in_review: Hourglass,
  approved: BadgeCheck,
  rolling_out: TrendingUp,
  halted: CirclePause,
  live: CircleCheck,
};

export function TrackStatusPill({ status, className }: { status?: TrackStatus; className?: string }) {
  const { t } = useI18n();
  const icon = (status && TRACK_ICON[status]) || (status ? CircleHelp : CircleDashed);
  return (
    <Pill variant={trackStatusBadgeVariant(status)} icon={icon} className={className}>
      {t(trackStatusLabelKey(status))}
    </Pill>
  );
}

const LIFECYCLE_ICON: Record<string, LucideIcon> = {
  unregistered: CircleDashed,
  onboarding: PencilLine,
  test_ready: Hourglass,
  live: CircleCheck,
};

export function LifecyclePill({ state }: { state: string }) {
  const { t } = useI18n();
  return (
    <Pill variant={lifecycleVariant(state)} icon={LIFECYCLE_ICON[state] ?? CircleHelp}>
      {t(lifecycleLabelKey(state))}
    </Pill>
  );
}

const REVIEW_ICON: Record<string, LucideIcon> = {
  waiting_for_review: Hourglass,
  in_review: Hourglass,
  approved: CircleCheck,
  rejected: OctagonAlert,
};

export function ReviewPill({ state }: { state: string }) {
  const { t } = useI18n();
  return (
    <Pill variant={reviewVariant(state)} icon={REVIEW_ICON[state] ?? CircleHelp}>
      {t(reviewLabelKey(state), { state })}
    </Pill>
  );
}

const BUILD_ICON: Record<string, LucideIcon> = {
  ready: CircleCheck,
  failed: OctagonAlert,
  action_required: TriangleAlert,
  dispatched: TrendingUp,
};

export function TestBuildPill({ status }: { status: string }) {
  const { t } = useI18n();
  const moving = isTestBuildActive(status);
  return (
    <Pill variant={testBuildStatusBadgeVariant(status)} icon={moving ? Loader2 : (BUILD_ICON[status] ?? CircleHelp)} spin={moving}>
      {t(testBuildStatusLabelKey(status))}
    </Pill>
  );
}
