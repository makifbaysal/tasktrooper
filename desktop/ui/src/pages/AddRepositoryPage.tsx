import { X } from "lucide-react";
import { useLocation, useNavigate, useSearchParams } from "react-router-dom";
import { DoneStep } from "@/components/projects/add/DoneStep";
import { ReviewStep } from "@/components/projects/add/ReviewStep";
import { ScanStep } from "@/components/projects/add/ScanStep";
import { SourceStep } from "@/components/projects/add/SourceStep";
import { useAddRepositoryFlow } from "@/components/projects/add/useAddRepositoryFlow";
import { SetupWizardStepper, type WizardStep } from "@/components/projects/SetupWizardStepper";
import { Button } from "@/components/ui/button";
import { useI18n } from "@/hooks/useI18n";

/**
 * The full-page "Add repository" flow: Source → Scan → Review → Done.
 * Leaving mid-flow is fine — anything already imported/scanning keeps
 * running server-side; there is nothing here to save on unmount.
 */
export function AddRepositoryPage() {
  const { t } = useI18n();
  const navigate = useNavigate();
  const location = useLocation();
  const [searchParams] = useSearchParams();
  const initialProjectId = searchParams.get("project") ?? undefined;

  const flow = useAddRepositoryFlow();

  const handleClose = () => {
    if (location.key === "default") navigate("/projects");
    else navigate(-1);
  };

  const steps: WizardStep[] = [
    { key: "source", label: t("addRepository.steps.source") },
    { key: "scan", label: t("addRepository.steps.scan") },
    { key: "review", label: t("addRepository.steps.review") },
    { key: "done", label: t("addRepository.steps.done") },
  ];

  return (
    <div className="mx-auto flex w-full max-w-3xl flex-col gap-6">
      <div className="flex flex-col gap-4">
        <div className="flex items-center gap-3">
          <Button variant="ghost" size="icon" onClick={handleClose} aria-label={t("addRepository.close")}>
            <X className="h-4 w-4" />
          </Button>
          <h1 className="text-title font-semibold">{t("addRepository.title")}</h1>
        </div>
        <SetupWizardStepper
          steps={steps}
          current={flow.state.step}
          onSelect={flow.setStep}
          label={t("addRepository.stepperLabel")}
        />
      </div>

      {flow.state.step === 0 && <SourceStep initialProjectId={initialProjectId} onScan={flow.startScan} />}
      {flow.state.step === 1 && <ScanStep repos={flow.state.repos} onRetry={flow.retryImport} onContinue={() => flow.setStep(2)} />}
      {flow.state.step === 2 && <ReviewStep repos={flow.state.repos} onFinish={flow.finish} />}
      {flow.state.step === 3 && flow.state.doneStats && (
        <DoneStep
          repos={flow.state.repos}
          projectId={flow.state.projectId ?? ""}
          projectName={flow.state.projectName}
          stats={flow.state.doneStats}
        />
      )}
    </div>
  );
}
