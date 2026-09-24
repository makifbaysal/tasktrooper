import { useEffect, useState } from "react";
import { toast } from "sonner";
import { RESOURCE_KINDS, type ResourceKind, type ResourceRef } from "@/api";
import { FormDialog } from "@/components/admin/FormDialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { useI18n } from "@/hooks/useI18n";

interface ResourceFieldsProps {
  kind: ResourceKind;
  onKindChange: (kind: ResourceKind) => void;
  name: string;
  onNameChange: (name: string) => void;
  vendor: string;
  onVendorChange: (vendor: string) => void;
}

/** The three fields a SystemResource needs, shared by ExternalResourceDialog
 * (retargeting a link) and AddLinkDialog (a brand new link to a resource). */
export function ResourceFields({ kind, onKindChange, name, onNameChange, vendor, onVendorChange }: ResourceFieldsProps) {
  const { t } = useI18n();
  return (
    <>
      <div className="space-y-2">
        <Label>{t("repositoryPage.externalResource.kind")}</Label>
        <Select value={kind} onValueChange={(v) => onKindChange(v as ResourceKind)}>
          <SelectTrigger>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {RESOURCE_KINDS.map((k) => (
              <SelectItem key={k} value={k}>
                {t(`projectModel.resourceKinds.${k}`)}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>
      <div className="space-y-2">
        <Label>{t("repositoryPage.externalResource.name")}</Label>
        <Input value={name} onChange={(e) => onNameChange(e.target.value)} />
      </div>
      <div className="space-y-2">
        <Label>{t("repositoryPage.externalResource.vendor")}</Label>
        <Input value={vendor} onChange={(e) => onVendorChange(e.target.value)} />
      </div>
    </>
  );
}

interface ExternalResourceDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onSubmit: (resource: ResourceRef) => Promise<void>;
}

/** "Mark as external service…" on a link's side panel: retargets it at a
 * SystemResource instead of a component. */
export function ExternalResourceDialog({ open, onOpenChange, onSubmit }: ExternalResourceDialogProps) {
  const { t } = useI18n();
  const [kind, setKind] = useState<ResourceKind>("api");
  const [name, setName] = useState("");
  const [vendor, setVendor] = useState("");
  const [submitting, setSubmitting] = useState(false);

  useEffect(() => {
    if (!open) return;
    setKind("api");
    setName("");
    setVendor("");
  }, [open]);

  const submit = async () => {
    if (!name.trim()) return;
    setSubmitting(true);
    try {
      await onSubmit({ kind, name: name.trim(), vendor: vendor.trim() || undefined });
      onOpenChange(false);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("common.saveFailed"));
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <FormDialog
      open={open}
      onOpenChange={onOpenChange}
      title={t("repositoryPage.externalResource.title")}
      description={t("repositoryPage.externalResource.description")}
      footer={
        <>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={submitting}>
            {t("common.cancel")}
          </Button>
          <Button onClick={submit} disabled={submitting || !name.trim()}>
            {t("repositoryPage.externalResource.submit")}
          </Button>
        </>
      }
    >
      <ResourceFields kind={kind} onKindChange={setKind} name={name} onNameChange={setName} vendor={vendor} onVendorChange={setVendor} />
    </FormDialog>
  );
}
