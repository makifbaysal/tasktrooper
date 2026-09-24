import { toast } from "sonner";
import { api, type InitiativeProject, type RepositorySummary } from "@/api";
import { GitWarningIcon } from "@/components/projects/hub/GitWarningIcon";
import { ScanStatusLabel } from "@/components/projects/hub/ScanStatusLabel";
import { RepoShapeBadge } from "@/components/projects/model/RepoShapeBadge";
import { Card } from "@/components/ui/card";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { useI18n } from "@/hooks/useI18n";

interface UnassignedRepositoriesCardProps {
  repositories: RepositorySummary[];
  projects: InitiativeProject[] | Pick<InitiativeProject, "id" | "name">[];
  onAssigned: () => void;
}

/** Repositories that link to no project yet — each row can be dropped
 * straight into one via the same PUT the settings tab uses. */
export function UnassignedRepositoriesCard({ repositories, projects, onAssigned }: UnassignedRepositoriesCardProps) {
  const { t } = useI18n();

  const assign = async (repositoryId: string, projectId: string) => {
    try {
      await api.setRepositoryProjects(repositoryId, [projectId]);
      onAssigned();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("common.saveFailed"));
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
            <Select key={projects.length} onValueChange={(projectId) => void assign(repo.id, projectId)}>
              <SelectTrigger className="h-8 w-48 shrink-0 text-sm">
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
          </div>
        ))}
      </div>
    </Card>
  );
}
