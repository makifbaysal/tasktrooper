import { CheckCircle2 } from "lucide-react";
import { useNavigate } from "react-router-dom";
import type { DoneStats, PendingRepo } from "@/components/projects/add/flow-types";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Notice } from "@/components/ui/notice";
import { useI18n } from "@/hooks/useI18n";

interface DoneStepProps {
  repos: PendingRepo[];
  projectId: string;
  projectName: string;
  stats: DoneStats;
}

function StatTile({ value, label }: { value: number; label: string }) {
  return (
    <Card>
      <CardContent className="flex flex-col items-center gap-1 py-4 text-center">
        <span className="text-display font-semibold">{value}</span>
        <span className="text-caption text-muted-foreground">{label}</span>
      </CardContent>
    </Card>
  );
}

/** Step 4: what landed, at a glance, and where to go next. Single successful
 * repository opens straight to it; several open the project instead, since
 * there is no one obvious repository to land on. */
export function DoneStep({ repos, projectId, projectName, stats }: DoneStepProps) {
  const { t } = useI18n();
  const navigate = useNavigate();
  const readyRepos = repos.filter((r): r is PendingRepo & { repositoryId: string } => r.status === "ready" && Boolean(r.repositoryId));
  const names = readyRepos.map((r) => r.label).join(", ");

  const openRepository = () => {
    if (readyRepos.length === 1) navigate(`/repositories/${readyRepos[0].repositoryId}?project=${projectId}`);
    else navigate(`/projects/${projectId}`);
  };

  return (
    <div className="space-y-6">
      <div className="flex flex-col items-center gap-2 py-6 text-center">
        <CheckCircle2 className="h-10 w-10 text-success" aria-hidden />
        <h2 className="text-title font-semibold">{t("addRepository.done.added", { names, project: projectName })}</h2>
      </div>

      <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
        <StatTile value={stats.components} label={t("addRepository.done.statComponents")} />
        <StatTile value={stats.checks} label={t("addRepository.done.statChecks", { required: stats.requiredChecks })} />
        <StatTile value={stats.links} label={t("addRepository.done.statLinks")} />
        <StatTile value={stats.reviewAnswered} label={t("addRepository.done.statReview")} />
      </div>

      <Notice variant="info" title={t("addRepository.done.backgroundTitle")}>
        {t("addRepository.done.backgroundNote")}
      </Notice>

      <div className="flex justify-center gap-2">
        <Button onClick={openRepository}>{t("addRepository.done.openRepository")}</Button>
        <Button variant="outline" onClick={() => navigate(`/projects/${projectId}`)}>
          {t("addRepository.done.openProject")}
        </Button>
      </div>
    </div>
  );
}
