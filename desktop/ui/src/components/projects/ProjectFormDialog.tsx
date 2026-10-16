import { Loader2 } from "lucide-react";
import { useEffect, useState } from "react";
import { toast } from "sonner";
import { api, type InitiativeProject } from "@/api";
import { FormDialog } from "@/components/admin/FormDialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { useI18n } from "@/hooks/useI18n";
import { logEvent } from "@/lib/analytics";

interface ProjectFormDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** The project being edited, or null to create one. */
  project?: InitiativeProject | null;
  /** Fires after the server accepted the write, with what it answered. */
  onSaved: (project: InitiativeProject) => void;
}

/**
 * Name and describe a project (an initiative).
 *
 * Shared by `ProjectsPage` and step 4 of the guided setup, which needs exactly
 * this dialog for the FIRST project — the one the sequence then imports a
 * repository into. Keeping it in one place is what stops the setup flow from
 * quietly creating projects through a form that has since grown a field.
 */
export function ProjectFormDialog({ open, onOpenChange, project = null, onSaved }: ProjectFormDialogProps) {
  const { t } = useI18n();
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [saving, setSaving] = useState(false);

  // Seeded on open rather than from an initialiser: the same mounted dialog is
  // reopened for a different project, and useState would keep the first one's
  // values for ever.
  useEffect(() => {
    if (!open) return;
    setName(project?.name ?? "");
    setDescription(project?.description ?? "");
  }, [open, project]);

  const save = async () => {
    if (!name.trim()) {
      toast.error(t("projectAdmin.projects.nameRequired"));
      return;
    }
    setSaving(true);
    try {
      const saved = project
        ? await api.updateInitiativeProject(project.id, {
            name: name.trim(),
            description: description.trim(),
          })
        : await api.createInitiativeProject({ name: name.trim(), description: description.trim() });
      toast.success(project ? t("projectAdmin.projects.updated") : t("projectAdmin.projects.created"));
      if (!project) logEvent("project_create", {});
      onOpenChange(false);
      onSaved(saved);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("common.saveFailed"));
    } finally {
      setSaving(false);
    }
  };

  return (
    <FormDialog
      open={open}
      onOpenChange={onOpenChange}
      title={project ? t("projectAdmin.projects.editTitle") : t("projectAdmin.projects.newTitle")}
      footer={
        <>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            {t("common.cancel")}
          </Button>
          <Button onClick={() => void save()} disabled={saving || !name.trim()}>
            {saving && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
            {project ? t("common.save") : t("projectAdmin.projects.create")}
          </Button>
        </>
      }
    >
      <div className="space-y-2">
        <Label htmlFor="initiative-name">{t("projectAdmin.projects.nameLabel")}</Label>
        <Input
          id="initiative-name"
          value={name}
          onChange={(e) => setName(e.target.value)}
          placeholder={t("projectAdmin.projects.namePlaceholder")}
          autoFocus
        />
      </div>
      <div className="space-y-2">
        <Label htmlFor="initiative-desc">{t("projectAdmin.projects.descriptionLabel")}</Label>
        <Textarea
          id="initiative-desc"
          value={description}
          onChange={(e) => setDescription(e.target.value)}
          rows={4}
          placeholder={t("projectAdmin.projects.descriptionPlaceholder")}
        />
      </div>
    </FormDialog>
  );
}
