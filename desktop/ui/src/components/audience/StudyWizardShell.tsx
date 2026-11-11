import { X } from "lucide-react";
import type { ReactNode } from "react";
import { Link } from "react-router-dom";
import { StudyStepper } from "@/components/audience/StudyStepper";
import { Button } from "@/components/ui/button";
import { useI18n } from "@/hooks/useI18n";
import type { WizardStep } from "@/lib/studyWizard";

interface StudyWizardShellProps {
  title: string;
  description?: string;
  current: WizardStep;
  reached: WizardStep;
  onSelectStep?: (step: WizardStep) => void;
  /** Buttons next to Exit, e.g. cancel or delete. */
  actions?: ReactNode;
  children: ReactNode;
}

/** The frame of a study: its title, the five steps and the way out. */
export function StudyWizardShell({ title, description, current, reached, onSelectStep, actions, children }: StudyWizardShellProps) {
  const { t } = useI18n();
  return (
    <div className="mx-auto w-full max-w-5xl space-y-6">
      <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
        <div className="min-w-0">
          <h1 className="text-display font-semibold tracking-tight">{title}</h1>
          {description && <p className="mt-1 text-body text-muted-foreground">{description}</p>}
        </div>
        <div className="flex shrink-0 items-center gap-2">
          {actions}
          <Button variant="outline" asChild>
            <Link to="/audience">
              <X />
              {t("audienceStudy.exit")}
            </Link>
          </Button>
        </div>
      </div>
      <StudyStepper current={current} reached={reached} onSelect={onSelectStep} />
      {children}
    </div>
  );
}
