import { useCallback, useState } from "react";
import { toast } from "sonner";
import { api, type BoardTask, type RepositoryModel } from "@/api";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { HelpTooltip } from "@/components/ui/help-tooltip";
import { Label } from "@/components/ui/label";
import { Notice } from "@/components/ui/notice";
import { Switch } from "@/components/ui/switch";
import { useI18n } from "@/hooks/useI18n";
import { usePolling } from "@/hooks/usePolling";

// Picks up a setup task that was deleted or finished elsewhere, so the button
// frees itself without a reload.
const SETUP_TASK_POLL_MS = 10000;

interface RepositoryReviewSettingsProps {
  model: RepositoryModel;
  repositoryId: string;
  onReload: () => void;
}

export function RepositoryReviewSettings({ model, repositoryId, onReload }: RepositoryReviewSettingsProps) {
  const { t } = useI18n();
  const repository = model.repository;
  const [creatingSetupTask, setCreatingSetupTask] = useState(false);
  const [setupTask, setSetupTask] = useState<BoardTask | null>(null);

  const hasCIChecks = model.checks.some((c) => c.source === "ci");

  const loadSetupTask = useCallback(async () => {
    try {
      const res = await api.getWorkflowSetupTask(repositoryId);
      setSetupTask(res.task ?? null);
    } catch {
      // Keep the last answer: the server refuses a duplicate on its own anyway.
    }
  }, [repositoryId]);

  usePolling(loadSetupTask, SETUP_TASK_POLL_MS, !hasCIChecks);

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
      const task = await api.createWorkflowSetupTask(repositoryId);
      setSetupTask(task);
      toast.success(t("repositoryPage.components.setupTaskOpened", { key: task.key }));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("common.actionFailed"));
    } finally {
      setCreatingSetupTask(false);
    }
  };

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
            <span>
              {setupTask
                ? t("repositoryPage.components.noWorkflowsTaskOpenDesc", { key: setupTask.key })
                : t("repositoryPage.components.noWorkflowsDesc")}
            </span>
            <Button
              size="sm"
              onClick={handleCreateSetupTask}
              disabled={creatingSetupTask || setupTask !== null}
              className="shrink-0"
            >
              {setupTask
                ? t("repositoryPage.components.setupTaskOpen", { key: setupTask.key })
                : t("repositoryPage.components.openSetupTask")}
            </Button>
          </div>
        </Notice>
      )}
    </>
  );
}
