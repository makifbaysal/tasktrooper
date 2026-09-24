import { Link } from "react-router-dom";
import type { RepositorySummary } from "@/api";
import { EnvironmentChips } from "@/components/projects/hub/EnvironmentChips";
import { GitWarningIcon } from "@/components/projects/hub/GitWarningIcon";
import { ScanStatusLabel } from "@/components/projects/hub/ScanStatusLabel";
import { RepoShapeBadge } from "@/components/projects/model/RepoShapeBadge";
import { RoleBadge } from "@/components/projects/model/RoleBadge";
import { Badge } from "@/components/ui/badge";
import { useI18n } from "@/hooks/useI18n";

interface RepositoryOverviewRowProps {
  repository: RepositorySummary;
  projectId: string;
}

/** One repository line inside a project card: name, shape, its components
 * (a RoleBadge per component, path shown only for a monorepo's many), a
 * single-component repo's stack summary, scan status and review count. */
export function RepositoryOverviewRow({ repository, projectId }: RepositoryOverviewRowProps) {
  const { t } = useI18n();
  const single = repository.shape === "single";

  return (
    <div className="flex flex-wrap items-center justify-between gap-3 px-4 py-3">
      <div className="min-w-0 flex-1">
        <div className="flex flex-wrap items-center gap-2">
          <Link
            to={`/repositories/${repository.id}?project=${projectId}`}
            className="truncate font-medium hover:underline"
          >
            {repository.name}
          </Link>
          <RepoShapeBadge shape={repository.shape} />
          {repository.git_warning && <GitWarningIcon warning={repository.git_warning} />}
        </div>
        {repository.components.length > 0 && (
          <div className="mt-1.5 space-y-1">
            {repository.components.map((c) => {
              const envs = repository.environments.filter((e) => e.component_id === c.id);
              return single ? (
                <div key={c.id} className="flex flex-wrap items-center gap-1.5">
                  <RoleBadge role={c.role} />
                  <EnvironmentChips environments={envs} />
                </div>
              ) : (
                <div key={c.id} className="flex flex-wrap items-center gap-1.5 text-micro text-muted-foreground">
                  <RoleBadge role={c.role} />
                  <span className="font-mono">{c.path}</span>
                  <EnvironmentChips environments={envs} />
                </div>
              );
            })}
          </div>
        )}
        {single && repository.components[0]?.stack_summary && (
          <p className="mt-1 text-micro text-muted-foreground">{repository.components[0].stack_summary}</p>
        )}
      </div>
      <div className="flex shrink-0 items-center gap-3 text-caption">
        <ScanStatusLabel scan={repository.last_scan} />
        {repository.review_count > 0 && (
          <Badge variant="warning">{t("projectsHub.card.reviewBadge", { count: repository.review_count })}</Badge>
        )}
      </div>
    </div>
  );
}
