import { useEffect, useState } from "react";
import { toast } from "sonner";
import { api, LINK_PROTOCOLS, type LinkProtocol, type ResourceKind } from "@/api";
import { ComponentPickerDialog } from "@/components/projects/repository/ComponentPickerDialog";
import { ResourceFields } from "@/components/projects/repository/ExternalResourceDialog";
import { FormDialog } from "@/components/admin/FormDialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { useI18n } from "@/hooks/useI18n";
import { cn } from "@/lib/utils";

interface AddLinkDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  fromComponentId: string;
  fromLabel: string;
  onCreated: () => void;
}

type TargetType = "component" | "resource";

export function AddLinkDialog({ open, onOpenChange, fromComponentId, fromLabel, onCreated }: AddLinkDialogProps) {
  const { t } = useI18n();
  const [targetType, setTargetType] = useState<TargetType>("component");
  const [targetComponentId, setTargetComponentId] = useState("");
  const [targetComponentLabel, setTargetComponentLabel] = useState("");
  const [pickerOpen, setPickerOpen] = useState(false);
  const [kind, setKind] = useState<ResourceKind>("api");
  const [name, setName] = useState("");
  const [vendor, setVendor] = useState("");
  const [protocol, setProtocol] = useState<LinkProtocol>("http");
  const [detail, setDetail] = useState("");
  const [submitting, setSubmitting] = useState(false);

  useEffect(() => {
    if (!open) return;
    setTargetType("component");
    setTargetComponentId("");
    setTargetComponentLabel("");
    setKind("api");
    setName("");
    setVendor("");
    setProtocol("http");
    setDetail("");
  }, [open]);

  const canSubmit = targetType === "component" ? Boolean(targetComponentId) : Boolean(name.trim());

  const submit = async () => {
    if (!canSubmit) return;
    setSubmitting(true);
    try {
      await api.createLink({
        from_component_id: fromComponentId,
        protocol,
        detail: detail.trim() || undefined,
        ...(targetType === "component"
          ? { to_component_id: targetComponentId }
          : { to_resource: { kind, name: name.trim(), vendor: vendor.trim() || undefined } }),
      });
      onCreated();
      onOpenChange(false);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("common.saveFailed"));
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <>
      <FormDialog
        open={open}
        onOpenChange={onOpenChange}
        title={t("repositoryPage.addLink.title")}
        description={t("repositoryPage.addLink.description")}
        footer={
          <>
            <Button variant="outline" onClick={() => onOpenChange(false)} disabled={submitting}>
              {t("common.cancel")}
            </Button>
            <Button onClick={submit} disabled={submitting || !canSubmit}>
              {t("repositoryPage.addLink.submit")}
            </Button>
          </>
        }
      >
        <div className="space-y-1.5">
          <Label className="text-xs text-muted-foreground">{t("repositoryPage.addLink.from")}</Label>
          <p className="font-mono text-body">{fromLabel}</p>
        </div>

        <div className="space-y-2">
          <Label>{t("repositoryPage.addLink.targetType")}</Label>
          <div className="flex gap-2">
            <Button
              type="button"
              size="sm"
              variant={targetType === "component" ? "default" : "outline"}
              onClick={() => setTargetType("component")}
            >
              {t("repositoryPage.addLink.targetComponent")}
            </Button>
            <Button
              type="button"
              size="sm"
              variant={targetType === "resource" ? "default" : "outline"}
              onClick={() => setTargetType("resource")}
            >
              {t("repositoryPage.addLink.targetResource")}
            </Button>
          </div>
        </div>

        {targetType === "component" ? (
          <div className="space-y-2">
            <Button
              type="button"
              variant="outline"
              size="sm"
              onClick={() => setPickerOpen(true)}
              className={cn(!targetComponentLabel && "text-muted-foreground")}
            >
              {targetComponentLabel || t("repositoryPage.componentPicker.title")}
            </Button>
          </div>
        ) : (
          <ResourceFields kind={kind} onKindChange={setKind} name={name} onNameChange={setName} vendor={vendor} onVendorChange={setVendor} />
        )}

        <div className="grid gap-3 sm:grid-cols-2">
          <div className="space-y-2">
            <Label>{t("repositoryPage.addLink.protocol")}</Label>
            <Select value={protocol} onValueChange={(v) => setProtocol(v as LinkProtocol)}>
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {LINK_PROTOCOLS.map((p) => (
                  <SelectItem key={p} value={p}>
                    {t(`projectModel.linkProtocols.${p}`)}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <div className="space-y-2">
            <Label>{t("repositoryPage.addLink.detail")}</Label>
            <Input value={detail} onChange={(e) => setDetail(e.target.value)} />
          </div>
        </div>
      </FormDialog>

      <ComponentPickerDialog
        open={pickerOpen}
        onOpenChange={setPickerOpen}
        onPick={(id, label) => {
          setTargetComponentId(id);
          setTargetComponentLabel(label);
        }}
      />
    </>
  );
}
