import { Trash2 } from "lucide-react";
import { useMemo, useState } from "react";
import { useNavigate } from "react-router-dom";
import { toast } from "sonner";
import { api, type ProjectDetail, type RepositorySummary } from "@/api";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { ConfirmDialog } from "@/components/ui/confirm-dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Textarea } from "@/components/ui/textarea";
import { useI18n } from "@/hooks/useI18n";
import { useProjectsOverview } from "@/hooks/useProjectsOverview";

interface ProjectSettingsTabProps {
  project: ProjectDetail;
  onSaved: () => void;
}

/** Name/description, the member repository list (each removable), a picker
 * to add an existing repository, and the delete-project danger zone. */
export function ProjectSettingsTab({ project, onSaved }: ProjectSettingsTabProps) {
  const { t } = useI18n();
  const navigate = useNavigate();
  const { overview } = useProjectsOverview();

  const [name, setName] = useState(project.name);
  const [description, setDescription] = useState(project.description);
  const [saving, setSaving] = useState(false);
  const [removingId, setRemovingId] = useState<string | null>(null);
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [deleting, setDeleting] = useState(false);

  const availableRepos = useMemo(() => {
    if (!overview) return [];
    const byId = new Map<string, RepositorySummary>();
    for (const p of overview.projects) for (const r of p.repositories) byId.set(r.id, r);
    for (const r of overview.unassigned) byId.set(r.id, r);
    return [...byId.values()].filter((r) => !(r.project_ids ?? []).includes(project.id));
  }, [overview, project.id]);

  const save = async () => {
    if (!name.trim()) return;
    setSaving(true);
    try {
      await api.updateInitiativeProject(project.id, { name: name.trim(), description: description.trim() });
      toast.success(t("projectsHub.project.settings.saved"));
      onSaved();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("common.saveFailed"));
    } finally {
      setSaving(false);
    }
  };

  const removeFromProject = async (repo: RepositorySummary) => {
    setRemovingId(repo.id);
    try {
      const remaining = (repo.project_ids ?? []).filter((id) => id !== project.id);
      await api.setRepositoryProjects(repo.id, remaining);
      toast.success(t("projectsHub.project.settings.removed"));
      onSaved();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("projectsHub.project.settings.removeFailed"));
    } finally {
      setRemovingId(null);
    }
  };

  const addExisting = async (repositoryId: string) => {
    const repo = availableRepos.find((r) => r.id === repositoryId);
    try {
      await api.setRepositoryProjects(repositoryId, [...(repo?.project_ids ?? []), project.id]);
      toast.success(t("projectsHub.project.settings.added"));
      onSaved();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("projectsHub.project.settings.addFailed"));
    }
  };

  const handleDelete = async () => {
    setDeleting(true);
    try {
      await api.deleteInitiativeProject(project.id);
      toast.success(t("projectsHub.deleted"));
      navigate("/projects");
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("projectsHub.deleteFailed"));
    } finally {
      setDeleting(false);
    }
  };

  return (
    <div className="grid gap-6 lg:grid-cols-2">
      <Card className="space-y-4 p-6">
        <div className="space-y-2">
          <Label htmlFor="project-name">{t("projectsHub.project.settings.nameLabel")}</Label>
          <Input id="project-name" value={name} onChange={(e) => setName(e.target.value)} />
        </div>
        <div className="space-y-2">
          <Label htmlFor="project-description">{t("projectsHub.project.settings.descriptionLabel")}</Label>
          <Textarea
            id="project-description"
            value={description}
            onChange={(e) => setDescription(e.target.value)}
            rows={3}
          />
        </div>
        <div className="flex justify-end">
          <Button onClick={() => void save()} disabled={saving || !name.trim()}>
            {t("common.save")}
          </Button>
        </div>
      </Card>

      <Card className="space-y-3 p-6">
        <h3 className="font-semibold">{t("projectsHub.project.settings.members")}</h3>
        {project.repositories.length === 0 ? (
          <p className="text-sm text-muted-foreground">{t("projectsHub.project.settings.noMembers")}</p>
        ) : (
          <div className="divide-y divide-border rounded-md border border-border">
            {project.repositories.map((repo) => (
              <div key={repo.id} className="flex items-center justify-between gap-2 px-3 py-2">
                <span className="truncate text-sm font-medium">{repo.name}</span>
                <Button
                  variant="ghost"
                  size="sm"
                  disabled={removingId === repo.id}
                  onClick={() => void removeFromProject(repo)}
                >
                  {t("projectsHub.project.settings.removeFromProject")}
                </Button>
              </div>
            ))}
          </div>
        )}
        {availableRepos.length > 0 && (
          <div className="space-y-1.5 pt-1">
            <Label>{t("projectsHub.project.settings.addExisting")}</Label>
            <Select key={availableRepos.length} onValueChange={(id) => void addExisting(id)}>
              <SelectTrigger>
                <SelectValue placeholder={t("projectsHub.project.settings.addExistingPlaceholder")} />
              </SelectTrigger>
              <SelectContent>
                {availableRepos.map((r) => (
                  <SelectItem key={r.id} value={r.id}>
                    {r.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
        )}
      </Card>

      <Card className="space-y-3 border-destructive/30 p-6 lg:col-span-2">
        <h3 className="font-semibold text-destructive">{t("projectsHub.project.settings.dangerZone")}</h3>
        <p className="text-sm text-muted-foreground">{t("projectsHub.project.settings.deleteWarning")}</p>
        <Button variant="destructive" onClick={() => setDeleteOpen(true)} className="gap-2">
          <Trash2 className="h-4 w-4" />
          {t("projectsHub.project.settings.deleteProject")}
        </Button>
      </Card>

      <ConfirmDialog
        open={deleteOpen}
        onOpenChange={setDeleteOpen}
        title={t("projectsHub.project.settings.deleteConfirmTitle", { name: project.name })}
        description={t("projectsHub.project.settings.deleteConfirmDescription")}
        confirmLabel={t("projectsHub.project.settings.delete")}
        loading={deleting}
        onConfirm={handleDelete}
      />
    </div>
  );
}
