import { Check } from "lucide-react";
import { useI18n } from "@/hooks/useI18n";
import { stepIndex, WIZARD_STEPS, type WizardStep } from "@/lib/studyWizard";
import { cn } from "@/lib/utils";

interface StudyStepperProps {
  /** The step the person is looking at. */
  current: WizardStep;
  /** The furthest step the study has reached; every earlier step is done. */
  reached: WizardStep;
  /** Called with a finished step the person clicks. */
  onSelect?: (step: WizardStep) => void;
}

/** Context, Personas, Script, Interviews, Report: where the study is, with finished steps open to look at again. */
export function StudyStepper({ current, reached, onSelect }: StudyStepperProps) {
  const { t } = useI18n();
  return (
    <nav aria-label={t("audienceStudy.steps.label")}>
      <ol className="flex flex-wrap items-center gap-x-2 gap-y-2">
        {WIZARD_STEPS.map((step, i) => {
          const done = i < stepIndex(reached);
          const isCurrent = step === current;
          const clickable = done && !isCurrent && onSelect;
          const label = t(`audienceStudy.steps.${step}`);
          const body = (
            <>
              <span
                aria-hidden
                className={cn(
                  "flex h-6 w-6 shrink-0 items-center justify-center rounded-full border text-caption font-semibold",
                  done && "border-success bg-success/10 text-success",
                  isCurrent && !done && "border-primary bg-primary text-primary-foreground",
                  isCurrent && done && "border-primary bg-primary/10 text-primary",
                  !done && !isCurrent && "border-border text-muted-foreground",
                )}
              >
                {done ? <Check className="h-3.5 w-3.5" /> : i + 1}
              </span>
              <span className={cn("text-body", isCurrent ? "font-semibold" : done ? "" : "text-muted-foreground")}>{label}</span>
            </>
          );
          return (
            <li key={step} className="flex items-center gap-2" aria-current={isCurrent ? "step" : undefined}>
              {clickable ? (
                <button
                  type="button"
                  onClick={() => onSelect(step)}
                  className="flex items-center gap-2 rounded-md px-1 py-0.5 hover:bg-accent/40 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                >
                  {body}
                </button>
              ) : (
                <span className="flex items-center gap-2 px-1 py-0.5">{body}</span>
              )}
              {i < WIZARD_STEPS.length - 1 && <span aria-hidden className="h-px w-6 bg-border" />}
            </li>
          );
        })}
      </ol>
    </nav>
  );
}
