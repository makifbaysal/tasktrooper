import { Trash2 } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";
import { api, type InitiativeProject, type RepositorySummary } from "@/api";
import { GitWarningIcon } from "@/components/projects/hub/GitWarningIcon";
import { ScanStatusLabel } from "@/components/projects/hub/ScanStatusLabel";
import { RepoShapeBadge } from "@/components/projects/model/RepoShapeBadge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { ConfirmDialog } from "@/components/ui/confirm-dialog";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { useI18n } from "@/hooks/useI18n";

interface UnassignedRepositoriesCardProps {
  repositories: RepositorySummary[];
  projects: InitiativeProject[] | Pick<InitiativeProject, "id" | "name">[];
  onChanged: () => void;
}

/** Repositories that link to no project yet — each row can be dropped
 * straight into one via the same PUT the settings tab uses, or removed with
 * the same DELETE the repository's own settings tab uses. */
export function UnassignedRepositoriesCard({ repositories, projects, onChanged }: UnassignedRepositoriesCardProps) {
  const { t } = useI18n();
  const [removeTarget, setRemoveTarget] = useState<RepositorySummary | null>(null);
  const [removing, setRemoving] = useState(false);

  const assign = async (repositoryId: string, projectId: string) => {
    try {
      await api.setRepositoryProjects(repositoryId, [projectId]);
      onChanged();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("common.saveFailed"));
    }
  };

  const remove = async () => {
    if (!removeTarget) return;
    setRemoving(true);
    try {
      await api.deleteRepository(removeTarget.id);
      toast.success(t("projectsHub.unassigned.removed"));
      setRemoveTarget(null);
      onChanged();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("projectsHub.unassigned.removeFailed"));
    } finally {
      setRemoving(false);
    }
  };

  return (
    <Card className="overflow-hidden p-0">
      <div className="border-b border-border px-4 py-3">
        <h3 className="font-semibold">{t("projectsHub.unassigned.title")}</h3>
        <p className="mt-0.5 text-sm text-muted-foreground">{t("projectsHub.unassigned.description")}</p>
      </div>
      <div className="divide-y divide-border">
        {repositories.map((repo) => (
          <div key={repo.id} className="flex flex-wrap items-center justify-between gap-3 px-4 py-3">
            <div className="min-w-0 flex-1">
              <div className="flex flex-wrap items-center gap-2">
                <span className="truncate font-medium">{repo.name}</span>
                <RepoShapeBadge shape={repo.shape} />
                {repo.git_warning && <GitWarningIcon warning={repo.git_warning} />}
              </div>
              <ScanStatusLabel scan={repo.last_scan} className="mt-1 text-micro" />
            </div>
            <div className="flex shrink-0 items-center gap-1">
              <Select key={projects.length} onValueChange={(projectId) => void assign(repo.id, projectId)}>
                <SelectTrigger className="h-8 w-48 text-sm">
                  <SelectValue placeholder={t("projectsHub.unassigned.addToProject")} />
                </SelectTrigger>
                <SelectContent>
                  {projects.map((p) => (
                    <SelectItem key={p.id} value={p.id}>
                      {p.name}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <Button
                variant="ghost"
                size="icon"
                className="h-8 w-8 text-muted-foreground hover:text-destructive"
                onClick={() => setRemoveTarget(repo)}
                title={t("projectsHub.unassigned.remove")}
                aria-label={t("projectsHub.unassigned.remove")}
              >
                <Trash2 className="h-3.5 w-3.5" />
              </Button>
            </div>
          </div>
        ))}
      </div>

      <ConfirmDialog
        open={removeTarget !== null}
        onOpenChange={(open) => !open && setRemoveTarget(null)}
        title={t("projectsHub.unassigned.removeTitle", { name: removeTarget?.name ?? "" })}
        description={t("projectsHub.unassigned.removeDescription")}
        confirmLabel={t("projectsHub.unassigned.remove")}
        loading={removing}
        onConfirm={remove}
      />
    </Card>
  );
}
