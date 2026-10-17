import { useEffect, useId, useState } from "react";
import { api, type MobileStorePlatform, type StoreTestGroup } from "@/api";
import { FormDialog } from "@/components/admin/FormDialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { useI18n } from "@/hooks/useI18n";
import { useStoreAction } from "@/hooks/useMobileReleases";
import { cn } from "@/lib/utils";

interface CreateTestGroupDialogProps {
  repositoryId: string;
  platform: MobileStorePlatform;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onCreated: (group: StoreTestGroup) => void;
}

/** A new TestFlight group; Play's testing tracks are fixed, so iOS only. */
export function CreateTestGroupDialog({ repositoryId, platform, open, onOpenChange, onCreated }: CreateTestGroupDialogProps) {
  const { t } = useI18n();
  const s = (key: string) => t(`operations.storeTest.${key}`);
  const nameId = useId();
  const kindId = useId();
  const [name, setName] = useState("");
  const [internal, setInternal] = useState(true);
  const { busy, run } = useStoreAction();

  useEffect(() => {
    if (!open) return;
    setName("");
    setInternal(true);
  }, [open]);

  const create = async () => {
    const trimmed = name.trim();
    if (!trimmed) return;
    const ok = await run("create", () => api.createStoreTestGroup(repositoryId, platform, { name: trimmed, internal }), (group) => {
      onCreated(group);
      return s("groupCreated");
    });
    if (ok) onOpenChange(false);
  };

  const kinds = [
    { value: true, label: s("groupInternal"), hint: s("groupInternalHint") },
    { value: false, label: s("groupExternal"), hint: s("groupExternalHint") },
  ];

  return (
    <FormDialog
      open={open}
      onOpenChange={(next) => busy === null && onOpenChange(next)}
      title={s("newGroupTitle")}
      footer={
        <>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={busy !== null}>
            {t("common.cancel")}
          </Button>
          <Button onClick={() => void create()} disabled={busy !== null || !name.trim()}>
            {busy !== null ? t("common.saving") : s("create")}
          </Button>
        </>
      }
    >
      <div className="space-y-2">
        <Label htmlFor={nameId}>{s("groupName")}</Label>
        <Input
          id={nameId}
          value={name}
          onChange={(e) => setName(e.target.value)}
          placeholder={s("groupNamePlaceholder")}
          disabled={busy !== null}
          autoComplete="off"
        />
      </div>
      <div className="space-y-2">
        <p id={kindId} className="text-body font-medium">
          {s("groupType")}
        </p>
        <div className="grid gap-2 sm:grid-cols-2" role="group" aria-labelledby={kindId}>
          {kinds.map((kind) => {
            const pressed = internal === kind.value;
            return (
              <Button
                key={String(kind.value)}
                type="button"
                variant="outline"
                aria-pressed={pressed}
                disabled={busy !== null}
                onClick={() => setInternal(kind.value)}
                className={cn(
                  "h-auto flex-col items-start gap-0.5 px-3 py-2 text-left whitespace-normal",
                  pressed && "border-ring bg-accent ring-1 ring-ring",
                )}
              >
                <span className="text-body font-medium">{kind.label}</span>
                <span className="text-caption font-normal text-muted-foreground">{kind.hint}</span>
              </Button>
            );
          })}
        </div>
      </div>
    </FormDialog>
  );
}
