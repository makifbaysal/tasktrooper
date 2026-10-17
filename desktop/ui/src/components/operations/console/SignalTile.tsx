import { ArrowDownRight, ArrowRight, ArrowUpRight, OctagonAlert, TriangleAlert } from "lucide-react";
import type { ReactNode } from "react";
import { Sparkline } from "@/components/operations/charts/Sparkline";
import { StatTile } from "@/components/ui/stat-tile";
import { useI18n } from "@/hooks/useI18n";
import { cn } from "@/lib/utils";

export type SignalDeltaTone = "good" | "bad" | "flat";

export interface SignalDelta {
  text: string;
  tone: SignalDeltaTone;
  direction?: "up" | "down" | "flat";
  label?: string;
}

export interface SignalTileProps {
  label: string;
  value: ReactNode;
  unit?: string;
  title?: string;
  delta?: SignalDelta;
  sparkline?: Array<[number, number | null]>;
  sparklineColorIndex?: number;
  state?: "alarm" | "warn";
  foot?: ReactNode;
  compact?: boolean;
  className?: string;
}

const DELTA_TONE: Record<SignalDeltaTone, string> = {
  good: "text-success",
  bad: "text-destructive",
  flat: "text-muted-foreground",
};

const DELTA_ICON = { up: ArrowUpRight, down: ArrowDownRight, flat: ArrowRight };

export function SignalTile({
  label,
  value,
  unit,
  title,
  delta,
  sparkline,
  sparklineColorIndex,
  state,
  foot,
  compact = false,
  className,
}: SignalTileProps) {
  const { t } = useI18n();
  const StateIcon = state === "alarm" ? OctagonAlert : TriangleAlert;
  const DeltaIcon = DELTA_ICON[delta?.direction ?? "flat"];
  return (
    <StatTile
      label={label}
      title={title}
      className={cn(
        "min-w-0",
        state === "alarm" && "border-l-4 border-l-destructive",
        state === "warn" && "border-l-4 border-l-warning",
        className,
      )}
      value={
        <span className="flex min-w-0 items-baseline gap-1.5">
          {state && (
            <span className="self-center">
              <StateIcon
                className={cn("h-4 w-4", state === "alarm" ? "text-destructive" : "text-warning")}
                aria-hidden
              />
              <span className="sr-only">{t(state === "alarm" ? "operations.status.critical" : "operations.status.warning")}</span>
            </span>
          )}
          <span className={cn("min-w-0 truncate", compact && "text-title")}>{value}</span>
          {unit && <span className="shrink-0 text-body font-normal text-muted-foreground">{unit}</span>}
        </span>
      }
      foot={foot}
    >
      {(delta || sparkline) && (
        <div className="mt-2 flex items-center justify-between gap-3">
          {delta ? (
            <span className={cn("inline-flex items-center gap-1 text-caption font-medium", DELTA_TONE[delta.tone])}>
              <DeltaIcon className="h-3.5 w-3.5" aria-hidden />
              {delta.text}
              {delta.tone !== "flat" && (
                <span className="sr-only">{` ${t(`operations.signalDelta.${delta.tone === "good" ? "better" : "worse"}`)} `}</span>
              )}
              {delta.label && <span className="font-normal text-muted-foreground">{delta.label}</span>}
            </span>
          ) : (
            <span />
          )}
          {sparkline && <Sparkline points={sparkline} colorIndex={sparklineColorIndex} />}
        </div>
      )}
    </StatTile>
  );
}
