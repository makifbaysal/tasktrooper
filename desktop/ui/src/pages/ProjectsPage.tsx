import { FolderKanban, LayoutGrid, Map as MapIcon, Plus } from "lucide-react";
import { lazy, Suspense, useEffect, useMemo, useState } from "react";
import { Link, useNavigate, useSearchParams } from "react-router-dom";
import { toast } from "sonner";
import {
  api,
  COMPONENT_ROLES,
  type ComponentRole,
  type InitiativeProject,
  type ProjectOverview,
  type RepositorySummary,
} from "@/api";
import { AttentionNotice } from "@/components/projects/hub/AttentionNotice";
import { ProjectCard } from "@/components/projects/hub/ProjectCard";
import { ProjectFilters } from "@/components/projects/hub/ProjectFilters";
import { UnassignedRepositoriesCard } from "@/components/projects/hub/UnassignedRepositoriesCard";
const WorkspaceMapView = lazy(() => import("@/components/projects/map/WorkspaceMapView").then((m) => ({ default: m.WorkspaceMapView })));
import { ProjectFormDialog } from "@/components/projects/ProjectFormDialog";
import { PageHeader } from "@/components/admin/PageHeader";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { ConfirmDialog } from "@/components/ui/confirm-dialog";
import { EmptyState } from "@/components/ui/empty-state";
import { Skeleton } from "@/components/ui/skeleton";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { useI18n } from "@/hooks/useI18n";
import { useProjectsOverview } from "@/hooks/useProjectsOverview";
import { useWorkspaceMap } from "@/hooks/useWorkspaceMap";

type ProjectsView = "cards" | "map";

function normalize(value: string): string {
  return value.trim().toLocaleLowerCase("tr");
}

function repoMatches(repo: RepositorySummary, role: ComponentRole | null, query: string): boolean {
  if (role && !repo.components.some((c) => c.role === role)) return false;
  if (!query) return true;
  if (normalize(repo.name).includes(query)) return true;
  return repo.components.some((c) => normalize(c.path).includes(query) || normalize(c.stack_summary).includes(query));
}

/** Repositories to render for one project card, after role/search narrow it
 * down; a role filter always narrows to matching repos, a text query keeps
 * every repo when the project's own name already matched it. */
function visibleRepositories(project: ProjectOverview, role: ComponentRole | null, query: string): RepositorySummary[] {
  if (role) return project.repositories.filter((r) => repoMatches(r, role, query));
  if (query && !normalize(project.name).includes(query)) {
    return project.repositories.filter((r) => repoMatches(r, null, query));
  }
  return project.repositories;
}

function projectVisible(project: ProjectOverview, role: ComponentRole | null, query: string): boolean {
  return visibleRepositories(project, role, query).length > 0 || (!role && !query);
}

const RUNNING_POLL_MS = 3000;

export function ProjectsPage() {
  const { t } = useI18n();
  const navigate = useNavigate();
  const [searchParams, setSearchParams] = useSearchParams();
  const { overview, loading, error, reload } = useProjectsOverview();

  const roleParam = searchParams.get("role");
  const role = (COMPONENT_ROLES as string[]).includes(roleParam ?? "") ? (roleParam as ComponentRole) : null;
  const query = normalize(searchParams.get("q") ?? "");
  const view: ProjectsView = searchParams.get("view") === "map" ? "map" : "cards";
  const setView = (next: ProjectsView) =>
    setSearchParams(
      (prev) => {
        const params = new URLSearchParams(prev);
        if (next === "map") params.set("view", "map");
        else params.delete("view");
        return params;
      },
      { replace: true },
    );

  const setRole = (next: ComponentRole | null) =>
    setSearchParams((prev) => {
      const params = new URLSearchParams(prev);
      if (next) params.set("role", next);
      else params.delete("role");
      return params;
    });
  const setQuery = (next: string) =>
    setSearchParams(
      (prev) => {
        const params = new URLSearchParams(prev);
        if (next) params.set("q", next);
        else params.delete("q");
        return params;
      },
      { replace: true },
    );

  const [projectDialogOpen, setProjectDialogOpen] = useState(false);
  const [editProject, setEditProject] = useState<InitiativeProject | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<ProjectOverview | null>(null);
  const [deleting, setDeleting] = useState(false);

  useEffect(() => {
    if (error) toast.error(error);
  }, [error]);

  // The scan pipeline runs in seconds, so a light poll is enough to catch it
  // finishing without a websocket — stops the moment nothing is running.
  useEffect(() => {
    if (!overview) return;
    const anyRunning =
      overview.projects.some((p) => p.repositories.some((r) => r.last_scan?.status === "running")) ||
      overview.unassigned.some((r) => r.last_scan?.status === "running");
    if (!anyRunning) return;
    const id = window.setInterval(() => void reload(), RUNNING_POLL_MS);
    return () => window.clearInterval(id);
  }, [overview, reload]);

  const totals = useMemo(() => {
    if (!overview) return { projects: 0, repositories: 0, components: 0 };
    const projectRepos = overview.projects.flatMap((p) => p.repositories);
    const allRepos = [...projectRepos, ...overview.unassigned];
    return {
      projects: overview.projects.length,
      repositories: allRepos.length,
      components: allRepos.reduce((sum, r) => sum + r.components.length, 0),
    };
  }, [overview]);

  const filteredProjects = useMemo(
    () => (overview ? overview.projects.filter((p) => projectVisible(p, role, query)) : []),
    [overview, role, query],
  );
  const filteredUnassigned = useMemo(
    () => (overview ? overview.unassigned.filter((r) => repoMatches(r, role, query)) : []),
    [overview, role, query],
  );

  const openCreateProject = () => {
    setEditProject(null);
    setProjectDialogOpen(true);
  };

  const openEditProject = (project: ProjectOverview) => {
    setEditProject({ id: project.id, name: project.name, description: project.description, created_at: "", updated_at: "" });
    setProjectDialogOpen(true);
  };

  const handleCreated = (project: InitiativeProject) => {
    if (!editProject) navigate(`/projects/new?project=${project.id}`);
    else void reload();
  };

  const handleDelete = async () => {
    if (!deleteTarget) return;
    setDeleting(true);
    try {
      await api.deleteInitiativeProject(deleteTarget.id);
      toast.success(t("projectsHub.deleted"));
      setDeleteTarget(null);
      await reload();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("projectsHub.deleteFailed"));
    } finally {
      setDeleting(false);
    }
  };

  const noProjectsAtAll = !loading && overview !== null && overview.projects.length === 0 && overview.unassigned.length === 0;

  return (
    <>
      <PageHeader
        title={t("projectsHub.title")}
        description={
          overview
            ? t("projectsHub.subtitle", {
                projects: totals.projects,
                repositories: totals.repositories,
                components: totals.components,
              })
            : undefined
        }
        action={
          <div className="flex flex-wrap gap-2">
            <Button variant="outline" asChild className="gap-2">
              <Link to="/projects/new">
                <Plus className="h-4 w-4" />
                {t("projectsHub.actions.addRepository")}
              </Link>
            </Button>
            <Button onClick={openCreateProject} className="gap-2">
              <Plus className="h-4 w-4" />
              {t("projectsHub.actions.newProject")}
            </Button>
          </div>
        }
      />

      {loading && !overview ? (
        <div className="space-y-4">
          <Skeleton className="h-40 rounded-xl" />
          <Skeleton className="h-40 rounded-xl" />
        </div>
      ) : noProjectsAtAll ? (
        <Card className="border-dashed">
          <EmptyState
            icon={FolderKanban}
            title={t("projectsHub.empty.title")}
            description={t("projectsHub.empty.description")}
            action={<Button onClick={openCreateProject}>{t("projectsHub.empty.cta")}</Button>}
            className="py-16"
          />
        </Card>
      ) : (
        <div className="space-y-4">
          {overview && <AttentionNotice projects={overview.projects} />}

          <div className="flex flex-wrap items-center justify-between gap-2">
            {view === "cards" ? (
              <ProjectFilters role={role} onRoleChange={setRole} query={searchParams.get("q") ?? ""} onQueryChange={setQuery} />
            ) : (
              <span />
            )}
            <Tabs value={view} onValueChange={(v) => setView(v as ProjectsView)} variant="pill" className="w-fit">
              <TabsList>
                <TabsTrigger value="cards" className="gap-1.5">
                  <LayoutGrid className="h-3.5 w-3.5" />
                  {t("projectsHub.view.cards")}
                </TabsTrigger>
                <TabsTrigger value="map" className="gap-1.5">
                  <MapIcon className="h-3.5 w-3.5" />
                  {t("projectsHub.view.map")}
                </TabsTrigger>
              </TabsList>
            </Tabs>
          </div>

          {view === "map" ? (
            <ProjectsMapContent />
          ) : (
            <>
              <div className="space-y-4">
                {filteredProjects.map((project) => (
                  <ProjectCard
                    key={project.id}
                    project={project}
                    repositories={visibleRepositories(project, role, query)}
                    onEdit={() => openEditProject(project)}
                    onDelete={() => setDeleteTarget(project)}
                  />
                ))}
              </div>

              {filteredUnassigned.length > 0 && (
                <UnassignedRepositoriesCard
                  repositories={filteredUnassigned}
                  projects={overview?.projects ?? []}
                  onAssigned={() => void reload()}
                />
              )}
            </>
          )}
        </div>
      )}

      <ProjectFormDialog
        open={projectDialogOpen}
        onOpenChange={setProjectDialogOpen}
        project={editProject}
        onSaved={handleCreated}
      />

      <ConfirmDialog
        open={deleteTarget !== null}
        onOpenChange={(open) => !open && setDeleteTarget(null)}
        title={t("projectsHub.card.deleteTitle", { name: deleteTarget?.name ?? "" })}
        description={t("projectsHub.card.deleteDescription")}
        confirmLabel={t("projectsHub.card.deleteProject")}
        loading={deleting}
        onConfirm={handleDelete}
      />
    </>
  );
}

/** Loads the workspace map only once the Map view is actually selected —
 * same on-demand contract as the project page's own Architecture tab. */
function ProjectsMapContent() {
  const { map, loading, error } = useWorkspaceMap();

  useEffect(() => {
    if (error) toast.error(error);
  }, [error]);

  if (loading && !map) {
    return <Skeleton className="h-[640px] w-full rounded-xl" />;
  }
  if (!map) return null;

  return (
    <Suspense fallback={<Skeleton className="h-[640px] w-full rounded-xl" />}>
      <WorkspaceMapView map={map} />
    </Suspense>
  );
}
