import { useEffect, useId, useState } from "react";
import { toast } from "sonner";
import { api, type MobileStorePlatform, type StoreTestGroup } from "@/api";
import { FormDialog } from "@/components/admin/FormDialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { useI18n } from "@/hooks/useI18n";

interface NewTestGroupDialogProps {
  repositoryId: string;
  platform: MobileStorePlatform;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onCreated: (group: StoreTestGroup) => void;
}

/** A new TestFlight group (iOS only: Play's testing tracks are fixed). */
export function NewTestGroupDialog({ repositoryId, platform, open, onOpenChange, onCreated }: NewTestGroupDialogProps) {
  const { t } = useI18n();
  const nameId = useId();
  const kindName = useId();
  const [name, setName] = useState("");
  const [internal, setInternal] = useState(true);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    if (!open) return;
    setName("");
    setInternal(true);
  }, [open]);

  const create = async () => {
    const trimmed = name.trim();
    if (!trimmed) return;
    setBusy(true);
    try {
      const group = await api.createStoreTestGroup(repositoryId, platform, { name: trimmed, internal });
      toast.success(t("operations.storeTest.groupCreated"));
      onCreated(group);
      onOpenChange(false);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("common.actionFailed"));
    } finally {
      setBusy(false);
    }
  };

  const kinds = [
    {
      value: true,
      label: t("operations.storeTest.groupInternal"),
      hint: t("operations.storeTest.groupInternalHint"),
    },
    {
      value: false,
      label: t("operations.storeTest.groupExternal"),
      hint: t("operations.storeTest.groupExternalHint"),
    },
  ];

  return (
    <FormDialog
      open={open}
      onOpenChange={(next) => !busy && onOpenChange(next)}
      title={t("operations.storeTest.newGroupTitle")}
      footer={
        <>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={busy}>
            {t("common.cancel")}
          </Button>
          <Button onClick={() => void create()} disabled={busy || !name.trim()}>
            {busy ? t("common.saving") : t("operations.storeTest.create")}
          </Button>
        </>
      }
    >
      <div className="space-y-2">
        <Label htmlFor={nameId}>{t("operations.storeTest.groupName")}</Label>
        <Input
          id={nameId}
          value={name}
          onChange={(e) => setName(e.target.value)}
          placeholder={t("operations.storeTest.groupNamePlaceholder")}
          disabled={busy}
          autoComplete="off"
        />
      </div>
      <fieldset className="space-y-2">
        <legend className="text-sm font-medium">{t("operations.storeTest.groupType")}</legend>
        <div className="divide-y divide-border rounded-lg border border-border">
          {kinds.map((kind) => (
            <label
              key={String(kind.value)}
              className="flex cursor-pointer items-start gap-3 px-3 py-2 transition-colors hover:bg-muted/50 has-[:checked]:bg-muted"
            >
              <input
                type="radio"
                name={kindName}
                className="mt-1 h-4 w-4 shrink-0 accent-primary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                checked={internal === kind.value}
                disabled={busy}
                onChange={() => setInternal(kind.value)}
              />
              <span className="min-w-0 space-y-0.5">
                <span className="block text-sm font-medium">{kind.label}</span>
                <span className="block text-xs text-muted-foreground">{kind.hint}</span>
              </span>
            </label>
          ))}
        </div>
      </fieldset>
    </FormDialog>
  );
}
