import { FolderPlus } from "lucide-react";
import { Link } from "react-router-dom";
import type { ProjectDetail } from "@/api";
import { EnvironmentChips } from "@/components/projects/hub/EnvironmentChips";
import { ScanStatusLabel } from "@/components/projects/hub/ScanStatusLabel";
import { RepoShapeBadge } from "@/components/projects/model/RepoShapeBadge";
import { RoleBadge } from "@/components/projects/model/RoleBadge";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { EmptyState } from "@/components/ui/empty-state";
import { useI18n } from "@/hooks/useI18n";

interface ProjectRepositoriesTableProps {
  project: ProjectDetail;
}

/** The project page's default tab: one row per repository, its shape,
 * component roles, the total required checks across them, review count and
 * last scan. */
export function ProjectRepositoriesTable({ project }: ProjectRepositoriesTableProps) {
  const { t } = useI18n();

  if (project.repositories.length === 0) {
    return (
      <Card className="p-0">
        <EmptyState
          icon={FolderPlus}
          title={t("projectsHub.card.emptyRepos")}
          action={
            <Button size="sm" asChild>
              <Link to={`/projects/new?project=${project.id}`}>{t("projectsHub.actions.addRepository")}</Link>
            </Button>
          }
          className="py-12"
        />
      </Card>
    );
  }

  return (
    <Card className="overflow-x-auto p-0">
      <table className="w-full text-left text-body">
        <thead className="border-b border-border bg-muted/20 text-caption uppercase text-muted-foreground">
          <tr>
            <th className="px-4 py-2 font-medium">{t("projectsHub.project.table.repository")}</th>
            <th className="px-4 py-2 font-medium">{t("projectsHub.project.table.shape")}</th>
            <th className="px-4 py-2 font-medium">{t("projectsHub.project.table.components")}</th>
            <th className="px-4 py-2 font-medium">{t("projectsHub.project.table.requiredChecks")}</th>
            <th className="px-4 py-2 font-medium">{t("projectsHub.project.table.review")}</th>
            <th className="px-4 py-2 font-medium">{t("projectsHub.project.table.lastScan")}</th>
            <th className="px-4 py-2" />
          </tr>
        </thead>
        <tbody className="divide-y divide-border">
          {project.repositories.map((repo) => (
            <tr key={repo.id} className="relative transition-colors hover:bg-muted/40">
              <td className="px-4 py-3">
                <Link
                  to={`/repositories/${repo.id}?project=${project.id}`}
                  className="font-medium after:absolute after:inset-0"
                >
                  {repo.name}
                </Link>
              </td>
              <td className="px-4 py-3">
                <RepoShapeBadge shape={repo.shape} />
              </td>
              <td className="px-4 py-3">
                <div className="space-y-1">
                  {repo.components.map((c) => (
                    <div key={c.id} className="flex flex-wrap items-center gap-1.5">
                      <RoleBadge role={c.role} />
                      <EnvironmentChips environments={repo.environments.filter((e) => e.component_id === c.id)} />
                    </div>
                  ))}
                </div>
              </td>
              <td className="px-4 py-3">{repo.components.reduce((sum, c) => sum + c.required_checks, 0)}</td>
              <td className="px-4 py-3">
                {repo.review_count > 0 ? (
                  <Badge variant="warning">{t("projectsHub.card.reviewBadge", { count: repo.review_count })}</Badge>
                ) : (
                  <span className="text-muted-foreground">—</span>
                )}
              </td>
              <td className="px-4 py-3">
                <ScanStatusLabel scan={repo.last_scan} />
              </td>
              <td className="px-4 py-3 text-right">
                <Button size="sm" variant="outline" asChild className="relative z-10">
                  <Link to={`/repositories/${repo.id}?project=${project.id}`}>{t("projectsHub.project.table.open")}</Link>
                </Button>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </Card>
  );
}
