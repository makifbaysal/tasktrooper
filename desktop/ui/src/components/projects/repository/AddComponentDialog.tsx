import { useEffect, useState } from "react";
import { toast } from "sonner";
import { api, COMPONENT_ROLES, type Component, type ComponentRole } from "@/api";
import { DirectoryPickerDialog } from "@/components/projects/DirectoryPickerDialog";
import { FormDialog } from "@/components/admin/FormDialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { useI18n } from "@/hooks/useI18n";

interface AddComponentDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  repositoryId: string;
  existingPaths: string[];
  onCreated: (component: Component) => void;
}

/** DirectoryPickerDialog first (a folder), then a small role/name form —
 * matches how a monorepo sub-project is added elsewhere in the app. */
export function AddComponentDialog({ open, onOpenChange, repositoryId, existingPaths, onCreated }: AddComponentDialogProps) {
  const { t } = useI18n();
  const [step, setStep] = useState<"pick" | "role">("pick");
  const [path, setPath] = useState("");
  const [role, setRole] = useState<ComponentRole>("other");
  const [name, setName] = useState("");
  const [submitting, setSubmitting] = useState(false);

  useEffect(() => {
    if (!open) return;
    setStep("pick");
    setPath("");
    setRole("other");
    setName("");
  }, [open]);

  const submit = async () => {
    setSubmitting(true);
    try {
      const created = await api.createComponent(repositoryId, { path, role, name: name.trim() || undefined });
      onCreated(created);
      onOpenChange(false);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("repositoryPage.components.saveFailed"));
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <>
      <DirectoryPickerDialog
        open={open && step === "pick"}
        onOpenChange={(next) => {
          if (!next) onOpenChange(false);
        }}
        repositoryId={repositoryId}
        excludePaths={existingPaths}
        onSelect={(selectedPath) => {
          setPath(selectedPath);
          setStep("role");
        }}
      />
      <FormDialog
        open={open && step === "role"}
        onOpenChange={(next) => {
          if (!next) onOpenChange(false);
        }}
        title={t("repositoryPage.addComponent.title")}
        description={t("repositoryPage.addComponent.description")}
        footer={
          <>
            <Button variant="outline" onClick={() => onOpenChange(false)} disabled={submitting}>
              {t("common.cancel")}
            </Button>
            <Button onClick={submit} disabled={submitting || !path}>
              {t("repositoryPage.addComponent.submit")}
            </Button>
          </>
        }
      >
        <div className="space-y-2">
          <Label>{t("repositoryPage.addComponent.path")}</Label>
          <div className="flex items-center justify-between gap-2 rounded-md border border-input px-3 py-2">
            <span className="truncate font-mono text-body">{path}</span>
            <Button type="button" variant="ghost" size="sm" onClick={() => setStep("pick")}>
              {t("repositoryPage.addComponent.changePath")}
            </Button>
          </div>
        </div>
        <div className="space-y-2">
          <Label>{t("repositoryPage.addComponent.role")}</Label>
          <Select value={role} onValueChange={(v) => setRole(v as ComponentRole)}>
            <SelectTrigger>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {COMPONENT_ROLES.map((r) => (
                <SelectItem key={r} value={r}>
                  {t(`projectModel.roles.${r}`)}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
        <div className="space-y-2">
          <Label>{t("repositoryPage.addComponent.name")}</Label>
          <Input
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder={t("repositoryPage.components.namePlaceholder")}
          />
        </div>
      </FormDialog>
    </>
  );
}
