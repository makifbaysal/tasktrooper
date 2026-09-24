import { CheckCircle2 } from "lucide-react";
import { useCallback, useEffect, useMemo, useState } from "react";
import { toast } from "sonner";
import { api, type ProjectDetail, type RepositoryModel } from "@/api";
import { ReviewList } from "@/components/projects/model/ReviewList";
import { Card } from "@/components/ui/card";
import { EmptyState } from "@/components/ui/empty-state";
import { Skeleton } from "@/components/ui/skeleton";
import { useI18n } from "@/hooks/useI18n";

interface ProjectReviewTabProps {
  project: ProjectDetail;
  /** Reload the project overview too — a review answer can change review_count
   * project-wide, not just inside the one repository model refetched here. */
  onChanged: () => void;
}

/** One card per repository that still has review items, each fed the shared
 * ReviewList. Loads every such repository's full model in parallel since the
 * summaries on ProjectDetail only carry a count, not the items themselves. */
export function ProjectReviewTab({ project, onChanged }: ProjectReviewTabProps) {
  const { t } = useI18n();
  const repos = useMemo(() => project.repositories.filter((r) => r.review_count > 0), [project]);
  const repoIds = useMemo(() => repos.map((r) => r.id), [repos]);
  const [models, setModels] = useState<Record<string, RepositoryModel>>({});
  const [loading, setLoading] = useState(true);

  const load = useCallback(async () => {
    if (repoIds.length === 0) {
      setModels({});
      setLoading(false);
      return;
    }
    setLoading(true);
    try {
      const pairs = await Promise.all(repoIds.map(async (id) => [id, await api.getRepositoryModel(id)] as const));
      setModels(Object.fromEntries(pairs));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("projectModel.review.failed"));
    } finally {
      setLoading(false);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [repoIds.join(",")]);

  useEffect(() => {
    void load();
  }, [load]);

  const reloadOne = async (id: string) => {
    try {
      const fresh = await api.getRepositoryModel(id);
      setModels((prev) => ({ ...prev, [id]: fresh }));
    } catch {
      // The full reload below re-derives which repos still have anything to
      // show; a failed single refetch just leaves this card stale till then.
    }
    onChanged();
  };

  if (loading) {
    return (
      <div className="space-y-4">
        <Skeleton className="h-32 w-full" />
        <Skeleton className="h-32 w-full" />
      </div>
    );
  }

  if (repos.length === 0) {
    return <EmptyState icon={CheckCircle2} title={t("projectsHub.project.reviewEmpty")} className="py-12" />;
  }

  return (
    <div className="space-y-4">
      {repos.map((repo) => {
        const model = models[repo.id];
        if (!model) return null;
        return (
          <Card key={repo.id} className="space-y-3 p-4">
            <h3 className="font-semibold">{repo.name}</h3>
            <ReviewList model={model} onChanged={() => void reloadOne(repo.id)} />
          </Card>
        );
      })}
    </div>
  );
}
