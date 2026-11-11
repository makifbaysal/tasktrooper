import { CheckCircle2 } from "lucide-react";
import { useNavigate } from "react-router-dom";
import type { NewRepositoryResponse } from "@/api";
import { Button } from "@/components/ui/button";
import { Notice } from "@/components/ui/notice";
import { useI18n } from "@/hooks/useI18n";

interface NewRepositoryDoneStepProps {
  result: NewRepositoryResponse;
  projectId: string;
  projectName: string;
}

/** The Done step for a repository created from scratch: no scan ran, so
 * instead of stats it points at the bootstrap task that sets the repo up. */
export function NewRepositoryDoneStep({ result, projectId, projectName }: NewRepositoryDoneStepProps) {
  const { t } = useI18n();
  const navigate = useNavigate();
  const { repository, task } = result;

  return (
    <div className="space-y-6">
      <div className="flex flex-col items-center gap-2 py-6 text-center">
        <CheckCircle2 className="h-10 w-10 text-success" aria-hidden />
        <h2 className="text-title font-semibold">
          {t("addRepository.done.created", { name: repository.name, project: projectName })}
        </h2>
        <p className="text-caption text-muted-foreground">{t("addRepository.done.analyzedAfterMerge")}</p>
      </div>

      {task ? (
        <Notice variant="info" title={t("addRepository.done.setupTaskAdded", { key: task.key, title: task.title })}>
          <Button
            size="sm"
            variant="outline"
            className="mt-2"
            onClick={() => navigate(`/board?task=${encodeURIComponent(task.id)}`)}
          >
            {t("addRepository.done.openTask")}
          </Button>
        </Notice>
      ) : (
        <Notice variant="warning" title={t("addRepository.done.noSetupTaskTitle")}>
          {t("addRepository.done.noSetupTaskBody")}
        </Notice>
      )}

      <div className="flex justify-center gap-2">
        <Button onClick={() => navigate(`/repositories/${repository.id}?project=${projectId}&setup=1`)}>
          {t("addRepository.done.openRepository")}
        </Button>
        <Button variant="outline" onClick={() => navigate(`/projects/${projectId}`)}>
          {t("addRepository.done.openProject")}
        </Button>
      </div>
    </div>
  );
}
