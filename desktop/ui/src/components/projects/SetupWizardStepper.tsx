import { Check } from "lucide-react";
import { Fragment } from "react";
import { cn } from "@/lib/utils";

export interface WizardStep {
  key: string;
  label: string;
}

interface SetupWizardStepperProps {
  steps: WizardStep[];
  current: number;
  /** Jumping is only offered backwards — a step ahead may not exist yet. */
  onSelect?: (index: number) => void;
  disabled?: boolean;
  label: string;
  className?: string;
}

/**
 * The wizard's progress header: numbered pills, one per step, scrolling
 * sideways rather than wrapping so a monorepo with a dozen sub-repos does not
 * push the form down the dialog every time a row is added.
 */
export function SetupWizardStepper({
  steps,
  current,
  onSelect,
  disabled,
  label,
  className,
}: SetupWizardStepperProps) {
  return (
    <nav aria-label={label} className={cn("flex items-center gap-1 overflow-x-auto pb-1", className)}>
      {steps.map((step, index) => {
        const done = index < current;
        const active = index === current;
        return (
          <Fragment key={step.key}>
            {index > 0 && <span aria-hidden className="h-px w-3 shrink-0 bg-border" />}
            <button
              type="button"
              disabled={disabled || index > current}
              aria-current={active ? "step" : undefined}
              onClick={() => onSelect?.(index)}
              className={cn(
                "flex shrink-0 items-center gap-1.5 rounded-full border px-2.5 py-1 text-xs transition-colors",
                "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
                active
                  ? "border-primary bg-primary/10 font-medium text-primary"
                  : done
                    ? "border-success/40 text-success hover:text-success"
                    : "border-dashed border-border text-muted-foreground/60",
                index > current && "cursor-default",
              )}
            >
              <span
                className={cn(
                  "flex h-4 w-4 shrink-0 items-center justify-center rounded-full text-[10px] leading-none",
                  active
                    ? "bg-primary text-primary-foreground"
                    : done
                      ? "bg-success/15 text-success"
                      : "bg-muted text-muted-foreground",
                )}
              >
                {done ? <Check className="h-2.5 w-2.5" /> : index + 1}
              </span>
              <span className="max-w-[8rem] truncate">{step.label}</span>
            </button>
          </Fragment>
        );
      })}
    </nav>
  );
}
