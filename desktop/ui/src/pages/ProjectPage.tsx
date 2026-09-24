import { ChevronRight, FolderKanban, Plus } from "lucide-react";
import { lazy, Suspense, useEffect, useState } from "react";
import { Link, useParams, useSearchParams } from "react-router-dom";
import { toast } from "sonner";
import type { InitiativeProject, ProjectDetail } from "@/api";
import { PageHeader } from "@/components/admin/PageHeader";
import { ProjectFormDialog } from "@/components/projects/ProjectFormDialog";
import { ProjectRepositoriesTable } from "@/components/projects/hub/ProjectRepositoriesTable";
import { ProjectReviewTab } from "@/components/projects/hub/ProjectReviewTab";
import { ProjectSettingsTab } from "@/components/projects/hub/ProjectSettingsTab";
const ProjectArchitectureMap = lazy(() => import("@/components/projects/map/ProjectArchitectureMap").then((m) => ({ default: m.ProjectArchitectureMap })));
import { ProjectTypeBadge } from "@/components/projects/model/ProjectTypeBadge";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { EmptyState } from "@/components/ui/empty-state";
import { Skeleton } from "@/components/ui/skeleton";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { useI18n } from "@/hooks/useI18n";
import { useProjectMap } from "@/hooks/useProjectMap";
import { useProjectOverview } from "@/hooks/useProjectOverview";

type ProjectTab = "architecture" | "repositories" | "review" | "settings";
const TABS: ProjectTab[] = ["architecture", "repositories", "review", "settings"];

/** Architecture is the default the moment there is something to map; a
 * project with no repositories yet has nothing to draw, so it opens on the
 * table that gets it its first one. */
function tabFromParam(raw: string | null, project: ProjectDetail | null): ProjectTab {
  const requested = TABS.find((t) => t === raw);
  if (requested) return requested;
  return project && project.repositories.length === 0 ? "repositories" : "architecture";
}

/** One project: its repositories, cross-project links, review queue and
 * settings (`GET /v1/projects/:projectId/overview`). */
export function ProjectPage() {
  const { t } = useI18n();
  const { projectId } = useParams();
  const { project, loading, reload } = useProjectOverview(projectId);
  const [searchParams, setSearchParams] = useSearchParams();
  const tab = tabFromParam(searchParams.get("tab"), project);
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
          <TabsTrigger value="architecture">{t("projectsHub.project.tabs.architecture")}</TabsTrigger>
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

        <TabsContent value="architecture">
          <ProjectArchitectureTabContent project={project} />
        </TabsContent>

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

/** Loads the map only once the Architecture tab is actually mounted — same
 * on-demand contract as ProjectReviewTab's own per-repository model fetch. */
function ProjectArchitectureTabContent({ project }: { project: ProjectDetail }) {
  const { map, loading, error, reload } = useProjectMap(project.id);

  useEffect(() => {
    if (error) toast.error(error);
  }, [error]);

  if (loading && !map) {
    return <Skeleton className="h-[600px] w-full rounded-xl" />;
  }
  if (!map) return null;

  return (
    <Suspense fallback={<Skeleton className="h-[600px] w-full rounded-xl" />}>
      <ProjectArchitectureMap
        map={map}
        currentProjectId={project.id}
        crossProjects={project.cross_projects}
        onChanged={() => void reload()}
      />
    </Suspense>
  );
}
