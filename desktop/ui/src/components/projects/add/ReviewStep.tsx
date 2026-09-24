import { useCallback, useRef, useState } from "react";
import type { RepositoryModel } from "@/api";
import { RepoReviewCard } from "@/components/projects/add/RepoReviewCard";
import type { DoneStats, PendingRepo } from "@/components/projects/add/flow-types";
import { Button } from "@/components/ui/button";
import { Notice } from "@/components/ui/notice";
import { useI18n } from "@/hooks/useI18n";
import { requiredChecks } from "@/lib/project-model";

interface ReviewStepProps {
  repos: PendingRepo[];
  onFinish: (stats: DoneStats) => void;
}

/**
 * Step 3: one card per successfully-imported repository (a failed import
 * never reached here — there is nothing to review). Shows the success
 * notice only once every card's model has loaded and none of them has a
 * review item left, so it never flashes on top of cards still loading.
 */
export function ReviewStep({ repos, onFinish }: ReviewStepProps) {
  const { t } = useI18n();
  const readyRepos = repos.filter((r): r is PendingRepo & { repositoryId: string } => r.status === "ready" && Boolean(r.repositoryId));

  const [models, setModels] = useState<Record<string, RepositoryModel>>({});
  const initialReviewRef = useRef<Record<string, number>>({});

  const handleModelChange = useCallback((repositoryId: string, model: RepositoryModel) => {
    setModels((prev) => ({ ...prev, [repositoryId]: model }));
    if (!(repositoryId in initialReviewRef.current)) {
      initialReviewRef.current[repositoryId] = model.review.length;
    }
  }, []);

  const allLoaded = readyRepos.length > 0 && readyRepos.every((r) => Boolean(models[r.repositoryId]));
  const totalReview = Object.values(models).reduce((sum, m) => sum + m.review.length, 0);

  const handleFinish = () => {
    const values = Object.values(models);
    const reviewInitialTotal = Object.values(initialReviewRef.current).reduce((sum, n) => sum + n, 0);
    const stats: DoneStats = {
      components: values.reduce((sum, m) => sum + m.components.filter((c) => c.status === "active").length, 0),
      checks: values.reduce((sum, m) => sum + m.checks.length, 0),
      requiredChecks: values.reduce((sum, m) => sum + requiredChecks(m.checks), 0),
      links: values.reduce((sum, m) => sum + m.links.length, 0),
      reviewAnswered: reviewInitialTotal - totalReview,
    };
    onFinish(stats);
  };

  return (
    <div className="space-y-4">
      <h2 className="text-title font-semibold">{t("addRepository.review.heading")}</h2>

      {allLoaded && totalReview === 0 && <Notice variant="info" title={t("addRepository.review.nothingToAsk")} />}

      <div className="space-y-4">
        {readyRepos.map((r) => (
          <RepoReviewCard key={r.repositoryId} repositoryId={r.repositoryId} onModelChange={handleModelChange} />
        ))}
      </div>

      <div className="flex justify-end">
        <Button size="lg" disabled={!allLoaded} onClick={handleFinish}>
          {t("addRepository.review.finish")}
        </Button>
      </div>
    </div>
  );
}
