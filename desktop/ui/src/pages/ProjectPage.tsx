import { ChevronRight, FolderKanban, Plus } from "lucide-react";
import { useState } from "react";
import { Link, useParams, useSearchParams } from "react-router-dom";
import type { InitiativeProject } from "@/api";
import { PageHeader } from "@/components/admin/PageHeader";
import { ProjectFormDialog } from "@/components/projects/ProjectFormDialog";
import { ProjectRepositoriesTable } from "@/components/projects/hub/ProjectRepositoriesTable";
import { ProjectReviewTab } from "@/components/projects/hub/ProjectReviewTab";
import { ProjectSettingsTab } from "@/components/projects/hub/ProjectSettingsTab";
import { ProjectTypeBadge } from "@/components/projects/model/ProjectTypeBadge";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { EmptyState } from "@/components/ui/empty-state";
import { Skeleton } from "@/components/ui/skeleton";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { useI18n } from "@/hooks/useI18n";
import { useProjectOverview } from "@/hooks/useProjectOverview";

type ProjectTab = "repositories" | "review" | "settings";
const TABS: ProjectTab[] = ["repositories", "review", "settings"];

function tabFromParam(raw: string | null): ProjectTab {
  return TABS.find((t) => t === raw) ?? "repositories";
}

/** One project: its repositories, cross-project links, review queue and
 * settings (`GET /v1/projects/:projectId/overview`). */
export function ProjectPage() {
  const { t } = useI18n();
  const { projectId } = useParams();
  const { project, loading, reload } = useProjectOverview(projectId);
  const [searchParams, setSearchParams] = useSearchParams();
  const tab = tabFromParam(searchParams.get("tab"));
  const setTab = (next: ProjectTab) =>
    setSearchParams(
      (prev) => {
        const params = new URLSearchParams(prev);
        params.set("tab", next);
        return params;
      },
      { replace: true },
    );
  const [editOpen, setEditOpen] = useState(false);

  if (loading && !project) {
    return (
      <div className="space-y-4">
        <Skeleton className="h-8 w-64" />
        <Skeleton className="h-40 w-full" />
      </div>
    );
  }

  if (!project) {
    return (
      <EmptyState
        icon={FolderKanban}
        title={t("projectsHub.project.notFound.title")}
        description={t("projectsHub.project.notFound.description")}
        action={
          <Button asChild>
            <Link to="/projects">{t("projectsHub.project.notFound.back")}</Link>
          </Button>
        }
        className="py-16"
      />
    );
  }

  const editTarget: InitiativeProject = {
    id: project.id,
    name: project.name,
    description: project.description,
    created_at: "",
    updated_at: "",
  };

  return (
    <>
      <div className="mb-2 flex items-center gap-1.5 text-caption text-muted-foreground">
        <Link to="/projects" className="hover:text-foreground hover:underline">
          {t("projectsHub.project.breadcrumb")}
        </Link>
        <ChevronRight className="h-3 w-3" aria-hidden />
        <span className="text-foreground">{project.name}</span>
      </div>

      <PageHeader
        title={project.name}
        description={project.description || undefined}
        action={
          <div className="flex flex-wrap gap-2">
            <Button variant="outline" asChild className="gap-2">
              <Link to={`/projects/new?project=${project.id}`}>
                <Plus className="h-4 w-4" />
                {t("projectsHub.actions.addRepository")}
              </Link>
            </Button>
            <Button onClick={() => setEditOpen(true)}>{t("projectsHub.project.edit")}</Button>
          </div>
        }
      />

      <div className="-mt-4 mb-4 flex flex-wrap items-center gap-1.5">
        <ProjectTypeBadge type={project.type} />
        {project.cross_projects.map((p) => (
          <Link key={p.id} to={`/projects/${p.id}`}>
            <Badge variant="outline">{p.name}</Badge>
          </Link>
        ))}
      </div>

      <Tabs value={tab} onValueChange={(v) => setTab(v as ProjectTab)}>
        <TabsList>
          <TabsTrigger value="repositories">{t("projectsHub.project.tabs.repositories")}</TabsTrigger>
          <TabsTrigger value="review">
            {t("projectsHub.project.tabs.review")}
            {project.review.length > 0 && (
              <Badge variant="warning" className="ml-1.5">
                {project.review.length}
              </Badge>
            )}
          </TabsTrigger>
          <TabsTrigger value="settings">{t("projectsHub.project.tabs.settings")}</TabsTrigger>
        </TabsList>

        <TabsContent value="repositories">
          <ProjectRepositoriesTable project={project} />
        </TabsContent>

        <TabsContent value="review">
          <ProjectReviewTab project={project} onChanged={() => void reload()} />
        </TabsContent>

        <TabsContent value="settings">
          <ProjectSettingsTab project={project} onSaved={() => void reload()} />
        </TabsContent>
      </Tabs>

      <ProjectFormDialog
        open={editOpen}
        onOpenChange={setEditOpen}
        project={editTarget}
        onSaved={() => void reload()}
      />
    </>
  );
}
