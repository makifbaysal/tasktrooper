import { FolderPlus, Plus } from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { api, type InitiativeProject } from "@/api";
import { ProjectFormDialog } from "@/components/projects/ProjectFormDialog";
import { Button } from "@/components/ui/button";
import { Notice } from "@/components/ui/notice";
import { Skeleton } from "@/components/ui/skeleton";
import { useI18n } from "@/hooks/useI18n";
import { useSetup } from "@/hooks/useSetup";

/**
 * Step 4: a project, and the first repository imported into it.
 *
 * Creating the project stays here (`ProjectFormDialog`, same as the Projects
 * page); importing a repository is now the full-page `/projects/new` flow, so
 * this step only has to send the user there — with `?project=` once a project
 * exists, so the wizard lands pre-selected instead of asking again.
 *
 * The step is done when a project has at least one repository linked to it,
 * which `useSetup` derives; this component only has to get the user there.
 */
export function FirstProjectStep() {
  const { t } = useI18n();
  const { steps, refresh } = useSetup();
  const step = steps.project;

  const [projects, setProjects] = useState<InitiativeProject[] | null>(null);
  const [loadError, setLoadError] = useState("");
  const [dialogOpen, setDialogOpen] = useState(false);

  // Its own read rather than the provider's: this screen renders the project
  // ROW — name, the add-repository link — and needs the objects, where the
  // provider only keeps the yes/no the step's state is derived from.
  const load = useCallback(async () => {
    try {
      const p = await api.listInitiativeProjects();
      setProjects(p.projects ?? []);
      setLoadError("");
    } catch (e) {
      setLoadError(e instanceof Error ? e.message : t("setup.project.loadFailed"));
    }
    // The verdict this step is gated on is the provider's, not this component's.
    await refresh();
  }, [t, refresh]);

  useEffect(() => {
    void load();
  }, [load]);

  // The first project is the one the sequence is about. Everything else on
  // this screen — editing, deleting, the other projects — belongs on the
  // Projects page, which is one click away once this is done.
  const project = projects?.[0] ?? null;
  const addRepositoryHref = project ? `/projects/new?project=${project.id}` : "/projects/new";

  return (
    <div className="space-y-3">
      <p className="text-sm text-muted-foreground">{t("setup.project.description")}</p>

      {step.state === "unknown" && (
        <Notice variant="warning" title={t("setup.state.unknown")}>
          <p>{step.error ?? t("setup.unknownHint")}</p>
          {step.error && <p className="mt-1">{t("setup.unknownHint")}</p>}
        </Notice>
      )}

      {loadError && (
        <Notice variant="error" title={t("setup.project.loadFailed")}>
          <p>{loadError}</p>
          <Button variant="outline" size="sm" className="mt-2" onClick={() => void load()}>
            {t("common.refresh")}
          </Button>
        </Notice>
      )}

      {projects === null && !loadError && <Skeleton className="h-32 rounded-xl" />}

      {projects !== null && project === null && (
        <Button variant="outline" className="gap-2" onClick={() => setDialogOpen(true)}>
          <FolderPlus className="h-4 w-4" />
          {t("setup.project.createProject")}
        </Button>
      )}

      {project !== null && (
        <>
          {step.state === "done" ? (
            <Notice variant="info" title={t("setup.project.doneTitle")}>
              {t("setup.project.doneBody")}
            </Notice>
          ) : (
            <Notice variant="warning" title={t("setup.project.needsRepositoryTitle")}>
              {t("setup.project.needsRepositoryBody")}
            </Notice>
          )}

          <div className="flex items-center justify-between gap-3 rounded-lg border p-3">
            <p className="text-body font-medium">{project.name}</p>
            <Button variant="outline" size="sm" onClick={() => setDialogOpen(true)}>
              {t("projectsHub.project.edit")}
            </Button>
          </div>
        </>
      )}

      {projects !== null && (
        <Button asChild className="gap-2">
          <Link to={addRepositoryHref}>
            <Plus className="h-4 w-4" />
            {t("projectsHub.actions.addRepository")}
          </Link>
        </Button>
      )}

      <ProjectFormDialog
        open={dialogOpen}
        onOpenChange={setDialogOpen}
        project={project}
        onSaved={() => void load()}
      />
    </div>
  );
}
