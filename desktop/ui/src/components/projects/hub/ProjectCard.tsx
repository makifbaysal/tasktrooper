import { FolderPlus, Pencil, Share2, Trash2 } from "lucide-react";
import { Link } from "react-router-dom";
import type { ProjectOverview } from "@/api";
import { RepositoryOverviewRow } from "@/components/projects/hub/RepositoryOverviewRow";
import { ProjectTypeBadge } from "@/components/projects/model/ProjectTypeBadge";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { EmptyState } from "@/components/ui/empty-state";
import { useI18n } from "@/hooks/useI18n";

interface ProjectCardProps {
  project: ProjectOverview;
  /** Repositories to show — the caller has already applied role/search filters. */
  repositories: ProjectOverview["repositories"];
  onEdit: () => void;
  onDelete: () => void;
}

/** One project card on the hub: header (name, type, counts, review badge,
 * edit/delete), a row per repository, and a footer that only appears when
 * this project crosses into another one — an independent project's card
 * ends at its last repository row. */
export function ProjectCard({ project, repositories, onEdit, onDelete }: ProjectCardProps) {
  const { t } = useI18n();
  const totalComponents = project.repositories.reduce((sum, r) => sum + r.components.length, 0);
  const hasCrossProject = project.cross_projects.length > 0 || project.shared_resources.length > 0;

  return (
    <Card className="w-full overflow-hidden p-0">
      <div className="flex flex-wrap items-start justify-between gap-3 border-b border-border px-4 py-3">
        <div className="min-w-0">
          <div className="flex flex-wrap items-center gap-2">
            <Link to={`/projects/${project.id}`} className="font-semibold hover:underline">
              {project.name}
            </Link>
            <ProjectTypeBadge type={project.type} />
            <span className="text-caption text-muted-foreground">
              {t("projectsHub.card.counts", { repos: project.repositories.length, components: totalComponents })}
            </span>
            {project.review_count > 0 && (
              <Badge variant="warning">{t("projectsHub.card.reviewBadge", { count: project.review_count })}</Badge>
            )}
          </div>
          <p className="mt-1 line-clamp-2 text-sm text-muted-foreground">
            {project.description || t("projectsHub.card.noDescription")}
          </p>
        </div>
        <div className="flex shrink-0 gap-1">
          <Button
            variant="ghost"
            size="icon"
            className="h-7 w-7"
            onClick={onEdit}
            title={t("projectsHub.card.editProject")}
          >
            <Pencil className="h-3.5 w-3.5" />
          </Button>
          <Button
            variant="ghost"
            size="icon"
            className="h-7 w-7 text-muted-foreground hover:text-destructive"
            onClick={onDelete}
            title={t("projectsHub.card.deleteProject")}
          >
            <Trash2 className="h-3.5 w-3.5" />
          </Button>
        </div>
      </div>

      {project.repositories.length === 0 ? (
        <EmptyState
          icon={FolderPlus}
          title={t("projectsHub.card.emptyRepos")}
          action={
            <Button size="sm" asChild>
              <Link to={`/projects/new?project=${project.id}`}>{t("projectsHub.actions.addRepository")}</Link>
            </Button>
          }
          className="py-8"
        />
      ) : (
        <>
          <div className="divide-y divide-border">
            {repositories.map((repo) => (
              <RepositoryOverviewRow key={repo.id} repository={repo} projectId={project.id} />
            ))}
          </div>
          {hasCrossProject && (
            <div className="flex flex-wrap items-center gap-1.5 border-t border-border bg-muted/20 px-4 py-2 text-caption text-muted-foreground">
              <Share2 className="h-3.5 w-3.5 shrink-0" aria-hidden />
              {project.cross_projects.length > 0 && (
                <span className="inline-flex flex-wrap items-center gap-1">
                  <span>{t("projectsHub.card.linksTo")}</span>
                  {project.cross_projects.map((p, i) => (
                    <span key={p.id}>
                      <Link to={`/projects/${p.id}`} className="font-medium text-foreground hover:underline">
                        {p.name}
                      </Link>
                      {i < project.cross_projects.length - 1 ? "," : ""}
                    </span>
                  ))}
                </span>
              )}
              {project.shared_resources.length > 0 && (
                <span className="inline-flex flex-wrap items-center gap-1">
                  {project.cross_projects.length > 0 && <span aria-hidden>·</span>}
                  <span>
                    {t("projectsHub.card.sharesPrefix")} {project.shared_resources.map((r) => r.name).join(", ")}
                  </span>
                  {project.cross_projects.length > 0 && (
                    <span>
                      {t("projectsHub.card.sharesWith")} {project.cross_projects.map((p) => p.name).join(", ")}
                    </span>
                  )}
                </span>
              )}
            </div>
          )}
        </>
      )}
    </Card>
  );
}
