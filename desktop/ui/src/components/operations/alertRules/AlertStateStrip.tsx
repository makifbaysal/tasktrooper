import { ALERT_RULE_STATES, type AlertRuleState } from "@/api";
import type { AlertStateFilter } from "@/components/operations/alertRules/alertRules";
import { ALERT_STATE_META } from "@/components/operations/alertRules/AlertStatePill";
import { Button } from "@/components/ui/button";
import { useI18n } from "@/hooks/useI18n";
import { cn } from "@/lib/utils";

const TONE: Record<AlertRuleState, string> = {
  firing: "bg-destructive/15 text-destructive",
  pending: "bg-warning/15 text-warning",
  no_data: "border border-dashed border-border text-muted-foreground",
  ok: "bg-success/15 text-success",
};

interface AlertStateStripProps {
  counts: Record<AlertRuleState, number>;
  active: AlertStateFilter;
  onSelect: (state: AlertStateFilter) => void;
}

/** The rules' states as filters, each with its count: Firing, Pending, No data, OK. */
export function AlertStateStrip({ counts, active, onSelect }: AlertStateStripProps) {
  const { t } = useI18n();
  return (
    <div className="grid grid-cols-2 gap-2 @2xl:grid-cols-4" role="group" aria-label={t("operations.alertRules.stateFilter")}>
      {ALERT_RULE_STATES.map((state) => {
        const pressed = active === state;
        const Icon = ALERT_STATE_META[state].icon;
        return (
          <Button
            key={state}
            variant="outline"
            aria-pressed={pressed}
            onClick={() => onSelect(pressed ? "all" : state)}
            className={cn(
              "h-auto min-w-0 justify-start gap-3 bg-card px-3 py-2.5 text-left whitespace-normal shadow-[var(--shadow-raised)]",
              pressed && "border-ring bg-accent ring-1 ring-ring",
            )}
          >
            <span className={cn("flex h-7 w-7 shrink-0 items-center justify-center rounded-md", TONE[state])}>
              <Icon className="h-4 w-4" aria-hidden />
            </span>
            <span className="flex min-w-0 flex-col">
              <span className="text-caption leading-tight font-normal text-muted-foreground">{t(`operations.alertRules.states.${state}`)}</span>
              <span className="font-mono text-title font-semibold tabular-nums">{counts[state]}</span>
            </span>
          </Button>
        );
      })}
    </div>
  );
}
