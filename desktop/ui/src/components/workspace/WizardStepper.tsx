import { Check } from "lucide-react";
import { useI18n } from "@/hooks/useI18n";
import { cn } from "@/lib/utils";

const STEPS = [
  { key: "info", titleKey: "chatArea.workspace.wizard.infoTitle", descriptionKey: "chatArea.workspace.wizard.infoDescription" },
  { key: "agents", titleKey: "chatArea.workspace.wizard.agentsTitle", descriptionKey: "chatArea.workspace.wizard.agentsDescription" },
  { key: "columns", titleKey: "chatArea.workspace.wizard.columnsTitle", descriptionKey: "chatArea.workspace.wizard.columnsDescription" },
  { key: "listeners", titleKey: "chatArea.workspace.wizard.listenersTitle", descriptionKey: "chatArea.workspace.wizard.listenersDescription" },
] as const;

interface WizardStepperProps {
  currentStep: number;
}

export function WizardStepper({ currentStep }: WizardStepperProps) {
  const { t } = useI18n();
  return (
    <nav aria-label={t("chatArea.workspace.wizard.ariaLabel")} className="mb-8">
      <ol className="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
        {STEPS.map((step, index) => {
          const done = index < currentStep;
          const active = index === currentStep;
          return (
            <li key={step.key} className="flex flex-1 items-start gap-3 sm:flex-col sm:items-center sm:text-center">
              <div
                className={cn(
                  "flex h-9 w-9 shrink-0 items-center justify-center rounded-full border-2 text-sm font-semibold transition-colors",
                  done && "border-success bg-success/15 text-success",
                  active && !done && "border-primary bg-primary/10 text-primary",
                  !done && !active && "border-muted-foreground/30 text-muted-foreground",
                )}
              >
                {done ? <Check className="h-4 w-4" /> : index + 1}
              </div>
              <div className="min-w-0 pt-0.5 sm:pt-2">
                <p className={cn("text-sm font-medium", active ? "text-foreground" : "text-muted-foreground")}>
                  {t(step.titleKey)}
                </p>
                <p className="hidden text-xs text-muted-foreground sm:block">{t(step.descriptionKey)}</p>
              </div>
            </li>
          );
        })}
      </ol>
    </nav>
  );
}

export { STEPS as WIZARD_STEPS };
