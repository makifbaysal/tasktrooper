import { useEffect, useId, useState } from "react";
import { toast } from "sonner";
import { api, type StoreTestBuild } from "@/api";
import { ActionSurface } from "@/components/operations/StoreTestBuildParts";
import { Button } from "@/components/ui/button";
import { useI18n } from "@/hooks/useI18n";

type Answer = "exempt" | "non_exempt";

interface ExportComplianceDialogProps {
  repositoryId: string;
  build: StoreTestBuild | null;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onAnswered: (build: StoreTestBuild) => void;
  inline?: boolean;
}

/**
 * The export compliance answer Apple holds a build for. It is the developer's
 * legal statement, so neither answer is preselected and nothing is sent until
 * one has been picked explicitly — every opening starts with no choice.
 */
export function ExportComplianceDialog({
  repositoryId,
  build,
  open,
  onOpenChange,
  onAnswered,
  inline = false,
}: ExportComplianceDialogProps) {
  const { t } = useI18n();
  const name = useId();
  const [answer, setAnswer] = useState<Answer | null>(null);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    setAnswer(null);
  }, [open, build?.id]);

  if (!build) return null;

  const submit = async () => {
    if (answer === null) return;
    setBusy(true);
    try {
      const updated = await api.answerStoreTestBuildCompliance(repositoryId, build.id, answer === "non_exempt");
      toast.success(t("operations.storeTest.complianceSaved"));
      onAnswered(updated);
      onOpenChange(false);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("common.actionFailed"));
    } finally {
      setBusy(false);
    }
  };

  const options: { value: Answer; label: string; hint: string }[] = [
    {
      value: "exempt",
      label: t("operations.storeTest.complianceExempt"),
      hint: t("operations.storeTest.complianceExemptHint"),
    },
    {
      value: "non_exempt",
      label: t("operations.storeTest.complianceNonExempt"),
      hint: t("operations.storeTest.complianceNonExemptHint"),
    },
  ];

  return (
    <ActionSurface
      inline={inline}
      open={open}
      onOpenChange={(next) => !busy && onOpenChange(next)}
      title={t("operations.storeTest.complianceDialogTitle", { build: build.build_number })}
      description={t("operations.storeTest.complianceDialogDescription")}
      footer={
        <>
          <Button variant="outline" size={inline ? "sm" : "default"} onClick={() => onOpenChange(false)} disabled={busy}>
            {t("common.cancel")}
          </Button>
          <Button size={inline ? "sm" : "default"} onClick={() => void submit()} disabled={busy || answer === null}>
            {busy ? t("common.saving") : t("operations.storeTest.complianceSubmit")}
          </Button>
        </>
      }
    >
      <fieldset className="rounded-lg border border-border">
        <legend className="sr-only">{t("operations.storeTest.complianceDialogDescription")}</legend>
        <div className="divide-y divide-border">
          {options.map((option) => (
            <label
              key={option.value}
              className="flex cursor-pointer items-start gap-3 px-3 py-2 transition-colors hover:bg-muted/50 has-[:checked]:bg-muted"
            >
              <input
                type="radio"
                name={name}
                value={option.value}
                className="mt-1 h-4 w-4 shrink-0 accent-primary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                checked={answer === option.value}
                disabled={busy}
                onChange={() => setAnswer(option.value)}
              />
              <span className="min-w-0 space-y-0.5">
                <span className="block text-sm font-medium">{option.label}</span>
                <span className="block text-xs text-muted-foreground">{option.hint}</span>
              </span>
            </label>
          ))}
        </div>
      </fieldset>
    </ActionSurface>
  );
}
