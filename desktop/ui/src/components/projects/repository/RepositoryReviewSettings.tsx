import { useState } from "react";
import { toast } from "sonner";
import { api, type RepositoryModel } from "@/api";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { HelpTooltip } from "@/components/ui/help-tooltip";
import { Label } from "@/components/ui/label";
import { Notice } from "@/components/ui/notice";
import { Switch } from "@/components/ui/switch";
import { useI18n } from "@/hooks/useI18n";

interface RepositoryReviewSettingsProps {
  model: RepositoryModel;
  repositoryId: string;
  onReload: () => void;
}

export function RepositoryReviewSettings({ model, repositoryId, onReload }: RepositoryReviewSettingsProps) {
  const { t } = useI18n();
  const repository = model.repository;
  const [creatingSetupTask, setCreatingSetupTask] = useState(false);

  const handleReviewChange = async (checked: boolean) => {
    try {
      await api.updateRepository(repositoryId, {
        name: repository.name,
        description: repository.description,
        require_human_review: checked,
      });
      onReload();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("common.saveFailed"));
    }
  };

  const handleCreateSetupTask = async () => {
    setCreatingSetupTask(true);
    try {
      await api.createWorkflowSetupTask(repositoryId);
      toast.success(t("repositoryPage.components.openSetupTask"));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("common.actionFailed"));
    } finally {
      setCreatingSetupTask(false);
    }
  };

  const hasCIChecks = model.checks.some((c) => c.source === "ci");

  return (
    <>
      <Card className="space-y-4 p-6">
        <div>
          <h2 className="font-semibold">{t("repositoryPage.components.reviewTitle")}</h2>
          <p className="text-caption text-muted-foreground">{t("repositoryPage.components.reviewScope")}</p>
        </div>
        <div className="flex items-center justify-between gap-4 rounded-md border border-border p-3">
          <div className="flex items-center gap-1.5">
            <Label>{t("repositoryPage.components.requireHumanReview")}</Label>
            <HelpTooltip text={t("repositoryPage.components.requireHumanReviewHelp")} />
          </div>
          <Switch checked={repository.require_human_review ?? false} onCheckedChange={handleReviewChange} />
        </div>
      </Card>

      {!hasCIChecks && (
        <Notice variant="warning" title={t("repositoryPage.components.noWorkflowsTitle")}>
          <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
            <span>{t("repositoryPage.components.noWorkflowsDesc")}</span>
            <Button size="sm" onClick={handleCreateSetupTask} disabled={creatingSetupTask} className="shrink-0">
              {t("repositoryPage.components.openSetupTask")}
            </Button>
          </div>
        </Notice>
      )}
    </>
  );
}
